package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// SubprocessRuntime executes pods as local subprocesses.
// This is the default runtime for local development and single-machine deployments.
type SubprocessRuntime struct {
	logger    *slog.Logger
	instances sync.Map // map[string]*subprocessInstance
}

// NewSubprocessRuntime creates a new subprocess-based runtime.
func NewSubprocessRuntime(logger *slog.Logger) *SubprocessRuntime {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return &SubprocessRuntime{
		logger: logger,
	}
}

func (r *SubprocessRuntime) Name() string {
	return "subprocess"
}

func (r *SubprocessRuntime) Spawn(ctx context.Context, spec *InstanceSpec) (Instance, error) {
	if spec.PodSpec == nil {
		return nil, fmt.Errorf("PodSpec is required")
	}

	if len(spec.PodSpec.Command) == 0 {
		return nil, fmt.Errorf("pod %s: empty command", spec.Name)
	}

	inst := &subprocessInstance{
		id:        fmt.Sprintf("%s-%d", spec.Name, time.Now().UnixNano()),
		name:      spec.Name,
		spec:      spec,
		pending:   make(map[int64]chan *jsonRPCResponse),
		done:      make(chan struct{}),
		startedAt: time.Now(),
		logger:    r.logger.With("instance", spec.Name),
	}
	inst.state.Store(StateStarting)
	inst.lastError.Store("")

	if err := inst.start(); err != nil {
		return nil, err
	}

	r.instances.Store(inst.id, inst)
	return inst, nil
}

func (r *SubprocessRuntime) Close() error {
	var wg sync.WaitGroup
	r.instances.Range(func(key, value any) bool {
		inst := value.(*subprocessInstance)
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			inst.Stop(ctx)
		}()
		return true
	})
	wg.Wait()
	return nil
}

// subprocessInstance is a single subprocess running a pod.
type subprocessInstance struct {
	id        string
	name      string
	spec      *InstanceSpec
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderr    io.ReadCloser

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

// JSON-RPC types (internal to avoid dependency on jsonrpc package for the interface)
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *jsonRPCError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

func (inst *subprocessInstance) start() error {
	podSpec := inst.spec.PodSpec

	// Create the command
	inst.cmd = exec.Command(podSpec.Command[0], podSpec.Command[1:]...)
	inst.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if podSpec.Workdir != "" {
		inst.cmd.Dir = podSpec.Workdir
		inst.logger.Debug("setting workdir", "workdir", podSpec.Workdir)
	}

	// Set environment
	inst.cmd.Env = os.Environ()
	for k, v := range podSpec.Env {
		inst.cmd.Env = append(inst.cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range inst.spec.Env {
		inst.cmd.Env = append(inst.cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	if inst.spec.ReplicaID != "" {
		inst.cmd.Env = append(inst.cmd.Env, fmt.Sprintf("POD_REPLICA_ID=%s", inst.spec.ReplicaID))
	}

	// Setup pipes
	var err error
	inst.stdin, err = inst.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	inst.stdout, err = inst.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	inst.stderr, err = inst.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	// Start the process
	if err := inst.cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	inst.logger.Info("instance started", "pid", inst.cmd.Process.Pid)

	// Start goroutines for I/O handling
	go inst.readResponses()
	go inst.readStderr()
	go inst.monitor()

	inst.state.Store(StateRunning)

	// Start health checks if configured
	if podSpec.HealthCheck != nil && podSpec.HealthCheck.Enabled {
		inst.startHealthCheck()
	}

	return nil
}

func (inst *subprocessInstance) startHealthCheck() {
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

func (inst *subprocessInstance) performHealthCheck() {
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

func (inst *subprocessInstance) readResponses() {
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

func (inst *subprocessInstance) handleInterPodRequest(line string) {
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
		Tool     string              `json:"tool"`
		Args     json.RawMessage     `json:"args"`
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
			if jsonErr, ok := err.(*jsonRPCError); ok {
				inst.sendInterPodError(req.ID, jsonErr.Code, jsonErr.Message)
			} else {
				inst.sendInterPodError(req.ID, -32000, err.Error())
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

func (inst *subprocessInstance) sendInterPodError(id int64, code int, message string) {
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

func (inst *subprocessInstance) sendInterPodResponse(resp *jsonRPCResponse) {
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

func (inst *subprocessInstance) readStderr() {
	scanner := bufio.NewScanner(inst.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		inst.logger.Info("instance stderr", "line", line)
	}
}

func (inst *subprocessInstance) monitor() {
	err := inst.cmd.Wait()

	// Close all pending response channels
	inst.pendingMu.Lock()
	for id, ch := range inst.pending {
		close(ch)
		delete(inst.pending, id)
	}
	inst.pendingMu.Unlock()

	currentState := inst.getState()
	if err != nil {
		if currentState == StateStopping {
			inst.state.Store(StateStopped)
			inst.logger.Info("instance stopped", "exit_status", err.Error())
		} else {
			inst.state.Store(StateCrashed)
			inst.lastError.Store(err.Error())
			inst.logger.Error("instance crashed", "error", err, "previous_state", currentState)
		}
	} else {
		inst.state.Store(StateStopped)
		inst.logger.Info("instance stopped")
	}

	close(inst.done)
}

func (inst *subprocessInstance) routeResponse(resp *jsonRPCResponse) {
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

func (inst *subprocessInstance) ID() string {
	return inst.id
}

func (inst *subprocessInstance) Name() string {
	return inst.name
}

func (inst *subprocessInstance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
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

func (inst *subprocessInstance) sendRequest(req *jsonRPCRequest) error {
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

func (inst *subprocessInstance) Stop(ctx context.Context) error {
	var stopErr error
	inst.closeOnce.Do(func() {
		if inst.healthCheckStop != nil {
			close(inst.healthCheckStop)
		}

		shutdownTimeout := 5 * time.Second
		if inst.spec.PodSpec != nil && inst.spec.PodSpec.ShutdownTimeout.Duration > 0 {
			shutdownTimeout = inst.spec.PodSpec.ShutdownTimeout.Duration
		}

		shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()

		_, err := inst.Call(shutdownCtx, "shutdown", nil)
		if err != nil {
			inst.logger.Warn("shutdown request failed", "error", err)
		}

		select {
		case <-inst.done:
			return
		case <-shutdownCtx.Done():
			inst.state.Store(StateStopping)
			inst.logger.Warn("shutdown timeout, killing process")
			if inst.cmd.Process != nil {
				if err := inst.cmd.Process.Kill(); err != nil {
					stopErr = fmt.Errorf("kill process: %w", err)
				}
			}
		}

		<-inst.done
	})
	return stopErr
}

func (inst *subprocessInstance) Status() InstanceStatus {
	pid := 0
	if inst.cmd != nil && inst.cmd.Process != nil {
		pid = inst.cmd.Process.Pid
	}

	lastErr := ""
	if v := inst.lastError.Load(); v != nil {
		lastErr = v.(string)
	}

	return InstanceStatus{
		ID:        inst.id,
		Name:      inst.name,
		State:     inst.getState(),
		PID:       pid,
		StartedAt: inst.startedAt,
		CallCount: inst.callCount.Load(),
		LastError: lastErr,
	}
}

func (inst *subprocessInstance) SetInterPodHandler(handler InterPodHandler) {
	inst.interPodHandler = handler
}

func (inst *subprocessInstance) getState() InstanceState {
	if v := inst.state.Load(); v != nil {
		return v.(InstanceState)
	}
	return StateStopped
}
