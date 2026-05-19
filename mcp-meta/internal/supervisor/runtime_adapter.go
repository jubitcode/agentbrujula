package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"

	"mcp-meta/internal/jsonrpc"
	"mcp-meta/internal/registry"
	"mcp-meta/internal/runtime"
)

// RuntimePod wraps a runtime.Instance as a Pod-compatible interface.
// This allows drun-based instances to be used seamlessly with PodPool.
type RuntimePod struct {
	instance runtime.Instance
	name     string
	spec     *registry.PodSpec
	logger   *slog.Logger

	state     atomic.Value
	callCount atomic.Int64
	lastError atomic.Value
}

// newRuntimePod creates a Pod from a runtime.Instance.
func newRuntimePod(name string, spec *registry.PodSpec, rt runtime.Runtime, logger *slog.Logger) (*RuntimePod, error) {
	instanceSpec := &runtime.InstanceSpec{
		Name:    name,
		PodSpec: spec,
		Image:   spec.Image,
		Resources: runtime.ParseResourceLimits(spec.Resources),
		Env:     spec.Env,
	}

	ctx, cancel := context.WithTimeout(context.Background(), spec.StartupTimeout.Duration)
	defer cancel()

	instance, err := rt.Spawn(ctx, instanceSpec)
	if err != nil {
		return nil, err
	}

	rp := &RuntimePod{
		instance: instance,
		name:     name,
		spec:     spec,
		logger:   logger.With("pod", name, "runtime", rt.Name()),
	}
	rp.state.Store(PodStateRunning)
	rp.lastError.Store("")

	rp.logger.Info("runtime pod started", "instance_id", instance.ID())

	return rp, nil
}

func (p *RuntimePod) Name() string {
	return p.name
}

func (p *RuntimePod) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if p.getState() != PodStateRunning {
		return nil, fmt.Errorf("pod %s is not running (state: %s)", p.name, p.getState())
	}

	p.callCount.Add(1)
	result, err := p.instance.Call(ctx, method, params)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (p *RuntimePod) Stop(ctx context.Context) error {
	p.state.Store(PodStateStopping)
	err := p.instance.Stop(ctx)
	if err != nil {
		p.state.Store(PodStateCrashed)
		p.lastError.Store(err.Error())
		return err
	}
	p.state.Store(PodStateStopped)
	return nil
}

func (p *RuntimePod) Status() PodStatus {
	status := p.instance.Status()

	lastErr := ""
	if v := p.lastError.Load(); v != nil {
		lastErr = v.(string)
	}
	if status.LastError != "" {
		lastErr = status.LastError
	}

	return PodStatus{
		Name:      p.name,
		PID:       status.PID,
		State:     p.getState(),
		StartedAt: status.StartedAt,
		CallCount: p.callCount.Load(),
		LastError: lastErr,
	}
}

func (p *RuntimePod) SetInterPodHandler(handler InterPodCallHandler) {
	// Wrap the handler for the runtime instance
	p.instance.SetInterPodHandler(func(ctx context.Context, sourcePod, targetTool string, args json.RawMessage, contract *runtime.ContractExpectation) (json.RawMessage, error) {
		return handler(ctx, sourcePod, targetTool, args)
	})
}

func (p *RuntimePod) SetInterPodHandlerWithContract(handler InterPodCallHandlerWithContract) {
	p.instance.SetInterPodHandler(func(ctx context.Context, sourcePod, targetTool string, args json.RawMessage, contract *runtime.ContractExpectation) (json.RawMessage, error) {
		var jrpcContract *jsonrpc.ContractExpectation
		if contract != nil {
			jrpcContract = &jsonrpc.ContractExpectation{
				ID:         contract.ID,
				Version:    contract.Version,
				SchemaHash: contract.SchemaHash,
			}
		}
		return handler(ctx, sourcePod, targetTool, args, jrpcContract)
	})
}

// SubscribeProgress is a no-op for runtime pods (progress notifications are
// only supported by subprocess pods via stdout).
func (p *RuntimePod) SubscribeProgress() (<-chan json.RawMessage, func()) {
	ch := make(chan json.RawMessage)
	close(ch)
	return ch, func() {}
}

func (p *RuntimePod) getState() PodState {
	if v := p.state.Load(); v != nil {
		return v.(PodState)
	}

	// Sync state from instance
	status := p.instance.Status()
	switch status.State {
	case runtime.StateStarting:
		return PodStateStarting
	case runtime.StateRunning:
		return PodStateRunning
	case runtime.StateStopping:
		return PodStateStopping
	case runtime.StateStopped:
		return PodStateStopped
	case runtime.StateCrashed:
		return PodStateCrashed
	default:
		return PodStateStopped
	}
}

// PodInterface defines the common interface for both Pod and RuntimePod.
// This allows PodPool to work with either type.
type PodInterface interface {
	Name() string
	Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
	Stop(ctx context.Context) error
	Status() PodStatus
	SetInterPodHandler(handler InterPodCallHandler)
	SetInterPodHandlerWithContract(handler InterPodCallHandlerWithContract)
	SubscribeProgress() (<-chan json.RawMessage, func())
	getState() PodState
}

// Verify both types implement PodInterface
var _ PodInterface = (*Pod)(nil)
var _ PodInterface = (*RuntimePod)(nil)

// RuntimeRegistry holds available runtimes for the supervisor.
type RuntimeRegistry struct {
	subprocess runtime.Runtime
	drun       map[string]runtime.Runtime // backend type -> runtime
}

// NewRuntimeRegistry creates a new runtime registry with subprocess as default.
func NewRuntimeRegistry(logger *slog.Logger) *RuntimeRegistry {
	return &RuntimeRegistry{
		subprocess: runtime.NewSubprocessRuntime(logger),
		drun:       make(map[string]runtime.Runtime),
	}
}

// RegisterDrunBackend registers a drun backend runtime.
func (r *RuntimeRegistry) RegisterDrunBackend(backendType string, rt runtime.Runtime) {
	r.drun[backendType] = rt
}

// GetRuntime returns the appropriate runtime for the given PodSpec.
func (r *RuntimeRegistry) GetRuntime(spec *registry.PodSpec) runtime.Runtime {
	if spec.RuntimeBackend == "" || spec.RuntimeBackend == "subprocess" {
		return r.subprocess
	}

	if rt, ok := r.drun[spec.RuntimeBackend]; ok {
		return rt
	}

	// Fallback to subprocess
	return r.subprocess
}

// Close closes all runtimes.
func (r *RuntimeRegistry) Close() error {
	if r.subprocess != nil {
		r.subprocess.Close()
	}
	for _, rt := range r.drun {
		rt.Close()
	}
	return nil
}
