package supervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"mcp-meta/internal/jsonrpc"
	"mcp-meta/internal/registry"
)

type PodState string

const (
	PodStateStarting PodState = "starting"
	PodStateRunning  PodState = "running"
	PodStateStopping PodState = "stopping"
	PodStateStopped  PodState = "stopped"
	PodStateCrashed  PodState = "crashed"
)

type PodStatus struct {
	Name      string
	PID       int
	State     PodState
	StartedAt time.Time
	CallCount int64
	LastError string
}

type InterPodCallHandler func(ctx context.Context, sourcePod string, targetTool string, args json.RawMessage) (json.RawMessage, error)

type InterPodCallHandlerWithContract func(ctx context.Context, sourcePod string, targetTool string, args json.RawMessage, contract *jsonrpc.ContractExpectation) (json.RawMessage, error)

type Pod struct {
	name   string
	spec   *registry.PodSpec
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	pending   map[int64]chan *jsonrpc.Response
	pendingMu sync.Mutex
	nextID    atomic.Int64

	state     atomic.Value
	startedAt time.Time
	callCount atomic.Int64
	lastError atomic.Value

	done      chan struct{}
	closeOnce sync.Once
	logger    *slog.Logger

	encoderMu sync.Mutex

	interPodHandler             InterPodCallHandler
	interPodHandlerWithContract InterPodCallHandlerWithContract

	healthCheckStop chan struct{}

	// Progress notification subscribers (keyed by subscriber ID)
	progressMu   sync.Mutex
	progressSubs map[int64]chan json.RawMessage
	nextSubID    atomic.Int64
}

// globalProgressHandler is a package-level function set by the supervisor.
// All pods call this when they receive progress notifications.
var globalProgressHandler func(json.RawMessage)

// SetGlobalProgressHandler sets the global progress handler (called by supervisor).
func SetGlobalProgressHandler(fn func(json.RawMessage)) {
	globalProgressHandler = fn
}

// ProgressEvent is emitted by pods via JSON-RPC notifications with method "progress".
type ProgressEvent struct {
	Agent   string `json:"agent"`
	Round   int    `json:"round"`
	Content string `json:"content"`
}

func newPod(name string, spec *registry.PodSpec, logger *slog.Logger) (*Pod, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	p := &Pod{
		name:         name,
		spec:         spec,
		pending:      make(map[int64]chan *jsonrpc.Response),
		progressSubs: make(map[int64]chan json.RawMessage),
		done:         make(chan struct{}),
		startedAt:    time.Now(),
		logger:       logger.With("pod", name),
	}
	p.state.Store(PodStateStarting)
	p.lastError.Store("")

	if err := p.start(); err != nil {
		return nil, err
	}

	return p, nil
}

func (p *Pod) start() error {
	if len(p.spec.Command) == 0 {
		return fmt.Errorf("pod %s: empty command", p.name)
	}

	// Pod lifetime is independent of request context - use process group for signal isolation
	p.cmd = exec.Command(p.spec.Command[0], p.spec.Command[1:]...)
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if p.spec.Workdir != "" {
		p.cmd.Dir = p.spec.Workdir
		p.logger.Debug("setting workdir", "workdir", p.spec.Workdir)
	}

	p.cmd.Env = os.Environ()
	for k, v := range p.spec.Env {
		p.cmd.Env = append(p.cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	var err error
	p.stdin, err = p.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	p.stdout, err = p.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	p.stderr, err = p.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	p.logger.Info("pod started", "pid", p.cmd.Process.Pid)

	go p.readResponses()
	go p.readStderr()
	go p.monitor()

	p.state.Store(PodStateRunning)

	if p.spec.HealthCheck != nil && p.spec.HealthCheck.Enabled {
		p.startHealthCheck()
	}

	return nil
}

func (p *Pod) startHealthCheck() {
	interval := p.spec.HealthCheck.Interval.Duration
	if interval <= 0 {
		interval = 30 * time.Second
	}

	p.healthCheckStop = make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.performHealthCheck()
			case <-p.healthCheckStop:
				return
			case <-p.done:
				return
			}
		}
	}()
}

func (p *Pod) performHealthCheck() {
	if p.getState() != PodStateRunning {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := p.Call(ctx, "ping", nil)
	if err != nil {
		p.logger.Warn("health check failed", "error", err)
		return
	}

	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		p.logger.Warn("health check invalid response", "error", err)
		return
	}

	p.logger.Debug("health check passed", "status", resp.Status)
}

// SubscribeProgress returns a channel that receives progress notifications from this pod.
// Call the returned cancel func to unsubscribe.
func (p *Pod) SubscribeProgress() (<-chan json.RawMessage, func()) {
	ch := make(chan json.RawMessage, 32)
	id := p.nextSubID.Add(1)
	p.progressMu.Lock()
	p.progressSubs[id] = ch
	p.progressMu.Unlock()
	return ch, func() {
		p.progressMu.Lock()
		delete(p.progressSubs, id)
		p.progressMu.Unlock()
		// drain
		for range ch {
		}
	}
}

func (p *Pod) emitProgress(params json.RawMessage) {
	// Forward to local subscribers
	p.progressMu.Lock()
	for _, ch := range p.progressSubs {
		select {
		case ch <- params:
		default:
		}
	}
	p.progressMu.Unlock()
	// Forward to global progress handler
	if globalProgressHandler != nil {
		globalProgressHandler(params)
	}
}

func (p *Pod) SetInterPodHandler(handler InterPodCallHandler) {
	p.interPodHandler = handler
}

func (p *Pod) SetInterPodHandlerWithContract(handler InterPodCallHandlerWithContract) {
	p.interPodHandlerWithContract = handler
}

// Messages can be either responses (to our requests) or requests (inter-pod calls).
func (p *Pod) readResponses() {
	bufSize := 1024 * 1024 // 1MB default
	if p.spec.MaxResponseSize > 0 {
		bufSize = p.spec.MaxResponseSize + 1024 // overhead for JSON framing
	}
	scanner := bufio.NewScanner(p.stdout)
	scanner.Buffer(make([]byte, bufSize), bufSize)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var msg map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			p.logger.Warn("invalid JSON from pod", "error", err, "line", line)
			continue
		}

		if methodRaw, hasMethod := msg["method"]; hasMethod {
			var method string
			json.Unmarshal(methodRaw, &method)
			if method == "progress" {
				// Forward progress notification to subscribers
				if paramsRaw, ok := msg["params"]; ok {
					p.emitProgress(paramsRaw)
				}
				continue
			}
			p.handleInterPodRequest(line)
			continue
		}

		var resp jsonrpc.Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			p.logger.Warn("invalid JSON response from pod", "error", err, "line", line)
			continue
		}

		p.routeResponse(&resp)
	}

	if err := scanner.Err(); err != nil {
		if p.getState() == PodStateRunning {
			p.logger.Error("stdout read error", "error", err)
		}
	}
}

func (p *Pod) handleInterPodRequest(line string) {
	var req jsonrpc.Request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		p.logger.Warn("invalid inter-pod request", "error", err)
		return
	}

	if req.Method != "__mcp_meta_call__" {
		p.logger.Warn("unknown inter-pod method", "method", req.Method)
		p.sendInterPodError(req.ID, jsonrpc.CodeMethodNotFound, "unknown method: "+req.Method)
		return
	}

	if p.interPodHandler == nil && p.interPodHandlerWithContract == nil {
		p.logger.Warn("no inter-pod handler configured")
		p.sendInterPodError(req.ID, jsonrpc.CodeInternalError, "inter-pod calls not configured")
		return
	}

	var params struct {
		Tool     string                       `json:"tool"`
		Args     json.RawMessage              `json:"args"`
		Contract *jsonrpc.ContractExpectation `json:"contract,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		p.sendInterPodError(req.ID, jsonrpc.CodeInvalidParams, "invalid params: "+err.Error())
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var result json.RawMessage
		var err error

		// Use the contract-aware handler if available, otherwise fall back to legacy handler
		if p.interPodHandlerWithContract != nil {
			result, err = p.interPodHandlerWithContract(ctx, p.name, params.Tool, params.Args, params.Contract)
		} else if p.interPodHandler != nil {
			// Legacy handler - contract verification not supported
			if params.Contract != nil {
				p.logger.Warn("contract verification requested but not supported by handler")
			}
			result, err = p.interPodHandler(ctx, p.name, params.Tool, params.Args)
		}

		if err != nil {
			var jsonErr *jsonrpc.Error
			if errors.As(err, &jsonErr) {
				p.sendInterPodError(req.ID, jsonErr.Code, jsonErr.Message)
			}
			return
		}

		resp := &jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		}
		p.sendInterPodResponse(resp)
	}()
}

func (p *Pod) sendInterPodError(id jsonrpc.ID, code int, message string) {
	resp := &jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonrpc.Error{
			Code:    code,
			Message: message,
		},
	}
	p.sendInterPodResponse(resp)
}

func (p *Pod) sendInterPodResponse(resp *jsonrpc.Response) {
	p.encoderMu.Lock()
	defer p.encoderMu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		p.logger.Error("failed to marshal inter-pod response", "error", err)
		return
	}

	if _, err := p.stdin.Write(append(data, '\n')); err != nil {
		p.logger.Error("failed to write inter-pod response", "error", err)
	}
}

func (p *Pod) readStderr() {
	scanner := bufio.NewScanner(p.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		p.logger.Info("pod stderr", "line", line)
	}
}

func (p *Pod) monitor() {
	err := p.cmd.Wait()

	p.pendingMu.Lock()
	for id, ch := range p.pending {
		close(ch)
		delete(p.pending, id)
	}
	p.pendingMu.Unlock()

	currentState := p.getState()
	if err != nil {
		if currentState == PodStateStopping {
			p.state.Store(PodStateStopped)
			p.logger.Info("pod stopped", "exit_status", err.Error())
		} else {
			p.state.Store(PodStateCrashed)
			p.lastError.Store(err.Error())
			p.logger.Error("pod crashed", "error", err, "previous_state", currentState)
		}
	} else {
		p.state.Store(PodStateStopped)
		p.logger.Info("pod stopped")
	}

	close(p.done)
}

func (p *Pod) routeResponse(resp *jsonrpc.Response) {
	if resp.ID.IsNull() {
		p.logger.Warn("received response with null ID")
		return
	}

	id := resp.ID.Int()

	p.pendingMu.Lock()
	ch, ok := p.pending[id]
	if ok {
		delete(p.pending, id)
	}
	p.pendingMu.Unlock()

	if !ok {
		p.logger.Warn("received response for unknown request", "id", id)
		return
	}

	ch <- resp
}

func (p *Pod) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if p.getState() != PodStateRunning {
		return nil, fmt.Errorf("pod %s is not running (state: %s)", p.name, p.getState())
	}

	id := p.nextID.Add(1)
	reqID := jsonrpc.NewIntID(id)

	respCh := make(chan *jsonrpc.Response, 1)

	p.pendingMu.Lock()
	p.pending[id] = respCh
	p.pendingMu.Unlock()

	defer func() {
		p.pendingMu.Lock()
		delete(p.pending, id)
		p.pendingMu.Unlock()
	}()

	req := jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  method,
		Params:  params,
	}

	if err := p.sendRequest(&req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	p.callCount.Add(1)

	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		p.logger.Info("pod.Call waiting", "method", method, "id", id, "deadline_in", time.Until(deadline).Round(time.Second))
	} else {
		p.logger.Info("pod.Call waiting", "method", method, "id", id, "deadline", "none")
	}

	select {
	case resp, ok := <-respCh:
		if !ok {
			return nil, fmt.Errorf("pod %s died while waiting for response", p.name)
		}
		p.logger.Info("pod.Call response received", "method", method, "id", id)
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil

	case <-ctx.Done():
		p.logger.Warn("pod.Call context cancelled", "method", method, "id", id, "err", ctx.Err())
		return nil, ctx.Err()

	case <-p.done:
		return nil, fmt.Errorf("pod %s died while waiting for response", p.name)
	}
}

func (p *Pod) sendRequest(req *jsonrpc.Request) error {
	p.encoderMu.Lock()
	defer p.encoderMu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	if _, err := p.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}

	return nil
}

func (p *Pod) Stop(ctx context.Context) error {
	var stopErr error
	p.closeOnce.Do(func() {
		if p.healthCheckStop != nil {
			close(p.healthCheckStop)
		}

		shutdownCtx, cancel := context.WithTimeout(ctx, p.spec.ShutdownTimeout.Duration)
		defer cancel()

		_, err := p.Call(shutdownCtx, "shutdown", nil)
		if err != nil {
			p.logger.Warn("shutdown request failed", "error", err)
		}

		select {
		case <-p.done:
			return
		case <-shutdownCtx.Done():
			// Mark as stopping before kill to prevent error logs from stdout reader
			p.state.Store(PodStateStopping)
			p.logger.Warn("shutdown timeout, killing process")
			if p.cmd.Process != nil {
				if err := p.cmd.Process.Kill(); err != nil {
					stopErr = fmt.Errorf("kill process: %w", err)
				}
			}
		}

		<-p.done
	})
	return stopErr
}

func (p *Pod) Status() PodStatus {
	pid := 0
	if p.cmd != nil && p.cmd.Process != nil {
		pid = p.cmd.Process.Pid
	}

	lastErr := ""
	if v := p.lastError.Load(); v != nil {
		lastErr = v.(string)
	}

	return PodStatus{
		Name:      p.name,
		PID:       pid,
		State:     p.getState(),
		StartedAt: p.startedAt,
		CallCount: p.callCount.Load(),
		LastError: lastErr,
	}
}

func (p *Pod) getState() PodState {
	if v := p.state.Load(); v != nil {
		return v.(PodState)
	}
	return PodStateStopped
}

func (p *Pod) Name() string {
	return p.name
}
