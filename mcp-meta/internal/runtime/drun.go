// Package runtime provides abstractions for different execution environments.
// This file implements the DrunRuntime which uses drun's RuntimeBackend
// to run pods in containers or VMs (Docker, Firecracker, VZ, Sandbox).
package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	drunruntime "drun/runtime"
)

// DrunRuntime executes pods using drun's RuntimeBackend interface.
// This enables running pods in Docker containers, Firecracker microVMs,
// Apple Virtualization.framework VMs, or sandboxed processes.
type DrunRuntime struct {
	backend   drunruntime.RuntimeBackend
	logger    *slog.Logger
	instances sync.Map // map[string]*drunInstance
}

// NewDrunRuntime creates a new drun-based runtime using the provided backend.
func NewDrunRuntime(backend drunruntime.RuntimeBackend, logger *slog.Logger) *DrunRuntime {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return &DrunRuntime{
		backend: backend,
		logger:  logger,
	}
}

func (r *DrunRuntime) Name() string {
	return "drun:" + r.backend.Name()
}

func (r *DrunRuntime) Spawn(ctx context.Context, spec *InstanceSpec) (Instance, error) {
	if spec.PodSpec == nil {
		return nil, fmt.Errorf("PodSpec is required")
	}

	// Only require image for container-based backends (docker, firecracker, vz)
	// Sandbox backend runs local processes and doesn't need an image
	backendName := r.backend.Name()
	needsImage := backendName != "sandbox"

	image := spec.Image
	if image == "" {
		image = spec.PodSpec.Image
	}

	if needsImage && image == "" {
		return nil, fmt.Errorf("pod %s: image is required for %s runtime", spec.Name, backendName)
	}

	// Prepare environment variables
	env := make(map[string]string)
	for k, v := range spec.PodSpec.Env {
		env[k] = v
	}
	for k, v := range spec.Env {
		env[k] = v
	}
	if spec.ReplicaID != "" {
		env["POD_REPLICA_ID"] = spec.ReplicaID
	}

	// Convert to drun InstanceSpec.
	// Use "sleep infinity" as the container CMD to keep it alive.
	// The actual pod command runs via ExecInteractive in startCommunication.
	drunSpec := &drunruntime.InstanceSpec{
		Name:    fmt.Sprintf("%s-%d", spec.Name, time.Now().UnixNano()),
		Image:   image,
		Command: []string{"sleep", "infinity"},
		Workdir: spec.PodSpec.Workdir,
		Env:     env,
		Labels: map[string]string{
			"mcp-meta.pod": spec.Name,
		},
		Resources: drunruntime.ResourceLimits{
			CPUCores: spec.Resources.CPUCores,
			MemoryMB: spec.Resources.MemoryMB,
		},
	}

	inst := &drunInstance{
		id:        drunSpec.Name,
		name:      spec.Name,
		spec:      spec,
		backend:   r.backend,
		pending:   make(map[int64]chan *jsonRPCResponse),
		done:      make(chan struct{}),
		startedAt: time.Now(),
		logger:    r.logger.With("instance", spec.Name, "runtime", r.backend.Name()),
	}
	inst.state.Store(StateStarting)
	inst.lastError.Store("")

	// Create the container/VM
	containerID, err := r.backend.Create(ctx, drunSpec)
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}
	inst.containerID = containerID

	// Start the container/VM
	if err := r.backend.Start(ctx, containerID); err != nil {
		err := r.backend.Remove(ctx, containerID)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("start container: %w", err)
	}

	// Start the communication loop with the container
	if err := inst.startCommunication(ctx); err != nil {
		r.backend.Stop(ctx, containerID, 5)
		r.backend.Remove(ctx, containerID)
		return nil, fmt.Errorf("start communication: %w", err)
	}

	r.instances.Store(inst.id, inst)
	return inst, nil
}

func (r *DrunRuntime) Close() error {
	var wg sync.WaitGroup
	r.instances.Range(func(key, value any) bool {
		inst := value.(*drunInstance)
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := inst.Stop(ctx)
			if err != nil {
				return
			}
		}()
		return true
	})
	wg.Wait()
	return r.backend.Close()
}

// drunInstance is a single container/VM running a pod.
type drunInstance struct {
	id          string
	name        string
	containerID string
	spec        *InstanceSpec
	backend     drunruntime.RuntimeBackend

	stdin  io.WriteCloser
	stdout io.ReadCloser

	pending   map[int64]chan *jsonRPCResponse
	pendingMu sync.Mutex
	nextID    atomic.Int64

	state     atomic.Value // InstanceState
	startedAt time.Time
	callCount atomic.Int64
	lastError atomic.Value // string

	done      chan struct{}
	closeOnce sync.Once
	logger    *slog.Logger

	encoderMu       sync.Mutex
	interPodHandler InterPodHandler
	healthCheckStop chan struct{}
}

// startCommunication attaches to the container and sets up stdio communication.
// This uses ExecInteractive to run the pod's command with attached pipes.
func (inst *drunInstance) startCommunication(ctx context.Context) error {
	// Create pipes for communication
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	inst.stdin = stdinWriter
	inst.stdout = stdoutReader

	// Start ExecInteractive in a goroutine
	go func() {
		cmd := inst.spec.PodSpec.Command
		if len(cmd) == 0 {
			cmd = []string{"/bin/sh", "-c", "cat"} // Fallback for containers with entrypoint
		}

		opts := &drunruntime.ExecOptions{
			TTY:    false,
			Stdin:  stdinReader,
			Stdout: stdoutWriter,
			Stderr: os.Stderr,
		}

		err := inst.backend.ExecInteractive(context.Background(), inst.containerID, cmd, opts)
		if err != nil {
			inst.logger.Error("exec interactive failed", "error", err)
			inst.lastError.Store(err.Error())
			if inst.getState() != StateStopping {
				inst.state.Store(StateCrashed)
			}
		}

		stdinReader.Close()
		stdoutWriter.Close()
		close(inst.done)
	}()

	// Start response reader
	go inst.readResponses()

	// Mark as running
	inst.state.Store(StateRunning)
	inst.logger.Info("instance started", "container_id", inst.containerID)

	// Start health checks if configured
	if inst.spec.PodSpec.HealthCheck != nil && inst.spec.PodSpec.HealthCheck.Enabled {
		inst.startHealthCheck()
	}

	return nil
}

func (inst *drunInstance) startHealthCheck() {
	interval := inst.spec.PodSpec.HealthCheck.Interval.Duration
	if interval <= 0 {
		interval = 30 * time.Second
	}

	inst.healthCheckStop = make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				inst.performHealthCheck()
			case <-inst.healthCheckStop:
				return
			case <-inst.done:
				return
			}
		}
	}()
}

func (inst *drunInstance) performHealthCheck() {
	if inst.getState() != StateRunning {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := inst.Call(ctx, "ping", nil)
	if err != nil {
		inst.logger.Warn("health check failed", "error", err)
		return
	}

	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		inst.logger.Warn("health check invalid response", "error", err)
		return
	}

	inst.logger.Debug("health check passed", "status", resp.Status)
}

func (inst *drunInstance) readResponses() {
	scanner := bufio.NewScanner(inst.stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var msg map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			inst.logger.Warn("invalid JSON from instance", "error", err, "line", line)
			continue
		}

		// Check if it's an inter-pod request (has "method" field)
		if _, hasMethod := msg["method"]; hasMethod {
			inst.handleInterPodRequest(line)
			continue
		}

		// It's a response
		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			inst.logger.Warn("invalid JSON response", "error", err, "line", line)
			continue
		}

		inst.routeResponse(&resp)
	}

	if err := scanner.Err(); err != nil {
		if inst.getState() == StateRunning {
			inst.logger.Error("stdout read error", "error", err)
		}
	}
}

func (inst *drunInstance) handleInterPodRequest(line string) {
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int64           `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		inst.logger.Warn("invalid inter-pod request", "error", err)
		return
	}

	if req.Method != "__mcp_meta_call__" {
		inst.logger.Warn("unknown inter-pod method", "method", req.Method)
		inst.sendInterPodError(req.ID, -32601, "unknown method: "+req.Method)
		return
	}

	if inst.interPodHandler == nil {
		inst.logger.Warn("no inter-pod handler configured")
		inst.sendInterPodError(req.ID, -32603, "inter-pod calls not configured")
		return
	}

	var params struct {
		Tool     string               `json:"tool"`
		Args     json.RawMessage      `json:"args"`
		Contract *ContractExpectation `json:"contract,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		inst.sendInterPodError(req.ID, -32602, "invalid params: "+err.Error())
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := inst.interPodHandler(ctx, inst.name, params.Tool, params.Args, params.Contract)
		if err != nil {
			var jsonErr *jsonRPCError
			if errors.As(err, &jsonErr) {
				inst.sendInterPodError(req.ID, jsonErr.Code, jsonErr.Message)
			}
			return
		}

		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		}
		inst.sendInterPodResponse(&resp)
	}()
}

func (inst *drunInstance) sendInterPodError(id int64, code int, message string) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonRPCError{
			Code:    code,
			Message: message,
		},
	}
	inst.sendInterPodResponse(&resp)
}

func (inst *drunInstance) sendInterPodResponse(resp *jsonRPCResponse) {
	inst.encoderMu.Lock()
	defer inst.encoderMu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		inst.logger.Error("failed to marshal inter-pod response", "error", err)
		return
	}

	if _, err := inst.stdin.Write(append(data, '\n')); err != nil {
		inst.logger.Error("failed to write inter-pod response", "error", err)
	}
}

func (inst *drunInstance) routeResponse(resp *jsonRPCResponse) {
	inst.pendingMu.Lock()
	ch, ok := inst.pending[resp.ID]
	if ok {
		delete(inst.pending, resp.ID)
	}
	inst.pendingMu.Unlock()

	if !ok {
		inst.logger.Warn("received response for unknown request", "id", resp.ID)
		return
	}

	ch <- resp
}

// Instance interface implementation

func (inst *drunInstance) ID() string {
	return inst.id
}

func (inst *drunInstance) Name() string {
	return inst.name
}

func (inst *drunInstance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if inst.getState() != StateRunning {
		return nil, fmt.Errorf("instance %s is not running (state: %s)", inst.name, inst.getState())
	}

	id := inst.nextID.Add(1)
	respCh := make(chan *jsonRPCResponse, 1)

	inst.pendingMu.Lock()
	inst.pending[id] = respCh
	inst.pendingMu.Unlock()

	defer func() {
		inst.pendingMu.Lock()
		delete(inst.pending, id)
		inst.pendingMu.Unlock()
	}()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := inst.sendRequest(&req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	inst.callCount.Add(1)

	select {
	case resp, ok := <-respCh:
		if !ok {
			return nil, fmt.Errorf("instance %s died while waiting for response", inst.name)
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil

	case <-ctx.Done():
		return nil, ctx.Err()

	case <-inst.done:
		return nil, fmt.Errorf("instance %s died while waiting for response", inst.name)
	}
}

func (inst *drunInstance) sendRequest(req *jsonRPCRequest) error {
	inst.encoderMu.Lock()
	defer inst.encoderMu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	if _, err := inst.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}

	return nil
}

func (inst *drunInstance) Stop(ctx context.Context) error {
	var stopErr error
	inst.closeOnce.Do(func() {
		if inst.healthCheckStop != nil {
			close(inst.healthCheckStop)
		}

		inst.state.Store(StateStopping)

		// Try graceful shutdown first
		shutdownTimeout := 5 * time.Second
		if inst.spec.PodSpec != nil && inst.spec.PodSpec.ShutdownTimeout.Duration > 0 {
			shutdownTimeout = inst.spec.PodSpec.ShutdownTimeout.Duration
		}

		shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()

		// Send shutdown request to the pod
		_, err := inst.Call(shutdownCtx, "shutdown", nil)
		if err != nil {
			inst.logger.Warn("shutdown request failed", "error", err)
		}

		// Wait briefly for graceful exit
		select {
		case <-inst.done:
			inst.logger.Info("instance stopped gracefully")
		case <-shutdownCtx.Done():
			inst.logger.Warn("shutdown timeout, stopping container")
		}

		// Stop and remove the container with a fresh context
		// (the parent ctx may be nearly expired after the shutdown wait)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()

		if err := inst.backend.Stop(cleanupCtx, inst.containerID, int(shutdownTimeout.Seconds())); err != nil {
			inst.logger.Warn("stop container failed", "error", err)
		}

		if err := inst.backend.Remove(cleanupCtx, inst.containerID); err != nil {
			inst.logger.Warn("remove container failed", "error", err)
		}

		inst.state.Store(StateStopped)

		// Close pipes
		if inst.stdin != nil {
			inst.stdin.Close()
		}
		if inst.stdout != nil {
			inst.stdout.Close()
		}
	})
	return stopErr
}

func (inst *drunInstance) Status() InstanceStatus {
	lastErr := ""
	if v := inst.lastError.Load(); v != nil {
		lastErr = v.(string)
	}

	return InstanceStatus{
		ID:        inst.id,
		Name:      inst.name,
		State:     inst.getState(),
		Address:   inst.containerID,
		StartedAt: inst.startedAt,
		CallCount: inst.callCount.Load(),
		LastError: lastErr,
	}
}

func (inst *drunInstance) SetInterPodHandler(handler InterPodHandler) {
	inst.interPodHandler = handler
}

func (inst *drunInstance) getState() InstanceState {
	if v := inst.state.Load(); v != nil {
		return v.(InstanceState)
	}
	return StateStopped
}
