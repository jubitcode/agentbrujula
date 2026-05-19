// Package supervisor provides warm pool management for MCP pods.
// The WarmPoolManager enables:
// - Scale-to-zero after idle timeout
// - Pre-warming based on usage patterns
// - Cold start metrics tracking
// - Predictive scaling based on historical patterns
package supervisor

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"mcp-meta/internal/registry"
)

// WarmPoolConfig extends ScalingSpec with warm pool specific configuration.
type WarmPoolConfig struct {
	// IdleTimeout is how long a pool can be idle before scaling to zero.
	// Set to 0 to disable scale-to-zero.
	IdleTimeout time.Duration

	// WarmupLeadTime is how far in advance to pre-warm before predicted demand.
	WarmupLeadTime time.Duration

	// MinWarmPods is the minimum number of warm pods to keep even during idle.
	// Set to 0 to allow full scale-to-zero.
	MinWarmPods int

	// ColdStartTarget is the target cold start latency in milliseconds.
	// Used for monitoring and alerting.
	ColdStartTarget time.Duration

	// EnablePredictiveScaling enables scaling based on historical patterns.
	EnablePredictiveScaling bool

	// UsageHistoryWindow is how much history to keep for pattern detection.
	UsageHistoryWindow time.Duration
}

// DefaultWarmPoolConfig returns sensible defaults for warm pool configuration.
func DefaultWarmPoolConfig() WarmPoolConfig {
	return WarmPoolConfig{
		IdleTimeout:             5 * time.Minute,
		WarmupLeadTime:          30 * time.Second,
		MinWarmPods:             0,
		ColdStartTarget:         500 * time.Millisecond,
		EnablePredictiveScaling: true,
		UsageHistoryWindow:      24 * time.Hour,
	}
}

// WarmPoolManager manages warm pools across all pods with usage tracking
// and predictive scaling capabilities.
type WarmPoolManager struct {
	supervisor Supervisor
	config     WarmPoolConfig
	logger     *slog.Logger

	mu          sync.RWMutex
	poolConfigs map[string]*registry.PodSpec // Pod specs by name
	usageStats  map[string]*UsageStats       // Usage statistics per pool
	coldStarts  map[string]*ColdStartMetrics // Cold start metrics per pool

	// Background workers
	stop    chan struct{}
	stopped chan struct{}
}

// UsageStats tracks call patterns for a pool.
type UsageStats struct {
	mu sync.RWMutex

	// Rolling call counters
	callsLastMinute  int64
	callsLastHour    int64
	callsLast24Hours int64

	// Timestamps for rolling windows
	lastMinuteReset  time.Time
	lastHourReset    time.Time
	last24HoursReset time.Time

	// Hourly pattern detection (24 buckets for each hour of day)
	hourlyPattern [24]int64

	// Last activity timestamp
	lastCall time.Time

	// Total calls for this pool
	totalCalls int64
}

// ColdStartMetrics tracks cold start performance.
type ColdStartMetrics struct {
	mu sync.RWMutex

	// Cold start counts and latencies
	coldStarts      int64
	totalLatencyMs  int64
	maxLatencyMs    int64
	recentLatencies []int64 // Last 100 cold starts for percentile calculation

	// Warm starts for comparison
	warmStarts    int64
	warmLatencyMs int64
}

// NewWarmPoolManager creates a new warm pool manager.
func NewWarmPoolManager(supervisor Supervisor, config WarmPoolConfig, logger *slog.Logger) *WarmPoolManager {
	if logger == nil {
		logger = slog.Default()
	}

	return &WarmPoolManager{
		supervisor:  supervisor,
		config:      config,
		logger:      logger.With("component", "warm-pool-manager"),
		poolConfigs: make(map[string]*registry.PodSpec),
		usageStats:  make(map[string]*UsageStats),
		coldStarts:  make(map[string]*ColdStartMetrics),
		stop:        make(chan struct{}),
		stopped:     make(chan struct{}),
	}
}

// Start begins the warm pool management background workers.
func (m *WarmPoolManager) Start(ctx context.Context) {
	go m.runManagementLoop(ctx)
	m.logger.Info("warm pool manager started",
		"idle_timeout", m.config.IdleTimeout,
		"warmup_lead_time", m.config.WarmupLeadTime,
		"predictive_scaling", m.config.EnablePredictiveScaling,
	)
}

// Stop shuts down the warm pool manager.
func (m *WarmPoolManager) Stop() {
	close(m.stop)
	<-m.stopped
	m.logger.Info("warm pool manager stopped")
}

// RegisterPool registers a pool for warm pool management.
func (m *WarmPoolManager) RegisterPool(name string, spec *registry.PodSpec) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.poolConfigs[name] = spec
	m.usageStats[name] = &UsageStats{
		lastMinuteReset:  time.Now(),
		lastHourReset:    time.Now(),
		last24HoursReset: time.Now(),
	}
	m.coldStarts[name] = &ColdStartMetrics{
		recentLatencies: make([]int64, 0, 100),
	}

	m.logger.Info("pool registered for warm management", "pool", name)
}

// UnregisterPool removes a pool from warm pool management.
func (m *WarmPoolManager) UnregisterPool(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.poolConfigs, name)
	delete(m.usageStats, name)
	delete(m.coldStarts, name)

	m.logger.Info("pool unregistered from warm management", "pool", name)
}

// RecordCall records a call to a pool for usage tracking.
func (m *WarmPoolManager) RecordCall(poolName string, latencyMs int64, wasColdStart bool) {
	m.mu.RLock()
	stats, ok := m.usageStats[poolName]
	coldStats := m.coldStarts[poolName]
	m.mu.RUnlock()

	if !ok {
		return
	}

	now := time.Now()

	// Update usage stats
	stats.mu.Lock()
	stats.totalCalls++
	stats.callsLastMinute++
	stats.callsLastHour++
	stats.callsLast24Hours++
	stats.lastCall = now

	// Update hourly pattern
	hour := now.Hour()
	stats.hourlyPattern[hour]++
	stats.mu.Unlock()

	// Update cold start metrics
	if coldStats != nil {
		coldStats.mu.Lock()
		if wasColdStart {
			coldStats.coldStarts++
			coldStats.totalLatencyMs += latencyMs
			if latencyMs > coldStats.maxLatencyMs {
				coldStats.maxLatencyMs = latencyMs
			}
			// Keep last 100 for percentile calculations
			if len(coldStats.recentLatencies) >= 100 {
				coldStats.recentLatencies = coldStats.recentLatencies[1:]
			}
			coldStats.recentLatencies = append(coldStats.recentLatencies, latencyMs)
		} else {
			coldStats.warmStarts++
			coldStats.warmLatencyMs += latencyMs
		}
		coldStats.mu.Unlock()
	}
}

// GetUsageStats returns usage statistics for a pool.
func (m *WarmPoolManager) GetUsageStats(poolName string) *UsageStatsSnapshot {
	m.mu.RLock()
	stats, ok := m.usageStats[poolName]
	m.mu.RUnlock()

	if !ok {
		return nil
	}

	stats.mu.RLock()
	defer stats.mu.RUnlock()

	return &UsageStatsSnapshot{
		CallsLastMinute:  stats.callsLastMinute,
		CallsLastHour:    stats.callsLastHour,
		CallsLast24Hours: stats.callsLast24Hours,
		LastCall:         stats.lastCall,
		TotalCalls:       stats.totalCalls,
		HourlyPattern:    stats.hourlyPattern,
	}
}

// UsageStatsSnapshot is a point-in-time snapshot of usage stats.
type UsageStatsSnapshot struct {
	CallsLastMinute  int64
	CallsLastHour    int64
	CallsLast24Hours int64
	LastCall         time.Time
	TotalCalls       int64
	HourlyPattern    [24]int64
}

// GetColdStartMetrics returns cold start metrics for a pool.
func (m *WarmPoolManager) GetColdStartMetrics(poolName string) *ColdStartSnapshot {
	m.mu.RLock()
	metrics, ok := m.coldStarts[poolName]
	m.mu.RUnlock()

	if !ok {
		return nil
	}

	metrics.mu.RLock()
	defer metrics.mu.RUnlock()

	avgColdStart := int64(0)
	if metrics.coldStarts > 0 {
		avgColdStart = metrics.totalLatencyMs / metrics.coldStarts
	}

	avgWarmStart := int64(0)
	if metrics.warmStarts > 0 {
		avgWarmStart = metrics.warmLatencyMs / metrics.warmStarts
	}

	// Calculate p95 from recent latencies
	p95 := int64(0)
	if len(metrics.recentLatencies) > 0 {
		sorted := make([]int64, len(metrics.recentLatencies))
		copy(sorted, metrics.recentLatencies)
		sortInt64s(sorted)
		idx := int(float64(len(sorted)) * 0.95)
		if idx >= len(sorted) {
			idx = len(sorted) - 1
		}
		p95 = sorted[idx]
	}

	return &ColdStartSnapshot{
		ColdStarts:     metrics.coldStarts,
		WarmStarts:     metrics.warmStarts,
		AvgColdStartMs: avgColdStart,
		AvgWarmStartMs: avgWarmStart,
		MaxColdStartMs: metrics.maxLatencyMs,
		P95ColdStartMs: p95,
		MeetsTarget:    avgColdStart <= m.config.ColdStartTarget.Milliseconds(),
		TargetMs:       m.config.ColdStartTarget.Milliseconds(),
	}
}

// ColdStartSnapshot is a point-in-time snapshot of cold start metrics.
type ColdStartSnapshot struct {
	ColdStarts     int64
	WarmStarts     int64
	AvgColdStartMs int64
	AvgWarmStartMs int64
	MaxColdStartMs int64
	P95ColdStartMs int64
	MeetsTarget    bool
	TargetMs       int64
}

// runManagementLoop is the main background loop for warm pool management.
func (m *WarmPoolManager) runManagementLoop(ctx context.Context) {
	defer close(m.stopped)

	// Tick every 10 seconds
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Rolling window reset tickers
	minuteTicker := time.NewTicker(1 * time.Minute)
	defer minuteTicker.Stop()

	hourTicker := time.NewTicker(1 * time.Hour)
	defer hourTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stop:
			return

		case <-ticker.C:
			m.checkIdlePools(ctx)
			if m.config.EnablePredictiveScaling {
				m.checkPredictiveScaling(ctx)
			}

		case <-minuteTicker.C:
			m.resetMinuteCounters()

		case <-hourTicker.C:
			m.resetHourCounters()
		}
	}
}

// checkIdlePools scales down pools that have been idle too long.
func (m *WarmPoolManager) checkIdlePools(ctx context.Context) {
	if m.config.IdleTimeout <= 0 {
		return
	}

	m.mu.RLock()
	poolNames := make([]string, 0, len(m.usageStats))
	for name := range m.usageStats {
		poolNames = append(poolNames, name)
	}
	m.mu.RUnlock()

	now := time.Now()

	for _, name := range poolNames {
		m.mu.RLock()
		stats := m.usageStats[name]
		spec := m.poolConfigs[name]
		m.mu.RUnlock()

		if stats == nil || spec == nil {
			continue
		}

		stats.mu.RLock()
		lastCall := stats.lastCall
		stats.mu.RUnlock()

		// Check if pool has been idle
		idleDuration := now.Sub(lastCall)
		if lastCall.IsZero() || idleDuration < m.config.IdleTimeout {
			continue
		}

		// Get current pool
		pool := m.supervisor.GetPool(name)
		if pool == nil {
			continue
		}

		currentSize := pool.Size()
		targetSize := m.config.MinWarmPods

		// If spec allows scale-to-zero and we have MinWarmPods=0, scale all the way down
		if spec.Scaling != nil && spec.Scaling.MinReplicas == 0 {
			targetSize = m.config.MinWarmPods
		} else if spec.Scaling != nil {
			// Respect spec's minReplicas as floor
			if targetSize < spec.Scaling.MinReplicas {
				targetSize = spec.Scaling.MinReplicas
			}
		}

		// Scale down if needed
		if currentSize > targetSize {
			toRemove := currentSize - targetSize
			m.logger.Info("scaling down idle pool",
				"pool", name,
				"current_size", currentSize,
				"target_size", targetSize,
				"idle_duration", idleDuration,
			)

			for i := 0; i < toRemove; i++ {
				if err := pool.ScaleDown(ctx); err != nil {
					m.logger.Warn("failed to scale down",
						"pool", name,
						"error", err,
					)
					break
				}
			}
		}
	}
}

// checkPredictiveScaling pre-warms pools based on historical patterns.
func (m *WarmPoolManager) checkPredictiveScaling(ctx context.Context) {
	m.mu.RLock()
	poolNames := make([]string, 0, len(m.usageStats))
	for name := range m.usageStats {
		poolNames = append(poolNames, name)
	}
	m.mu.RUnlock()

	now := time.Now()
	futureHour := now.Add(m.config.WarmupLeadTime).Hour()

	for _, name := range poolNames {
		m.mu.RLock()
		stats := m.usageStats[name]
		spec := m.poolConfigs[name]
		m.mu.RUnlock()

		if stats == nil || spec == nil {
			continue
		}

		// Calculate predicted demand based on hourly pattern
		predictedCalls := m.predictDemand(stats, futureHour)
		if predictedCalls == 0 {
			continue
		}

		// Get current pool
		pool := m.supervisor.GetPool(name)
		if pool == nil {
			// Pool doesn't exist yet, may need to pre-warm
			if predictedCalls > 0 {
				m.logger.Info("pre-warming pool for predicted demand",
					"pool", name,
					"predicted_calls", predictedCalls,
				)
				_, err := m.supervisor.EnsurePool(ctx, name, spec)
				if err != nil {
					m.logger.Warn("failed to pre-warm pool",
						"pool", name,
						"error", err,
					)
				}
			}
			continue
		}

		// Calculate desired size based on predicted calls
		// Rough heuristic: 1 pod per 10 calls/minute expected
		desiredSize := int(predictedCalls/10) + 1
		if spec.Scaling != nil {
			if desiredSize > spec.Scaling.MaxReplicas {
				desiredSize = spec.Scaling.MaxReplicas
			}
			if desiredSize < spec.Scaling.MinReplicas {
				desiredSize = spec.Scaling.MinReplicas
			}
		}

		currentSize := pool.Size()
		if desiredSize > currentSize {
			m.logger.Info("scaling up for predicted demand",
				"pool", name,
				"current_size", currentSize,
				"desired_size", desiredSize,
				"predicted_calls", predictedCalls,
			)

			for i := 0; i < desiredSize-currentSize; i++ {
				if err := pool.ScaleUp(ctx); err != nil {
					m.logger.Warn("failed to scale up for prediction",
						"pool", name,
						"error", err,
					)
					break
				}
			}
		}
	}
}

// predictDemand predicts demand for a given hour based on historical patterns.
func (m *WarmPoolManager) predictDemand(stats *UsageStats, hour int) int64 {
	stats.mu.RLock()
	defer stats.mu.RUnlock()

	// Simple prediction: use historical hourly pattern
	// Could be enhanced with more sophisticated ML-based prediction
	historicalCalls := stats.hourlyPattern[hour]

	// Apply smoothing: average with adjacent hours
	prevHour := (hour + 23) % 24
	nextHour := (hour + 1) % 24

	smoothedCalls := (historicalCalls + stats.hourlyPattern[prevHour] + stats.hourlyPattern[nextHour]) / 3

	return smoothedCalls
}

// resetMinuteCounters resets per-minute counters.
func (m *WarmPoolManager) resetMinuteCounters() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, stats := range m.usageStats {
		stats.mu.Lock()
		stats.callsLastMinute = 0
		stats.lastMinuteReset = time.Now()
		stats.mu.Unlock()
	}
}

// resetHourCounters resets per-hour counters.
func (m *WarmPoolManager) resetHourCounters() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, stats := range m.usageStats {
		stats.mu.Lock()
		stats.callsLastHour = 0
		stats.lastHourReset = time.Now()
		stats.mu.Unlock()
	}
}

// PreWarmPool immediately warms a specific pool to a desired size.
func (m *WarmPoolManager) PreWarmPool(ctx context.Context, poolName string, desiredSize int) error {
	m.mu.RLock()
	spec := m.poolConfigs[poolName]
	m.mu.RUnlock()

	if spec == nil {
		return nil
	}

	pool, err := m.supervisor.EnsurePool(ctx, poolName, spec)
	if err != nil {
		return err
	}

	currentSize := pool.Size()
	for i := currentSize; i < desiredSize; i++ {
		if err := pool.ScaleUp(ctx); err != nil {
			return err
		}
	}

	m.logger.Info("pool pre-warmed",
		"pool", poolName,
		"size", pool.Size(),
	)

	return nil
}

// Status returns the status of all managed warm pools.
func (m *WarmPoolManager) Status() map[string]*WarmPoolStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*WarmPoolStatus, len(m.poolConfigs))

	for name := range m.poolConfigs {
		stats := m.usageStats[name]
		coldStats := m.coldStarts[name]
		pool := m.supervisor.GetPool(name)

		status := &WarmPoolStatus{
			Name: name,
		}

		if pool != nil {
			status.CurrentSize = pool.Size()
			status.HealthyCount = pool.HealthyCount()
			status.PendingCount = pool.PendingCount()
		}

		if stats != nil {
			status.Usage = m.GetUsageStats(name)
		}

		if coldStats != nil {
			status.ColdStart = m.GetColdStartMetrics(name)
		}

		result[name] = status
	}

	return result
}

// WarmPoolStatus represents the complete status of a warm pool.
type WarmPoolStatus struct {
	Name         string
	CurrentSize  int
	HealthyCount int
	PendingCount int64
	Usage        *UsageStatsSnapshot
	ColdStart    *ColdStartSnapshot
}

// WarmPoolAcquirer wraps a pool with usage tracking.
type WarmPoolAcquirer struct {
	pool    *PodPool
	manager *WarmPoolManager
	name    string
}

// NewWarmPoolAcquirer creates a new warm pool acquirer that tracks usage.
func NewWarmPoolAcquirer(pool *PodPool, manager *WarmPoolManager, name string) *WarmPoolAcquirer {
	return &WarmPoolAcquirer{
		pool:    pool,
		manager: manager,
		name:    name,
	}
}

// Acquire gets a pod from the pool and tracks usage.
func (w *WarmPoolAcquirer) Acquire() (PodInterface, func(), error) {
	startTime := time.Now()

	pod, release, err := w.pool.AcquireWithTracking()
	if err != nil {
		return nil, nil, err
	}

	latencyMs := time.Since(startTime).Milliseconds()

	// Determine if this was a cold start (new pod spawned)
	// For now, we'll consider latency > 100ms as cold start indicator
	wasColdStart := latencyMs > 100

	// Wrap the release function to record the call
	wrappedRelease := func() {
		w.manager.RecordCall(w.name, latencyMs, wasColdStart)
		release()
	}

	return pod, wrappedRelease, nil
}

// ScaleToZeroConfig is configuration specific to scale-to-zero behavior.
type ScaleToZeroConfig struct {
	// Enabled allows the pool to scale to zero replicas.
	Enabled bool `yaml:"enabled"`

	// IdleTimeout is how long the pool can be idle before scaling to zero.
	IdleTimeout time.Duration `yaml:"idleTimeout"`

	// GracePeriod is how long to wait after the last request before starting scale-down.
	GracePeriod time.Duration `yaml:"gracePeriod"`

	// CooldownPeriod prevents rapid scale up/down cycles.
	CooldownPeriod time.Duration `yaml:"cooldownPeriod"`
}

// IdlePoolTracker tracks idle state for scale-to-zero.
type IdlePoolTracker struct {
	mu     sync.RWMutex
	pools  map[string]*idleState
	config ScaleToZeroConfig
	logger *slog.Logger
}

type idleState struct {
	lastActivity    time.Time
	lastScaleDown   time.Time
	lastScaleUp     time.Time
	currentReplicas int32
}

// NewIdlePoolTracker creates a new idle pool tracker.
func NewIdlePoolTracker(config ScaleToZeroConfig, logger *slog.Logger) *IdlePoolTracker {
	return &IdlePoolTracker{
		pools:  make(map[string]*idleState),
		config: config,
		logger: logger,
	}
}

// RecordActivity records activity for a pool.
func (t *IdlePoolTracker) RecordActivity(poolName string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, ok := t.pools[poolName]
	if !ok {
		state = &idleState{}
		t.pools[poolName] = state
	}
	state.lastActivity = time.Now()
}

// ShouldScaleToZero checks if a pool should scale to zero.
func (t *IdlePoolTracker) ShouldScaleToZero(poolName string) bool {
	if !t.config.Enabled {
		return false
	}

	t.mu.RLock()
	state, ok := t.pools[poolName]
	t.mu.RUnlock()

	if !ok {
		return false
	}

	now := time.Now()

	// Check idle timeout
	idleDuration := now.Sub(state.lastActivity)
	if idleDuration < t.config.IdleTimeout {
		return false
	}

	// Check grace period
	if idleDuration < t.config.GracePeriod {
		return false
	}

	// Check cooldown period
	if !state.lastScaleDown.IsZero() && now.Sub(state.lastScaleDown) < t.config.CooldownPeriod {
		return false
	}

	return true
}

// RecordScaleDown records a scale-down event.
func (t *IdlePoolTracker) RecordScaleDown(poolName string, replicas int32) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, ok := t.pools[poolName]
	if !ok {
		state = &idleState{}
		t.pools[poolName] = state
	}
	state.lastScaleDown = time.Now()
	state.currentReplicas = replicas
}

// RecordScaleUp records a scale-up event.
func (t *IdlePoolTracker) RecordScaleUp(poolName string, replicas int32) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, ok := t.pools[poolName]
	if !ok {
		state = &idleState{}
		t.pools[poolName] = state
	}
	state.lastScaleUp = time.Now()
	state.currentReplicas = replicas
}

// GetIdleStats returns idle statistics for all pools.
func (t *IdlePoolTracker) GetIdleStats() map[string]IdleStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]IdleStats, len(t.pools))
	now := time.Now()

	for name, state := range t.pools {
		result[name] = IdleStats{
			LastActivity:    state.lastActivity,
			IdleDuration:    now.Sub(state.lastActivity),
			CurrentReplicas: state.currentReplicas,
			LastScaleDown:   state.lastScaleDown,
			LastScaleUp:     state.lastScaleUp,
		}
	}

	return result
}

// IdleStats represents idle statistics for a pool.
type IdleStats struct {
	LastActivity    time.Time
	IdleDuration    time.Duration
	CurrentReplicas int32
	LastScaleDown   time.Time
	LastScaleUp     time.Time
}

// WarmPoolMetrics Metrics provides an interface for collecting warm pool metrics.
type WarmPoolMetrics struct {
	// Cold start latency histogram
	coldStartLatencyMs atomic.Int64
	coldStartCount     atomic.Int64

	// Warm start latency histogram
	warmStartLatencyMs atomic.Int64
	warmStartCount     atomic.Int64

	// Scale events
	scaleUpCount   atomic.Int64
	scaleDownCount atomic.Int64

	// Idle timeouts triggered
	idleTimeoutCount atomic.Int64

	// Predictive pre-warms
	predictivePreWarmCount atomic.Int64
}

// NewWarmPoolMetrics creates a new metrics collector.
func NewWarmPoolMetrics() *WarmPoolMetrics {
	return &WarmPoolMetrics{}
}

// RecordColdStart records a cold start event.
func (m *WarmPoolMetrics) RecordColdStart(latencyMs int64) {
	m.coldStartLatencyMs.Add(latencyMs)
	m.coldStartCount.Add(1)
}

// RecordWarmStart records a warm start event.
func (m *WarmPoolMetrics) RecordWarmStart(latencyMs int64) {
	m.warmStartLatencyMs.Add(latencyMs)
	m.warmStartCount.Add(1)
}

// RecordScaleUp records a scale-up event.
func (m *WarmPoolMetrics) RecordScaleUp() {
	m.scaleUpCount.Add(1)
}

// RecordScaleDown records a scale-down event.
func (m *WarmPoolMetrics) RecordScaleDown() {
	m.scaleDownCount.Add(1)
}

// RecordIdleTimeout records an idle timeout event.
func (m *WarmPoolMetrics) RecordIdleTimeout() {
	m.idleTimeoutCount.Add(1)
}

// RecordPredictivePreWarm records a predictive pre-warm event.
func (m *WarmPoolMetrics) RecordPredictivePreWarm() {
	m.predictivePreWarmCount.Add(1)
}

// Snapshot returns a snapshot of current metrics.
func (m *WarmPoolMetrics) Snapshot() MetricsSnapshot {
	coldCount := m.coldStartCount.Load()
	warmCount := m.warmStartCount.Load()

	avgCold := int64(0)
	if coldCount > 0 {
		avgCold = m.coldStartLatencyMs.Load() / coldCount
	}

	avgWarm := int64(0)
	if warmCount > 0 {
		avgWarm = m.warmStartLatencyMs.Load() / warmCount
	}

	return MetricsSnapshot{
		ColdStartCount:         coldCount,
		WarmStartCount:         warmCount,
		AvgColdStartLatencyMs:  avgCold,
		AvgWarmStartLatencyMs:  avgWarm,
		ScaleUpCount:           m.scaleUpCount.Load(),
		ScaleDownCount:         m.scaleDownCount.Load(),
		IdleTimeoutCount:       m.idleTimeoutCount.Load(),
		PredictivePreWarmCount: m.predictivePreWarmCount.Load(),
	}
}

// MetricsSnapshot is a point-in-time snapshot of warm pool metrics.
type MetricsSnapshot struct {
	ColdStartCount         int64
	WarmStartCount         int64
	AvgColdStartLatencyMs  int64
	AvgWarmStartLatencyMs  int64
	ScaleUpCount           int64
	ScaleDownCount         int64
	IdleTimeoutCount       int64
	PredictivePreWarmCount int64
}
