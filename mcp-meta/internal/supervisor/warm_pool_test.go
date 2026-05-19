package supervisor

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"mcp-meta/internal/registry"
)

func TestWarmPoolConfig_Defaults(t *testing.T) {
	config := DefaultWarmPoolConfig()

	if config.IdleTimeout != 5*time.Minute {
		t.Errorf("IdleTimeout = %v, want %v", config.IdleTimeout, 5*time.Minute)
	}
	if config.WarmupLeadTime != 30*time.Second {
		t.Errorf("WarmupLeadTime = %v, want %v", config.WarmupLeadTime, 30*time.Second)
	}
	if config.MinWarmPods != 0 {
		t.Errorf("MinWarmPods = %d, want 0", config.MinWarmPods)
	}
	if config.ColdStartTarget != 500*time.Millisecond {
		t.Errorf("ColdStartTarget = %v, want %v", config.ColdStartTarget, 500*time.Millisecond)
	}
	if !config.EnablePredictiveScaling {
		t.Error("EnablePredictiveScaling should be true by default")
	}
	if config.UsageHistoryWindow != 24*time.Hour {
		t.Errorf("UsageHistoryWindow = %v, want %v", config.UsageHistoryWindow, 24*time.Hour)
	}
}

func TestWarmPoolManager_RegisterUnregister(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	supervisor := New(logger)
	config := DefaultWarmPoolConfig()

	manager := NewWarmPoolManager(supervisor, config, logger)

	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"echo", "hello"},
	}

	// Register
	manager.RegisterPool("test-pod", spec)

	// Verify registration
	status := manager.Status()
	if _, ok := status["test-pod"]; !ok {
		t.Error("pool should be registered")
	}

	// Unregister
	manager.UnregisterPool("test-pod")

	// Verify unregistration
	status = manager.Status()
	if _, ok := status["test-pod"]; ok {
		t.Error("pool should be unregistered")
	}
}

func TestWarmPoolManager_RecordCall(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	supervisor := New(logger)
	config := DefaultWarmPoolConfig()

	manager := NewWarmPoolManager(supervisor, config, logger)

	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"echo", "hello"},
	}

	manager.RegisterPool("test-pod", spec)

	// Record some calls
	for i := 0; i < 10; i++ {
		manager.RecordCall("test-pod", 50, false) // warm starts
	}
	for i := 0; i < 5; i++ {
		manager.RecordCall("test-pod", 200, true) // cold starts
	}

	// Check usage stats
	usageStats := manager.GetUsageStats("test-pod")
	if usageStats == nil {
		t.Fatal("usage stats should not be nil")
	}
	if usageStats.TotalCalls != 15 {
		t.Errorf("TotalCalls = %d, want 15", usageStats.TotalCalls)
	}
	if usageStats.CallsLastMinute != 15 {
		t.Errorf("CallsLastMinute = %d, want 15", usageStats.CallsLastMinute)
	}

	// Check cold start metrics
	coldStartMetrics := manager.GetColdStartMetrics("test-pod")
	if coldStartMetrics == nil {
		t.Fatal("cold start metrics should not be nil")
	}
	if coldStartMetrics.ColdStarts != 5 {
		t.Errorf("ColdStarts = %d, want 5", coldStartMetrics.ColdStarts)
	}
	if coldStartMetrics.WarmStarts != 10 {
		t.Errorf("WarmStarts = %d, want 10", coldStartMetrics.WarmStarts)
	}
	if coldStartMetrics.AvgColdStartMs != 200 {
		t.Errorf("AvgColdStartMs = %d, want 200", coldStartMetrics.AvgColdStartMs)
	}
	if coldStartMetrics.AvgWarmStartMs != 50 {
		t.Errorf("AvgWarmStartMs = %d, want 50", coldStartMetrics.AvgWarmStartMs)
	}
}

func TestWarmPoolManager_HourlyPattern(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	supervisor := New(logger)
	config := DefaultWarmPoolConfig()

	manager := NewWarmPoolManager(supervisor, config, logger)

	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"echo", "hello"},
	}

	manager.RegisterPool("test-pod", spec)

	// Record calls - they should be bucketed by current hour
	for i := 0; i < 100; i++ {
		manager.RecordCall("test-pod", 50, false)
	}

	usageStats := manager.GetUsageStats("test-pod")
	if usageStats == nil {
		t.Fatal("usage stats should not be nil")
	}

	// Current hour should have 100 calls
	currentHour := time.Now().Hour()
	if usageStats.HourlyPattern[currentHour] != 100 {
		t.Errorf("HourlyPattern[%d] = %d, want 100", currentHour, usageStats.HourlyPattern[currentHour])
	}
}

func TestWarmPoolManager_ColdStartTarget(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	supervisor := New(logger)
	config := DefaultWarmPoolConfig()
	config.ColdStartTarget = 500 * time.Millisecond

	manager := NewWarmPoolManager(supervisor, config, logger)

	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"echo", "hello"},
	}

	manager.RegisterPool("test-pod", spec)

	// Record cold starts below target
	for i := 0; i < 10; i++ {
		manager.RecordCall("test-pod", 400, true)
	}

	metrics := manager.GetColdStartMetrics("test-pod")
	if !metrics.MeetsTarget {
		t.Error("should meet target with 400ms avg cold start")
	}

	// Record cold starts above target
	for i := 0; i < 20; i++ {
		manager.RecordCall("test-pod", 600, true)
	}

	metrics = manager.GetColdStartMetrics("test-pod")
	// Now average should be higher
	// (10*400 + 20*600) / 30 = 533ms
	if metrics.MeetsTarget {
		t.Errorf("should not meet target with avg %dms", metrics.AvgColdStartMs)
	}
}

func TestWarmPoolManager_StartStop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	supervisor := New(logger)
	config := DefaultWarmPoolConfig()

	manager := NewWarmPoolManager(supervisor, config, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Should not panic
	manager.Start(ctx)

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Should stop cleanly
	manager.Stop()
}

func TestIdlePoolTracker_Basic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	config := ScaleToZeroConfig{
		Enabled:        true,
		IdleTimeout:    100 * time.Millisecond,
		GracePeriod:    50 * time.Millisecond,
		CooldownPeriod: 200 * time.Millisecond,
	}

	tracker := NewIdlePoolTracker(config, logger)

	// Record activity
	tracker.RecordActivity("test-pool")

	// Should not scale to zero immediately
	if tracker.ShouldScaleToZero("test-pool") {
		t.Error("should not scale to zero immediately after activity")
	}

	// Wait for idle timeout
	time.Sleep(150 * time.Millisecond)

	// Now should scale to zero
	if !tracker.ShouldScaleToZero("test-pool") {
		t.Error("should scale to zero after idle timeout")
	}

	// Record scale down
	tracker.RecordScaleDown("test-pool", 0)

	// Immediately after scale down, cooldown should prevent another scale event
	tracker.RecordActivity("test-pool")
	time.Sleep(150 * time.Millisecond)

	// Should not scale due to cooldown
	if tracker.ShouldScaleToZero("test-pool") {
		t.Error("should not scale to zero during cooldown period")
	}
}

func TestIdlePoolTracker_Disabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	config := ScaleToZeroConfig{
		Enabled:     false,
		IdleTimeout: 100 * time.Millisecond,
	}

	tracker := NewIdlePoolTracker(config, logger)

	tracker.RecordActivity("test-pool")
	time.Sleep(150 * time.Millisecond)

	// Should not scale when disabled
	if tracker.ShouldScaleToZero("test-pool") {
		t.Error("should not scale to zero when disabled")
	}
}

func TestIdlePoolTracker_GetIdleStats(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	config := ScaleToZeroConfig{
		Enabled:     true,
		IdleTimeout: 1 * time.Minute,
	}

	tracker := NewIdlePoolTracker(config, logger)

	tracker.RecordActivity("pool-1")
	tracker.RecordActivity("pool-2")
	tracker.RecordScaleUp("pool-1", 3)
	tracker.RecordScaleDown("pool-2", 1)

	stats := tracker.GetIdleStats()

	if len(stats) != 2 {
		t.Errorf("expected 2 pools in stats, got %d", len(stats))
	}

	if stats["pool-1"].CurrentReplicas != 3 {
		t.Errorf("pool-1 replicas = %d, want 3", stats["pool-1"].CurrentReplicas)
	}

	if stats["pool-2"].CurrentReplicas != 1 {
		t.Errorf("pool-2 replicas = %d, want 1", stats["pool-2"].CurrentReplicas)
	}
}

func TestWarmPoolMetrics(t *testing.T) {
	metrics := NewWarmPoolMetrics()

	// Record some events
	metrics.RecordColdStart(500)
	metrics.RecordColdStart(600)
	metrics.RecordWarmStart(50)
	metrics.RecordWarmStart(60)
	metrics.RecordWarmStart(70)
	metrics.RecordScaleUp()
	metrics.RecordScaleDown()
	metrics.RecordIdleTimeout()
	metrics.RecordPredictivePreWarm()

	snapshot := metrics.Snapshot()

	if snapshot.ColdStartCount != 2 {
		t.Errorf("ColdStartCount = %d, want 2", snapshot.ColdStartCount)
	}
	if snapshot.WarmStartCount != 3 {
		t.Errorf("WarmStartCount = %d, want 3", snapshot.WarmStartCount)
	}
	if snapshot.AvgColdStartLatencyMs != 550 {
		t.Errorf("AvgColdStartLatencyMs = %d, want 550", snapshot.AvgColdStartLatencyMs)
	}
	if snapshot.AvgWarmStartLatencyMs != 60 {
		t.Errorf("AvgWarmStartLatencyMs = %d, want 60", snapshot.AvgWarmStartLatencyMs)
	}
	if snapshot.ScaleUpCount != 1 {
		t.Errorf("ScaleUpCount = %d, want 1", snapshot.ScaleUpCount)
	}
	if snapshot.ScaleDownCount != 1 {
		t.Errorf("ScaleDownCount = %d, want 1", snapshot.ScaleDownCount)
	}
	if snapshot.IdleTimeoutCount != 1 {
		t.Errorf("IdleTimeoutCount = %d, want 1", snapshot.IdleTimeoutCount)
	}
	if snapshot.PredictivePreWarmCount != 1 {
		t.Errorf("PredictivePreWarmCount = %d, want 1", snapshot.PredictivePreWarmCount)
	}
}

func TestSortInt64s(t *testing.T) {
	tests := []struct {
		name   string
		input  []int64
		expect []int64
	}{
		{"empty", []int64{}, []int64{}},
		{"single", []int64{5}, []int64{5}},
		{"sorted", []int64{1, 2, 3}, []int64{1, 2, 3}},
		{"reverse", []int64{3, 2, 1}, []int64{1, 2, 3}},
		{"random", []int64{5, 2, 8, 1, 9}, []int64{1, 2, 5, 8, 9}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy to avoid modifying test data
			input := make([]int64, len(tt.input))
			copy(input, tt.input)

			sortInt64s(input)

			if len(input) != len(tt.expect) {
				t.Fatalf("length mismatch: got %d, want %d", len(input), len(tt.expect))
			}

			for i := range input {
				if input[i] != tt.expect[i] {
					t.Errorf("input[%d] = %d, want %d", i, input[i], tt.expect[i])
				}
			}
		})
	}
}

func TestColdStartP95(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	supervisor := New(logger)
	config := DefaultWarmPoolConfig()

	manager := NewWarmPoolManager(supervisor, config, logger)

	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"echo", "hello"},
	}

	manager.RegisterPool("test-pod", spec)

	// Record 100 cold starts with varying latencies
	// Most are fast (100ms), but 5% are slow (1000ms)
	for i := 0; i < 95; i++ {
		manager.RecordCall("test-pod", 100, true)
	}
	for i := 0; i < 5; i++ {
		manager.RecordCall("test-pod", 1000, true)
	}

	metrics := manager.GetColdStartMetrics("test-pod")

	// P95 should be around 1000ms (the slow ones)
	if metrics.P95ColdStartMs < 900 || metrics.P95ColdStartMs > 1100 {
		t.Errorf("P95ColdStartMs = %d, want ~1000", metrics.P95ColdStartMs)
	}
}

func TestWarmPoolManager_NonexistentPool(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	supervisor := New(logger)
	config := DefaultWarmPoolConfig()

	manager := NewWarmPoolManager(supervisor, config, logger)

	// Should not panic on non-existent pool
	manager.RecordCall("nonexistent", 100, true)

	stats := manager.GetUsageStats("nonexistent")
	if stats != nil {
		t.Error("expected nil stats for nonexistent pool")
	}

	metrics := manager.GetColdStartMetrics("nonexistent")
	if metrics != nil {
		t.Error("expected nil metrics for nonexistent pool")
	}
}

func TestWarmPoolSpec_YAML(t *testing.T) {
	yamlContent := `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: warm-pool-test
spec:
  command: ["echo", "hello"]
  scaling:
    minReplicas: 0
    maxReplicas: 10
    targetPending: 5
    scaleDownDelay: 30s
    warmPool:
      scaleToZero: true
      idleTimeout: 5m
      minWarmPods: 1
      coldStartTarget: 500ms
      predictiveScaling: true
      warmupLeadTime: 30s
  tools:
    - name: test
      description: Test tool
`

	manifest, err := registry.ParseManifest([]byte(yamlContent))
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	if manifest.Spec.Scaling == nil {
		t.Fatal("scaling should not be nil")
	}

	if manifest.Spec.Scaling.WarmPool == nil {
		t.Fatal("warmPool should not be nil")
	}

	wp := manifest.Spec.Scaling.WarmPool

	if !wp.ScaleToZero {
		t.Error("scaleToZero should be true")
	}
	if wp.IdleTimeout.Duration != 5*time.Minute {
		t.Errorf("idleTimeout = %v, want 5m", wp.IdleTimeout.Duration)
	}
	if wp.MinWarmPods != 1 {
		t.Errorf("minWarmPods = %d, want 1", wp.MinWarmPods)
	}
	if wp.ColdStartTarget.Duration != 500*time.Millisecond {
		t.Errorf("coldStartTarget = %v, want 500ms", wp.ColdStartTarget.Duration)
	}
	if !wp.PredictiveScaling {
		t.Error("predictiveScaling should be true")
	}
	if wp.WarmupLeadTime.Duration != 30*time.Second {
		t.Errorf("warmupLeadTime = %v, want 30s", wp.WarmupLeadTime.Duration)
	}
}

// BenchmarkRecordCall benchmarks the RecordCall method
func BenchmarkRecordCall(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	supervisor := New(logger)
	config := DefaultWarmPoolConfig()

	manager := NewWarmPoolManager(supervisor, config, logger)

	spec := &registry.PodSpec{
		Name:    "bench-pod",
		Command: []string{"echo", "hello"},
	}

	manager.RegisterPool("bench-pod", spec)

	b.ResetTimer()
	for b.Loop() {
		manager.RecordCall("bench-pod", 50, false)
	}
}

// BenchmarkGetUsageStats benchmarks the GetUsageStats method
func BenchmarkGetUsageStats(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	supervisor := New(logger)
	config := DefaultWarmPoolConfig()

	manager := NewWarmPoolManager(supervisor, config, logger)

	spec := &registry.PodSpec{
		Name:    "bench-pod",
		Command: []string{"echo", "hello"},
	}

	manager.RegisterPool("bench-pod", spec)

	// Pre-populate with calls
	for i := 0; i < 1000; i++ {
		manager.RecordCall("bench-pod", 50, false)
	}

	b.ResetTimer()
	for b.Loop() {
		_ = manager.GetUsageStats("bench-pod")
	}
}
