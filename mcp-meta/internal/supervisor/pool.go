package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"mcp-meta/internal/metrics"
	"mcp-meta/internal/registry"
)

// PodPool manages a pool of pod replicas for horizontal scaling.
type PodPool struct {
	name    string
	spec    *registry.PodSpec
	logger  *slog.Logger
	runtime *RuntimeRegistry // Runtime registry for creating pods

	mu   sync.RWMutex
	pods []PodInterface // Changed from []*Pod to []PodInterface
	next atomic.Uint64  // Round-robin counter

	minSize int
	maxSize int

	interPodHandler             InterPodCallHandler
	interPodHandlerWithContract InterPodCallHandlerWithContract

	// For auto-scaling
	pendingCount   atomic.Int64
	autoScalerStop chan struct{}

	// Latency tracking for latency-based scaling
	latencyTracker *LatencyTracker
}

// NewPodPool creates a new pod pool with the given specification.
// If runtime is nil, a default subprocess runtime will be used.
func NewPodPool(name string, spec *registry.PodSpec, logger *slog.Logger, runtime *RuntimeRegistry) *PodPool {
	minSize := 1
	maxSize := 1

	if spec.Scaling != nil {
		if spec.Scaling.MinReplicas > 0 {
			minSize = spec.Scaling.MinReplicas
		}
		if spec.Scaling.MaxReplicas > 0 {
			maxSize = spec.Scaling.MaxReplicas
		}
		// Ensure maxSize >= minSize
		if maxSize < minSize {
			maxSize = minSize
		}
	}

	if logger == nil {
		logger = slog.Default()
	}

	// Create default runtime registry if not provided
	if runtime == nil {
		runtime = NewRuntimeRegistry(logger)
	}

	pool := &PodPool{
		name:    name,
		spec:    spec,
		logger:  logger.With("pool", name),
		runtime: runtime,
		pods:    make([]PodInterface, 0, minSize),
		minSize: minSize,
		maxSize: maxSize,
	}

	// Initialize latency tracker if latency-based scaling is configured
	if spec.Scaling != nil && spec.Scaling.Latency != nil {
		windowSize := spec.Scaling.Latency.WindowSize
		if windowSize <= 0 {
			windowSize = 100
		}
		pool.latencyTracker = NewLatencyTracker(windowSize)
	}

	return pool
}

// Start spawns the minimum number of pod replicas.
func (p *PodPool) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := 0; i < p.minSize; i++ {
		pod, err := p.spawnPodLocked(i)
		if err != nil {
			// Clean up any pods we already started
			for _, pod := range p.pods {
				pod.Stop(ctx)
			}
			p.pods = nil
			return fmt.Errorf("spawn replica %d: %w", i, err)
		}
		p.pods = append(p.pods, pod)
	}

	p.logger.Info("pod pool started", "replicas", len(p.pods))

	// Start auto-scaler if configured
	if p.spec.Scaling != nil && p.spec.Scaling.TargetPending > 0 {
		p.startAutoScaler(ctx)
	}

	return nil
}

// spawnPodLocked creates a new pod instance. Must be called with mu held.
func (p *PodPool) spawnPodLocked(replicaID int) (PodInterface, error) {
	// Create a copy of the spec with replica-specific env
	specCopy := *p.spec
	if specCopy.Env == nil {
		specCopy.Env = make(map[string]string)
	}
	specCopy.Env["POD_REPLICA_ID"] = fmt.Sprintf("%s-%d", p.name, replicaID)

	var pod PodInterface
	var err error

	// Use the appropriate runtime based on the spec
	if specCopy.RuntimeBackend != "" && specCopy.RuntimeBackend != "subprocess" {
		// Use drun-based runtime (docker, firecracker, vz, sandbox)
		rt := p.runtime.GetRuntime(&specCopy)
		pod, err = newRuntimePod(p.name, &specCopy, rt, p.logger)
	} else {
		// Use subprocess runtime (default)
		pod, err = newPod(p.name, &specCopy, p.logger)
	}

	if err != nil {
		return nil, err
	}

	metrics.PodSpawnsTotal.WithLabelValues(p.name).Inc()
	metrics.PodPoolSize.WithLabelValues(p.name).Set(float64(len(p.pods) + 1))

	if p.interPodHandlerWithContract != nil {
		pod.SetInterPodHandlerWithContract(p.interPodHandlerWithContract)
	} else if p.interPodHandler != nil {
		pod.SetInterPodHandler(p.interPodHandler)
	}

	return pod, nil
}

// Acquire returns a pod from the pool using round-robin selection.
// It skips crashed or unhealthy pods.
func (p *PodPool) Acquire() (PodInterface, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.pods) == 0 {
		return nil, fmt.Errorf("pool %s has no pods", p.name)
	}

	// Round-robin: get next pod index
	startIdx := p.next.Add(1) % uint64(len(p.pods))
	idx := startIdx

	// Try to find a healthy pod, cycling through all pods once
	for i := 0; i < len(p.pods); i++ {
		pod := p.pods[idx]
		if pod.getState() == PodStateRunning {
			return pod, nil
		}
		idx = (idx + 1) % uint64(len(p.pods))
	}

	return nil, fmt.Errorf("pool %s has no healthy pods", p.name)
}

// AcquireWithTracking returns a pod and a release function for pending count tracking.
// The release function should be called when the call completes to record latency.
func (p *PodPool) AcquireWithTracking() (PodInterface, func(), error) {
	pod, err := p.Acquire()
	if err != nil {
		return nil, nil, err
	}

	p.pendingCount.Add(1)

	release := func() {
		p.pendingCount.Add(-1)
	}

	return pod, release, nil
}

// AcquireWithLatencyTracking returns a pod and a release function that records latency.
// Call the release function with the latency in milliseconds when the call completes.
func (p *PodPool) AcquireWithLatencyTracking() (PodInterface, func(latencyMs int64), error) {
	pod, err := p.Acquire()
	if err != nil {
		return nil, nil, err
	}

	p.pendingCount.Add(1)

	release := func(latencyMs int64) {
		p.pendingCount.Add(-1)
		if p.latencyTracker != nil {
			p.latencyTracker.Record(latencyMs)
		}
	}

	return pod, release, nil
}

// RecordLatency records a latency measurement for this pool.
func (p *PodPool) RecordLatency(latencyMs int64) {
	if p.latencyTracker != nil {
		p.latencyTracker.Record(latencyMs)
	}
}

// GetLatencyPercentiles returns the P50, P95, and P99 latencies for this pool.
func (p *PodPool) GetLatencyPercentiles() (p50, p95, p99 int64) {
	if p.latencyTracker == nil {
		return 0, 0, 0
	}
	return p.latencyTracker.Percentiles()
}

// Size returns current replica count.
func (p *PodPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.pods)
}

// HealthyCount returns the number of healthy (running) pods.
func (p *PodPool) HealthyCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, pod := range p.pods {
		if pod.getState() == PodStateRunning {
			count++
		}
	}
	return count
}

// PendingCount returns the current number of in-flight requests.
func (p *PodPool) PendingCount() int64 {
	return p.pendingCount.Load()
}

// ScaleUp adds a new replica (if under max).
func (p *PodPool) ScaleUp(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.pods) >= p.maxSize {
		return fmt.Errorf("pool %s at max size %d", p.name, p.maxSize)
	}

	replicaID := len(p.pods)
	pod, err := p.spawnPodLocked(replicaID)
	if err != nil {
		return fmt.Errorf("spawn new replica: %w", err)
	}

	p.pods = append(p.pods, pod)
	p.logger.Info("scaled up", "replicas", len(p.pods))
	return nil
}

// ScaleDown removes a replica (if above min).
func (p *PodPool) ScaleDown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.pods) <= p.minSize {
		return fmt.Errorf("pool %s at min size %d", p.name, p.minSize)
	}

	// Remove last pod
	pod := p.pods[len(p.pods)-1]
	p.pods = p.pods[:len(p.pods)-1]

	// Stop async to not block scaling decisions
	go func() {
		if err := pod.Stop(ctx); err != nil {
			p.logger.Warn("error stopping scaled-down pod", "error", err)
		}
	}()

	p.logger.Info("scaled down", "replicas", len(p.pods))
	return nil
}

// Shutdown stops all pods in the pool.
func (p *PodPool) Shutdown(ctx context.Context) error {
	// Stop auto-scaler first
	if p.autoScalerStop != nil {
		close(p.autoScalerStop)
	}

	p.mu.Lock()
	pods := p.pods
	p.pods = nil
	p.mu.Unlock()

	var firstErr error
	for _, pod := range pods {
		if err := pod.Stop(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	p.logger.Info("pod pool shut down")
	return firstErr
}

// SetInterPodHandler sets the handler for inter-pod calls.
func (p *PodPool) SetInterPodHandler(h InterPodCallHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.interPodHandler = h
	for _, pod := range p.pods {
		pod.SetInterPodHandler(h)
	}
}

// SetInterPodHandlerWithContract sets the contract-aware inter-pod handler.
func (p *PodPool) SetInterPodHandlerWithContract(h InterPodCallHandlerWithContract) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.interPodHandlerWithContract = h
	for _, pod := range p.pods {
		pod.SetInterPodHandlerWithContract(h)
	}
}

// SubscribeProgress subscribes to progress notifications from all pods in the pool.
func (p *PodPool) SubscribeProgress() (<-chan json.RawMessage, func()) {
	merged := make(chan json.RawMessage, 64)
	p.mu.RLock()
	var cancels []func()
	for _, pod := range p.pods {
		ch, cancel := pod.SubscribeProgress()
		cancels = append(cancels, cancel)
		go func(c <-chan json.RawMessage) {
			for msg := range c {
				select {
				case merged <- msg:
				default:
				}
			}
		}(ch)
	}
	p.mu.RUnlock()
	return merged, func() {
		for _, c := range cancels {
			c()
		}
		close(merged)
	}
}

// Status returns the status of all pods in the pool.
func (p *PodPool) Status() []PodStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	statuses := make([]PodStatus, 0, len(p.pods))
	for _, pod := range p.pods {
		statuses = append(statuses, pod.Status())
	}
	return statuses
}

// startAutoScaler starts a goroutine that monitors pending requests and scales.
func (p *PodPool) startAutoScaler(ctx context.Context) {
	if p.spec.Scaling == nil {
		return
	}

	// Need either targetPending or latency config to enable auto-scaling
	hasLatencyConfig := p.spec.Scaling.Latency != nil && (p.spec.Scaling.Latency.TargetP50 > 0 ||
		p.spec.Scaling.Latency.TargetP95 > 0 ||
		p.spec.Scaling.Latency.TargetP99 > 0)
	hasPendingConfig := p.spec.Scaling.TargetPending > 0

	if !hasLatencyConfig && !hasPendingConfig {
		return
	}

	p.autoScalerStop = make(chan struct{})
	targetPending := p.spec.Scaling.TargetPending

	scaleDownDelay := 5 * time.Minute // default
	if p.spec.Scaling.ScaleDownDelay.Duration > 0 {
		scaleDownDelay = p.spec.Scaling.ScaleDownDelay.Duration
	}

	// Latency-based scaling thresholds
	var scaleUpViolations int
	var scaleDownGoodReadings int
	scaleUpThreshold := 3
	scaleDownThreshold := 10

	if p.spec.Scaling.Latency != nil {
		if p.spec.Scaling.Latency.ScaleUpThreshold > 0 {
			scaleUpThreshold = p.spec.Scaling.Latency.ScaleUpThreshold
		}
		if p.spec.Scaling.Latency.ScaleDownThreshold > 0 {
			scaleDownThreshold = p.spec.Scaling.Latency.ScaleDownThreshold
		}
	}

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		var lastScaleDown time.Time

		for {
			select {
			case <-ctx.Done():
				return
			case <-p.autoScalerStop:
				return
			case <-ticker.C:
				pending := p.pendingCount.Load()
				currentSize := p.Size()
				shouldScaleUp := false
				shouldScaleDown := true // Start optimistic

				// Check pending-based scaling
				if hasPendingConfig {
					if pending > int64(targetPending*currentSize) {
						shouldScaleUp = true
					}
					if pending >= int64(targetPending*currentSize/2) {
						shouldScaleDown = false
					}
				}

				// Check latency-based scaling
				if hasLatencyConfig && p.latencyTracker != nil {
					latencySpec := p.spec.Scaling.Latency
					p50, p95, p99 := p.latencyTracker.Percentiles()

					// Check if any latency target is violated
					violated := false
					if latencySpec.TargetP50 > 0 && p50 > int64(latencySpec.TargetP50) {
						violated = true
					}
					if latencySpec.TargetP95 > 0 && p95 > int64(latencySpec.TargetP95) {
						violated = true
					}
					if latencySpec.TargetP99 > 0 && p99 > int64(latencySpec.TargetP99) {
						violated = true
					}

					if violated {
						scaleUpViolations++
						scaleDownGoodReadings = 0
						if scaleUpViolations >= scaleUpThreshold {
							shouldScaleUp = true
						}
					} else {
						scaleUpViolations = 0
						scaleDownGoodReadings++
						// Only scale down if latency is consistently good
						if scaleDownGoodReadings < scaleDownThreshold {
							shouldScaleDown = false
						}
					}

					p.logger.Debug("latency auto-scaler check",
						"p50", p50, "p95", p95, "p99", p99,
						"violations", scaleUpViolations,
						"goodReadings", scaleDownGoodReadings)
				}

				// Execute scaling decisions
				if shouldScaleUp && currentSize < p.maxSize {
					if err := p.ScaleUp(ctx); err != nil {
						p.logger.Debug("auto-scale up failed", "error", err)
					} else {
						scaleUpViolations = 0
					}
					continue
				}

				// Scale down if all conditions are met
				if shouldScaleDown &&
					currentSize > p.minSize &&
					time.Since(lastScaleDown) > scaleDownDelay {
					if err := p.ScaleDown(ctx); err != nil {
						p.logger.Debug("auto-scale down failed", "error", err)
					} else {
						lastScaleDown = time.Now()
						scaleDownGoodReadings = 0
					}
				}
			}
		}
	}()
}

// LatencyTracker tracks request latencies for percentile calculation.
type LatencyTracker struct {
	mu         sync.RWMutex
	latencies  []int64 // Ring buffer of latencies in ms
	windowSize int
	idx        int
	count      int
}

// NewLatencyTracker creates a new latency tracker with the given window size.
func NewLatencyTracker(windowSize int) *LatencyTracker {
	if windowSize <= 0 {
		windowSize = 100
	}
	return &LatencyTracker{
		latencies:  make([]int64, windowSize),
		windowSize: windowSize,
	}
}

// Record adds a latency measurement in milliseconds.
func (lt *LatencyTracker) Record(latencyMs int64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	lt.latencies[lt.idx] = latencyMs
	lt.idx = (lt.idx + 1) % lt.windowSize
	if lt.count < lt.windowSize {
		lt.count++
	}
}

// Percentiles returns the P50, P95, and P99 latencies.
func (lt *LatencyTracker) Percentiles() (p50, p95, p99 int64) {
	lt.mu.RLock()
	defer lt.mu.RUnlock()

	if lt.count == 0 {
		return 0, 0, 0
	}

	// Copy and sort the latencies
	sorted := make([]int64, lt.count)
	copy(sorted, lt.latencies[:lt.count])
	sortInt64s(sorted)

	p50 = sorted[lt.count*50/100]
	p95 = sorted[lt.count*95/100]
	p99Idx := lt.count * 99 / 100
	if p99Idx >= lt.count {
		p99Idx = lt.count - 1
	}
	p99 = sorted[p99Idx]

	return p50, p95, p99
}

// Count returns the number of recorded latencies.
func (lt *LatencyTracker) Count() int {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	return lt.count
}

// sortInt64s sorts a slice of int64 using insertion sort (efficient for small slices).
func sortInt64s(s []int64) {
	for i := 1; i < len(s); i++ {
		v := s[i]
		j := i - 1
		for j >= 0 && s[j] > v {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = v
	}
}
