package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcp-meta/internal/correlation"
	"mcp-meta/internal/jsonrpc"
	"mcp-meta/internal/metrics"
	"mcp-meta/internal/registry"
	"mcp-meta/internal/resilience"
	"mcp-meta/internal/supervisor"
)

// CostTracker records per-tool costs for budget billing.
// Implemented by a2a.MemoryTaskStore — defined here to avoid import cycle.
type CostTracker interface {
	RecordCost(contextID, toolFQN, podName string, costStroops int64)
}

type Router interface {
	CallTool(ctx context.Context, toolName string, args json.RawMessage) (json.RawMessage, error)
	InternalCall(ctx context.Context, sourcePod string, targetTool string, args json.RawMessage) (json.RawMessage, error)
	InternalCallWithContract(ctx context.Context, sourcePod string, targetTool string, args json.RawMessage, expect *jsonrpc.ContractExpectation) (json.RawMessage, error)

	// SetWarmPoolManager Warm pool management
	SetWarmPoolManager(wpm *supervisor.WarmPoolManager)
	GetWarmPoolManager() *supervisor.WarmPoolManager
	GetWarmPoolStatus() map[string]*supervisor.WarmPoolStatus

	// SetCostTracker Cost tracking for budget billing
	SetCostTracker(ct CostTracker)

	// SubscribePoolProgress subscribes to progress notifications from a pod pool.
	// Returns nil channels if the pool doesn't exist.
	SubscribePoolProgress(poolName string) (events <-chan json.RawMessage, cancel func())
}

const defaultMaxResponseSize = 1 << 20 // 1MB

type router struct {
	registry        registry.Registry
	supervisor      supervisor.Supervisor
	warmPoolManager *supervisor.WarmPoolManager
	costTracker     CostTracker
	logger          *slog.Logger
	breakers        map[string]*resilience.CircuitBreaker
	breakersMu      sync.Mutex
}

// SetCostTracker sets the cost tracker for recording per-tool costs.
func (r *router) SetCostTracker(ct CostTracker) {
	r.costTracker = ct
}

func New(reg registry.Registry, sup supervisor.Supervisor, logger *slog.Logger) Router {
	return &router{
		registry:   reg,
		supervisor: sup,
		logger:     logger,
		breakers:   make(map[string]*resilience.CircuitBreaker),
	}
}

// NewWithWarmPool creates a router with warm pool management enabled.
func NewWithWarmPool(reg registry.Registry, sup supervisor.Supervisor, wpm *supervisor.WarmPoolManager, logger *slog.Logger) Router {
	return &router{
		registry:        reg,
		supervisor:      sup,
		warmPoolManager: wpm,
		logger:          logger,
		breakers:        make(map[string]*resilience.CircuitBreaker),
	}
}

// SetWarmPoolManager sets the warm pool manager for the router.
func (r *router) SetWarmPoolManager(wpm *supervisor.WarmPoolManager) {
	r.warmPoolManager = wpm
}

// GetWarmPoolManager returns the warm pool manager.
func (r *router) GetWarmPoolManager() *supervisor.WarmPoolManager {
	return r.warmPoolManager
}

// GetWarmPoolStatus returns the status of all managed warm pools.
func (r *router) GetWarmPoolStatus() map[string]*supervisor.WarmPoolStatus {
	if r.warmPoolManager == nil {
		return nil
	}
	return r.warmPoolManager.Status()
}

// SubscribePoolProgress subscribes to progress notifications from the supervisor's global bus.
// The poolName parameter is currently unused — all progress events are received.
func (r *router) SubscribePoolProgress(_ string) (<-chan json.RawMessage, func()) {
	return r.supervisor.SubscribeProgress()
}

func (r *router) CallTool(ctx context.Context, toolName string, args json.RawMessage) (json.RawMessage, error) {
	log := correlation.ContextLogger(ctx, r.logger)

	podSpec, toolDef, err := r.registry.ResolveTool(toolName)
	if err != nil {
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeMethodNotFound,
			Message: fmt.Sprintf("tool not found: %s", toolName),
		}
	}

	timeout := podSpec.CallTimeout.Duration
	if toolDef.Timeout.Duration > 0 {
		timeout = toolDef.Timeout.Duration
	}

	// If no deadline on the incoming context, set a generous outer deadline
	// to prevent the entire call (including retries) from hanging forever.
	// Individual attempt timeouts are set per-retry below.
	retryCfg := r.buildRetryConfig(podSpec, toolDef)
	outerTimeout := timeout * time.Duration(retryCfg.MaxRetries+1) * 2
	outerTimeout = max(outerTimeout, timeout)
	if deadline, ok := ctx.Deadline(); !ok || deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, outerTimeout)
		defer cancel()
	}

	// Register pool with warm pool manager if enabled
	if r.warmPoolManager != nil {
		r.warmPoolManager.RegisterPool(podSpec.Name, podSpec)
	}

	// Get or create the pool for this pod
	pool, err := r.supervisor.EnsurePool(ctx, podSpec.Name, podSpec)
	if err != nil {
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodePodSpawnFailed,
			Message: fmt.Sprintf("failed to spawn pod pool %s: %v", podSpec.Name, err),
		}
	}

	// Get circuit breaker for this pod type
	cb := r.getOrCreateBreaker(podSpec.Name, podSpec.CircuitBreaker)

	start := time.Now()
	var result json.RawMessage
	attempt := 0

	callErr := resilience.Retry(ctx, retryCfg, func(ctx context.Context) error {
		if attempt > 0 {
			metrics.RetryTotal.WithLabelValues(toolName, strconv.Itoa(attempt)).Inc()
			log.Info("retrying tool call", "tool", toolName, "attempt", attempt)
		}
		attempt++

		// Per-attempt timeout so each retry gets a fresh deadline
		attemptCtx, attemptCancel := context.WithTimeout(ctx, timeout)
		defer attemptCancel()

		// Circuit breaker check
		return cb.Execute(attemptCtx, func(ctx context.Context) error {
			// Track timing for cold start detection
			acquireStart := time.Now()

			pod, release, acqErr := pool.AcquireWithTracking()
			if acqErr != nil {
				return &jsonrpc.Error{
					Code:    jsonrpc.CodePodError,
					Message: fmt.Sprintf("no available pods in pool %s: %v", podSpec.Name, acqErr),
				}
			}

			acquireLatencyMs := time.Since(acquireStart).Milliseconds()
			defer func() {
				release()
				if r.warmPoolManager != nil {
					wasColdStart := acquireLatencyMs > 100
					r.warmPoolManager.RecordCall(podSpec.Name, acquireLatencyMs, wasColdStart)
				}
			}()

			res, callErr := pod.Call(ctx, toolDef.Name, args)
			if callErr != nil {
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return &jsonrpc.Error{
						Code:    jsonrpc.CodePodTimeout,
						Message: fmt.Sprintf("tool %s timed out after %v", toolName, timeout),
					}
				}
				if pod.Status().State == supervisor.PodStateCrashed {
					return &jsonrpc.Error{
						Code:    jsonrpc.CodePodCrashed,
						Message: fmt.Sprintf("pod %s crashed: %v", podSpec.Name, callErr),
					}
				}
				var jsonErr *jsonrpc.Error
				if errors.As(callErr, &jsonErr) {
					return jsonErr
				}
				return &jsonrpc.Error{
					Code:    jsonrpc.CodePodError,
					Message: callErr.Error(),
				}
			}

			result = res
			return nil
		})
	})

	// Record metrics
	duration := time.Since(start).Seconds()
	metrics.ToolCallDuration.WithLabelValues(toolName).Observe(duration)

	if callErr != nil {
		status := classifyErrorStatus(callErr)
		metrics.ToolCallsTotal.WithLabelValues(toolName, status).Inc()

		// Update circuit breaker state metric
		metrics.CircuitBreakerState.WithLabelValues(podSpec.Name).Set(float64(cb.State()))

		return nil, callErr
	}

	metrics.ToolCallsTotal.WithLabelValues(toolName, "success").Inc()
	metrics.CircuitBreakerState.WithLabelValues(podSpec.Name).Set(float64(cb.State()))

	// Record cost for budget billing.
	// Cost per call defaults to 0 — FastAPI reads actual rates from the Soroban
	// tool-registry contract and uses them for settlement calculations.
	// This ledger tracks which tools were called and by which pod.
	if r.costTracker != nil {
		contextID := GetContextID(ctx)
		if contextID != "" {
			r.costTracker.RecordCost(contextID, toolName, podSpec.Name, 0)
		}
	}

	// Enforce output size limits
	result = r.enforceResponseLimit(log, toolName, result, podSpec, toolDef)

	return result, nil
}

// getOrCreateBreaker returns the circuit breaker for a pod type, creating one if needed.
func (r *router) getOrCreateBreaker(podName string, spec *registry.CircuitBreakerSpec) *resilience.CircuitBreaker {
	r.breakersMu.Lock()
	defer r.breakersMu.Unlock()

	if cb, ok := r.breakers[podName]; ok {
		return cb
	}

	cfg := resilience.DefaultCircuitBreakerConfig()
	if spec != nil {
		if spec.FailureThreshold > 0 {
			cfg.FailureThreshold = spec.FailureThreshold
		}
		if spec.ResetTimeout.Duration > 0 {
			cfg.ResetTimeout = spec.ResetTimeout.Duration
		}
		if spec.SuccessThreshold > 0 {
			cfg.SuccessThreshold = spec.SuccessThreshold
		}
	}

	cb := resilience.NewCircuitBreaker(podName, cfg, r.logger)
	r.breakers[podName] = cb
	return cb
}

// buildRetryConfig builds retry configuration from pod spec and tool def.
func (r *router) buildRetryConfig(podSpec *registry.PodSpec, toolDef *registry.ToolDef) resilience.RetryConfig {
	cfg := resilience.DefaultRetryConfig()

	// Pod-level defaults
	if podSpec.Retry != nil {
		if podSpec.Retry.MaxRetries > 0 {
			cfg.MaxRetries = podSpec.Retry.MaxRetries
		}
		if podSpec.Retry.BaseDelay.Duration > 0 {
			cfg.BaseDelay = podSpec.Retry.BaseDelay.Duration
		}
		if podSpec.Retry.MaxDelay.Duration > 0 {
			cfg.MaxDelay = podSpec.Retry.MaxDelay.Duration
		}
	}

	// Tool-level overrides
	if toolDef.MaxRetries > 0 {
		cfg.MaxRetries = toolDef.MaxRetries
	}
	if toolDef.RetryBaseDelay.Duration > 0 {
		cfg.BaseDelay = toolDef.RetryBaseDelay.Duration
	}

	return cfg
}

// enforceResponseLimit truncates responses that exceed the configured max size.
func (r *router) enforceResponseLimit(log *slog.Logger, toolName string, result json.RawMessage, podSpec *registry.PodSpec, toolDef *registry.ToolDef) json.RawMessage {
	maxSize := podSpec.MaxResponseSize
	if toolDef.MaxResponseSize > 0 {
		maxSize = toolDef.MaxResponseSize
	}
	if maxSize <= 0 {
		maxSize = defaultMaxResponseSize
	}

	if len(result) <= maxSize {
		return result
	}

	log.Warn("response truncated",
		"tool", toolName,
		"original_size", len(result),
		"max_size", maxSize,
	)

	wrapper := map[string]any{
		"_truncated":     true,
		"_original_size": len(result),
		"_max_size":      maxSize,
		"data":           string(result[:maxSize]),
	}
	wrapped, _ := json.Marshal(wrapper)
	return wrapped
}

// classifyErrorStatus returns a metrics-friendly status label for an error.
func classifyErrorStatus(err error) string {
	if errors.Is(err, resilience.ErrCircuitOpen) {
		return "circuit_open"
	}
	var jsonErr *jsonrpc.Error
	if errors.As(err, &jsonErr) {
		switch jsonErr.Code {
		case jsonrpc.CodePodTimeout:
			return "timeout"
		case jsonrpc.CodePodCrashed:
			return "crashed"
		case jsonrpc.CodeMethodNotFound:
			return "not_found"
		default:
			return "error"
		}
	}
	return "error"
}

func (r *router) InternalCall(ctx context.Context, sourcePod string, targetTool string, args json.RawMessage) (json.RawMessage, error) {
	return r.InternalCallWithContract(ctx, sourcePod, targetTool, args, nil)
}

func (r *router) InternalCallWithContract(ctx context.Context, sourcePod string, targetTool string, args json.RawMessage, expect *jsonrpc.ContractExpectation) (json.RawMessage, error) {
	podSpec, toolDef, err := r.registry.ResolveTool(targetTool)
	if err != nil {
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeMethodNotFound,
			Message: fmt.Sprintf("tool not found: %s", targetTool),
		}
	}

	if expect != nil {
		if err := verifyContract(toolDef, expect); err != nil {
			return nil, err
		}
	}

	stack := GetCallStack(ctx)
	if detectCircular(stack, podSpec.Name) {
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeCircularCall,
			Message: fmt.Sprintf("circular call detected: %v -> %s", stack, podSpec.Name),
		}
	}

	newStack := append(stack, sourcePod)
	ctx = WithCallStack(ctx, newStack)

	return r.CallTool(ctx, targetTool, args)
}

func verifyContract(toolDef *registry.ToolDef, expect *jsonrpc.ContractExpectation) error {
	if expect == nil {
		return nil
	}

	if toolDef.Contract == nil {
		if expect.ID != "" || expect.SchemaHash != "" {
			return &jsonrpc.Error{
				Code:    jsonrpc.CodeContractMismatch,
				Message: fmt.Sprintf("contract mismatch: tool %s has no contract defined", toolDef.Name),
				Data:    buildContractMismatchData(nil, expect),
			}
		}
		return nil
	}

	actual := toolDef.Contract

	if expect.ID != "" && actual.ID != expect.ID {
		return &jsonrpc.Error{
			Code:    jsonrpc.CodeContractMismatch,
			Message: fmt.Sprintf("contract mismatch: expected ID %q, got %q", expect.ID, actual.ID),
			Data:    buildContractMismatchData(actual, expect),
		}
	}

	if expect.SchemaHash != "" && actual.SchemaHash != expect.SchemaHash {
		return &jsonrpc.Error{
			Code:    jsonrpc.CodeContractMismatch,
			Message: fmt.Sprintf("contract mismatch: schema hash mismatch for %s", toolDef.Name),
			Data:    buildContractMismatchData(actual, expect),
		}
	}

	if expect.Version != "" && !versionMatches(actual.Version, expect.Version) {
		return &jsonrpc.Error{
			Code:    jsonrpc.CodeContractMismatch,
			Message: fmt.Sprintf("contract mismatch: expected version %q, got %q", expect.Version, actual.Version),
			Data:    buildContractMismatchData(actual, expect),
		}
	}

	return nil
}

func buildContractMismatchData(actual *registry.ToolContract, expect *jsonrpc.ContractExpectation) json.RawMessage {
	data := map[string]any{
		"expected": expect,
	}
	if actual != nil {
		data["actual"] = actual
	}
	jsonData, _ := json.Marshal(data)
	return jsonData
}

// Supports: exact match, caret (^1.0.0), tilde (~1.2.0), and >= / > constraints.
func versionMatches(actualVersion, expectedConstraint string) bool {
	if actualVersion == "" {
		return false
	}

	// Exact match
	if !strings.HasPrefix(expectedConstraint, "^") &&
		!strings.HasPrefix(expectedConstraint, "~") &&
		!strings.HasPrefix(expectedConstraint, ">=") &&
		!strings.HasPrefix(expectedConstraint, ">") {
		return actualVersion == expectedConstraint
	}

	actualParts := parseVersion(actualVersion)
	if actualParts == nil {
		return false
	}

	// Caret: ^1.0.0 matches any 1.x.x where minor/patch >= expected
	if constraint, found := strings.CutPrefix(expectedConstraint, "^"); found {
		expectedParts := parseVersion(constraint)
		if expectedParts == nil {
			return false
		}
		if actualParts[0] != expectedParts[0] {
			return false
		}
		if actualParts[1] < expectedParts[1] {
			return false
		}
		if actualParts[1] == expectedParts[1] && actualParts[2] < expectedParts[2] {
			return false
		}
		return true
	}

	// Tilde: ~1.2.0 matches any 1.2.x where patch >= expected
	if constraint, found := strings.CutPrefix(expectedConstraint, "~"); found {
		expectedParts := parseVersion(constraint)
		if expectedParts == nil {
			return false
		}
		if actualParts[0] != expectedParts[0] || actualParts[1] != expectedParts[1] {
			return false
		}
		return actualParts[2] >= expectedParts[2]
	}

	if constraint, found := strings.CutPrefix(expectedConstraint, ">="); found {
		expectedParts := parseVersion(constraint)
		if expectedParts == nil {
			return false
		}
		return compareVersions(actualParts, expectedParts) >= 0
	}

	if constraint, found := strings.CutPrefix(expectedConstraint, ">"); found {
		expectedParts := parseVersion(constraint)
		if expectedParts == nil {
			return false
		}
		return compareVersions(actualParts, expectedParts) > 0
	}

	return false
}

func parseVersion(v string) []int {
	parts := strings.Split(v, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return nil
	}

	result := make([]int, 3)
	for i, p := range parts {
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err != nil {
			return nil
		}
		result[i] = n
	}
	return result
}

func compareVersions(a, b []int) int {
	for i := range 3 {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

type callStackKey struct{}

func GetCallStack(ctx context.Context) []string {
	if stack, ok := ctx.Value(callStackKey{}).([]string); ok {
		return stack
	}
	return nil
}

func WithCallStack(ctx context.Context, stack []string) context.Context {
	return context.WithValue(ctx, callStackKey{}, stack)
}

func detectCircular(stack []string, podName string) bool {
	return slices.Contains(stack, podName)
}

// contextIDKey propagates A2A contextId through inter-pod calls.
type contextIDKey struct{}

// GetContextID extracts the A2A contextId from the context.
func GetContextID(ctx context.Context) string {
	if id, ok := ctx.Value(contextIDKey{}).(string); ok {
		return id
	}
	return ""
}

// WithContextID returns a context carrying the A2A contextId.
func WithContextID(ctx context.Context, contextID string) context.Context {
	return context.WithValue(ctx, contextIDKey{}, contextID)
}
