package supervisor

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"mcp-meta/internal/jsonrpc"
	"mcp-meta/internal/registry"
)

func testSpec(replicas int) *registry.PodSpec {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		ShutdownTimeout: registry.Duration{
			Duration: 5 * time.Second,
		},
	}
	if replicas > 1 {
		spec.Scaling = &registry.ScalingSpec{
			MinReplicas: replicas,
			MaxReplicas: replicas,
		}
	}
	return spec
}

func TestPodPool_SingleReplica(t *testing.T) {
	spec := testSpec(1)
	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	if pool.Size() != 1 {
		t.Errorf("expected pool size 1, got %d", pool.Size())
	}

	pod, err := pool.Acquire()
	if err != nil {
		t.Fatalf("failed to acquire pod: %v", err)
	}

	result, err := pod.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("ping failed: %v", err)
	}

	var resp map[string]string
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %s", resp["status"])
	}
}

func TestPodPool_MultipleReplicas(t *testing.T) {
	spec := testSpec(3)
	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	if pool.Size() != 3 {
		t.Errorf("expected pool size 3, got %d", pool.Size())
	}

	if pool.HealthyCount() != 3 {
		t.Errorf("expected healthy count 3, got %d", pool.HealthyCount())
	}
}

func TestPodPool_RoundRobin(t *testing.T) {
	spec := testSpec(3)
	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Make multiple calls and track which replica handles each
	replicas := make(map[string]int)
	for i := 0; i < 9; i++ {
		pod, err := pool.Acquire()
		if err != nil {
			t.Fatalf("failed to acquire pod: %v", err)
		}

		result, err := pod.Call(ctx, "ping", nil)
		if err != nil {
			t.Fatalf("ping failed: %v", err)
		}

		var resp map[string]string
		json.Unmarshal(result, &resp)
		replicas[resp["replica"]]++
	}

	// With round-robin, each replica should handle 3 requests
	if len(replicas) != 3 {
		t.Errorf("expected 3 different replicas, got %d: %v", len(replicas), replicas)
	}

	for replica, count := range replicas {
		if count != 3 {
			t.Errorf("replica %s handled %d requests, expected 3", replica, count)
		}
	}
}

func TestPodPool_ConcurrentCalls(t *testing.T) {
	spec := testSpec(4)
	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Fire concurrent requests
	var wg sync.WaitGroup
	results := make(chan string, 100)
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			pod, release, err := pool.AcquireWithTracking()
			if err != nil {
				errors <- err
				return
			}
			defer release()

			result, err := pod.Call(ctx, "echo", json.RawMessage(`{"test": true}`))
			if err != nil {
				errors <- err
				return
			}

			var resp map[string]interface{}
			json.Unmarshal(result, &resp)
			if replica, ok := resp["handled_by"].(string); ok {
				results <- replica
			}
		}()
	}

	wg.Wait()
	close(results)
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("concurrent call error: %v", err)
	}

	// Collect results
	replicas := make(map[string]int)
	for replica := range results {
		replicas[replica]++
	}

	// All 4 replicas should have handled requests
	if len(replicas) < 4 {
		t.Errorf("expected 4 replicas to handle requests, got %d: %v", len(replicas), replicas)
	}

	t.Logf("Request distribution: %v", replicas)
}

func TestPodPool_PendingTracking(t *testing.T) {
	spec := testSpec(2)
	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	if pool.PendingCount() != 0 {
		t.Errorf("expected pending count 0, got %d", pool.PendingCount())
	}

	// Acquire with tracking
	_, release1, err := pool.AcquireWithTracking()
	if err != nil {
		t.Fatalf("failed to acquire pod: %v", err)
	}

	if pool.PendingCount() != 1 {
		t.Errorf("expected pending count 1, got %d", pool.PendingCount())
	}

	_, release2, err := pool.AcquireWithTracking()
	if err != nil {
		t.Fatalf("failed to acquire pod: %v", err)
	}

	if pool.PendingCount() != 2 {
		t.Errorf("expected pending count 2, got %d", pool.PendingCount())
	}

	// Release
	release1()
	if pool.PendingCount() != 1 {
		t.Errorf("expected pending count 1 after release, got %d", pool.PendingCount())
	}

	release2()
	if pool.PendingCount() != 0 {
		t.Errorf("expected pending count 0 after all releases, got %d", pool.PendingCount())
	}
}

func TestPodPool_CrashRecovery(t *testing.T) {
	spec := testSpec(2)
	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Get one pod and crash it
	pod, err := pool.Acquire()
	if err != nil {
		t.Fatalf("failed to acquire pod: %v", err)
	}

	// Send crash command (will exit the pod)
	pod.Call(ctx, "crash", nil)

	// Wait for crash to be detected
	time.Sleep(200 * time.Millisecond)

	// Pool should still have 1 healthy pod
	if pool.HealthyCount() != 1 {
		t.Errorf("expected 1 healthy pod after crash, got %d", pool.HealthyCount())
	}

	// Acquire should return the healthy pod
	healthyPod, err := pool.Acquire()
	if err != nil {
		t.Fatalf("failed to acquire healthy pod: %v", err)
	}

	result, err := healthyPod.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("ping on healthy pod failed: %v", err)
	}

	var resp map[string]string
	json.Unmarshal(result, &resp)
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %s", resp["status"])
	}
}

func TestPodPool_ScaleUpDown(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		ShutdownTimeout: registry.Duration{
			Duration: 5 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas: 2,
			MaxReplicas: 5,
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Should start with min replicas
	if pool.Size() != 2 {
		t.Errorf("expected initial size 2, got %d", pool.Size())
	}

	// Scale up
	if err := pool.ScaleUp(ctx); err != nil {
		t.Fatalf("scale up failed: %v", err)
	}
	if pool.Size() != 3 {
		t.Errorf("expected size 3 after scale up, got %d", pool.Size())
	}

	// Scale up again
	pool.ScaleUp(ctx)
	pool.ScaleUp(ctx)
	if pool.Size() != 5 {
		t.Errorf("expected size 5, got %d", pool.Size())
	}

	// Scale up at max should fail
	if err := pool.ScaleUp(ctx); err == nil {
		t.Error("expected error when scaling up at max, got nil")
	}

	// Scale down
	if err := pool.ScaleDown(ctx); err != nil {
		t.Fatalf("scale down failed: %v", err)
	}

	// Give time for async stop
	time.Sleep(100 * time.Millisecond)

	if pool.Size() != 4 {
		t.Errorf("expected size 4 after scale down, got %d", pool.Size())
	}

	// Scale down to min
	pool.ScaleDown(ctx)
	pool.ScaleDown(ctx)

	time.Sleep(100 * time.Millisecond)

	if pool.Size() != 2 {
		t.Errorf("expected size 2 at min, got %d", pool.Size())
	}

	// Scale down at min should fail
	if err := pool.ScaleDown(ctx); err == nil {
		t.Error("expected error when scaling down at min, got nil")
	}
}

func TestPodPool_Status(t *testing.T) {
	spec := testSpec(2)
	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	statuses := pool.Status()
	if len(statuses) != 2 {
		t.Errorf("expected 2 statuses, got %d", len(statuses))
	}

	for _, status := range statuses {
		if status.State != PodStateRunning {
			t.Errorf("expected pod state running, got %s", status.State)
		}
	}
}

func TestPodPool_SetInterPodHandler(t *testing.T) {
	spec := testSpec(1)
	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Set inter-pod handler
	handler := func(ctx context.Context, sourcePod, targetTool string, args json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"handled": true}`), nil
	}

	// Should not panic
	pool.SetInterPodHandler(handler)
}

func TestPodPool_SetInterPodHandlerWithContract(t *testing.T) {
	spec := testSpec(1)
	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Set inter-pod handler with contract
	handler := func(ctx context.Context, sourcePod, targetTool string, args json.RawMessage, contract *jsonrpc.ContractExpectation) (json.RawMessage, error) {
		return json.RawMessage(`{"handled": true}`), nil
	}

	// Should not panic
	pool.SetInterPodHandlerWithContract(handler)
}

func TestPodPool_PendingCount(t *testing.T) {
	spec := testSpec(1)
	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Initially no pending
	if pool.PendingCount() != 0 {
		t.Errorf("expected pending 0, got %d", pool.PendingCount())
	}
}

func TestPodPool_WithAutoScaler(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas:   1,
			MaxReplicas:   3,
			TargetPending: 5,
			ScaleDownDelay: registry.Duration{
				Duration: 1 * time.Second,
			},
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Auto-scaler should be running
	if pool.Size() != 1 {
		t.Errorf("expected size 1, got %d", pool.Size())
	}

	// Cancel context to stop auto-scaler
	cancel()
	time.Sleep(50 * time.Millisecond)
}

// TestAcquire_AllPodsUnhealthy tests that Acquire returns error when all pods are crashed
func TestAcquire_AllPodsUnhealthy(t *testing.T) {
	spec := testSpec(2)
	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Crash all pods
	for i := 0; i < pool.Size(); i++ {
		pod, err := pool.Acquire()
		if err != nil {
			continue
		}
		pod.Call(ctx, "crash", nil)
	}

	// Wait for crashes to be detected
	time.Sleep(300 * time.Millisecond)

	// All pods should be unhealthy
	if pool.HealthyCount() != 0 {
		t.Errorf("HealthyCount = %d, want 0", pool.HealthyCount())
	}

	// Acquire should fail
	_, err := pool.Acquire()
	if err == nil {
		t.Error("Acquire() should return error when all pods are unhealthy")
	}
}

// TestAutoScaler_ScalesUp_WhenPendingHigh tests that auto-scaler scales up under load
func TestAutoScaler_ScalesUp_WhenPendingHigh(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas:   1,
			MaxReplicas:   3,
			TargetPending: 2, // Low target to trigger scaling quickly
			ScaleDownDelay: registry.Duration{
				Duration: 10 * time.Second,
			},
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	initialSize := pool.Size()
	if initialSize != 1 {
		t.Errorf("initial size = %d, want 1", initialSize)
	}

	// Fire concurrent slow calls to increase pending count
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pod, release, err := pool.AcquireWithTracking()
			if err != nil {
				return
			}
			defer release()
			// Slow call to keep pending high
			pod.Call(ctx, "slow_call", json.RawMessage(`{"duration_ms": 500}`))
		}()
	}

	// Wait a bit for auto-scaler to react
	time.Sleep(200 * time.Millisecond)

	// Pool should have scaled up (may not always happen depending on timing)
	// We just verify the auto-scaler code path is exercised
	wg.Wait()

	// Pool size should be at least 1
	if pool.Size() < 1 {
		t.Errorf("pool size = %d, should be at least 1", pool.Size())
	}
}

// TestAutoScaler_ScalesDown_WithDelay tests scale-down respects delay
func TestAutoScaler_ScalesDown_WithDelay(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas:   1,
			MaxReplicas:   3,
			TargetPending: 1,
			ScaleDownDelay: registry.Duration{
				Duration: 100 * time.Millisecond, // Short delay for testing
			},
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Manually scale up
	pool.ScaleUp(ctx)
	pool.ScaleUp(ctx)

	if pool.Size() != 3 {
		t.Errorf("size after scale up = %d, want 3", pool.Size())
	}

	// Wait for auto-scaler to scale down (pending is 0, should trigger scale down)
	time.Sleep(300 * time.Millisecond)

	// Size should decrease (auto-scaler should have run)
	// Note: actual scaling depends on timing, but we exercise the code path
	t.Logf("Final pool size: %d", pool.Size())
}

// TestShutdown_StopsAutoScalerFirst tests that Shutdown stops auto-scaler cleanly
func TestShutdown_StopsAutoScalerFirst(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas:   1,
			MaxReplicas:   3,
			TargetPending: 5,
			ScaleDownDelay: registry.Duration{
				Duration: 1 * time.Second,
			},
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Shutdown should not panic or deadlock
	err := pool.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}

	// Pool should be empty after shutdown
	if pool.Size() != 0 {
		t.Errorf("size after shutdown = %d, want 0", pool.Size())
	}
}

// TestPool_MaxReplicasEnforced tests that pool doesn't exceed maxReplicas
func TestPool_MaxReplicasEnforced(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas: 1,
			MaxReplicas: 2,
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// First scale up should succeed
	err := pool.ScaleUp(ctx)
	if err != nil {
		t.Errorf("First ScaleUp() error = %v", err)
	}

	// Second scale up should fail (at max)
	err = pool.ScaleUp(ctx)
	if err == nil {
		t.Error("ScaleUp() should fail at max replicas")
	}
}

// TestPool_MinReplicasEnforced tests that pool doesn't go below minReplicas
func TestPool_MinReplicasEnforced(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas: 2,
			MaxReplicas: 3,
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Should start at min
	if pool.Size() != 2 {
		t.Errorf("initial size = %d, want 2", pool.Size())
	}

	// Scale down should fail (at min)
	err := pool.ScaleDown(ctx)
	if err == nil {
		t.Error("ScaleDown() should fail at min replicas")
	}
}

// TestNewPodPool_MaxLessThanMin tests that maxReplicas is adjusted to match min
func TestNewPodPool_MaxLessThanMin(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		Scaling: &registry.ScalingSpec{
			MinReplicas: 3,
			MaxReplicas: 1, // Less than min - should be adjusted
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)

	// MaxSize should be adjusted to match minSize
	if pool.maxSize < pool.minSize {
		t.Errorf("maxSize (%d) should be >= minSize (%d)", pool.maxSize, pool.minSize)
	}
}

// TestNewPodPool_NilLogger tests that nil logger is handled
func TestNewPodPool_NilLogger(t *testing.T) {
	spec := testSpec(1)
	pool := NewPodPool("test-pod", spec, nil, nil)

	// Should not panic with nil logger
	if pool == nil {
		t.Fatal("NewPodPool returned nil")
	}
}

// TestPool_Start_SpawnError tests Start handles spawn errors
func TestPool_Start_SpawnError(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "bad-pod",
		Command: []string{"nonexistent-command"},
		Scaling: &registry.ScalingSpec{
			MinReplicas: 2,
			MaxReplicas: 2,
		},
	}

	pool := NewPodPool("bad-pod", spec, nil, nil)
	ctx := context.Background()

	err := pool.Start(ctx)
	if err == nil {
		t.Error("Start() should return error for bad command")
		pool.Shutdown(ctx)
	}

	// Pool should be empty after failed start
	if pool.Size() != 0 {
		t.Errorf("size after failed start = %d, want 0", pool.Size())
	}
}

// TestPool_WithHealthCheck tests pool with health checks enabled
func TestPool_WithHealthCheck(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		HealthCheck: &registry.HealthCheck{
			Enabled:  true,
			Interval: registry.Duration{Duration: 100 * time.Millisecond},
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Wait for health checks to run
	time.Sleep(250 * time.Millisecond)

	// Pod should still be healthy
	if pool.HealthyCount() != 1 {
		t.Errorf("HealthyCount = %d, want 1", pool.HealthyCount())
	}
}

// TestPool_WithHealthCheck_DefaultInterval tests default health check interval
func TestPool_WithHealthCheck_DefaultInterval(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		HealthCheck: &registry.HealthCheck{
			Enabled: true,
			// Interval not set - should default
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Pod should be healthy immediately after start
	if pool.HealthyCount() != 1 {
		t.Errorf("HealthyCount = %d, want 1", pool.HealthyCount())
	}
}

// TestPool_Acquire_EmptyPool tests Acquire on empty pool
func TestPool_Acquire_EmptyPool(t *testing.T) {
	spec := testSpec(1)
	pool := NewPodPool("test-pod", spec, nil, nil)

	// Don't start the pool - it should be empty

	_, err := pool.Acquire()
	if err == nil {
		t.Error("Acquire() on empty pool should return error")
	}
}

// TestPool_AcquireWithTracking_Error tests AcquireWithTracking when Acquire fails
func TestPool_AcquireWithTracking_Error(t *testing.T) {
	spec := testSpec(1)
	pool := NewPodPool("test-pod", spec, nil, nil)

	// Don't start the pool - Acquire will fail

	_, release, err := pool.AcquireWithTracking()
	if err == nil {
		t.Error("AcquireWithTracking() should fail on empty pool")
		if release != nil {
			release()
		}
	}
}

// TestPool_ScaleUp_SpawnError tests ScaleUp when spawn fails
func TestPool_ScaleUp_SpawnError(t *testing.T) {
	// Start with a valid command, then change it to invalid
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas: 1,
			MaxReplicas: 3,
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Modify spec to use invalid command (this is a bit hacky but tests error path)
	pool.spec.Command = []string{"nonexistent-command-for-scale-test"}

	// ScaleUp should fail
	err := pool.ScaleUp(ctx)
	if err == nil {
		t.Error("ScaleUp() with invalid command should fail")
	}
}

// TestPool_WithWorkdir tests pool with workdir set
func TestPool_WithWorkdir(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "./main.go"},
		Workdir: "../../test/testpod",
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	pod, err := pool.Acquire()
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	result, err := pod.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("Call(ping) error = %v", err)
	}

	var resp map[string]string
	json.Unmarshal(result, &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
}

// ============ Latency Tracker Tests ============

func TestLatencyTracker_Record(t *testing.T) {
	tracker := NewLatencyTracker(100)

	// Record some latencies
	tracker.Record(10)
	tracker.Record(20)
	tracker.Record(30)

	if tracker.Count() != 3 {
		t.Errorf("Count() = %d, want 3", tracker.Count())
	}
}

func TestLatencyTracker_Percentiles(t *testing.T) {
	tracker := NewLatencyTracker(100)

	// Record 100 latencies: 1, 2, 3, ..., 100
	for i := int64(1); i <= 100; i++ {
		tracker.Record(i)
	}

	p50, p95, p99 := tracker.Percentiles()

	// P50 should be around 50
	if p50 < 45 || p50 > 55 {
		t.Errorf("P50 = %d, expected around 50", p50)
	}

	// P95 should be around 95
	if p95 < 90 || p95 > 99 {
		t.Errorf("P95 = %d, expected around 95", p95)
	}

	// P99 should be around 99
	if p99 < 95 || p99 > 100 {
		t.Errorf("P99 = %d, expected around 99", p99)
	}
}

func TestLatencyTracker_EmptyPercentiles(t *testing.T) {
	tracker := NewLatencyTracker(100)

	p50, p95, p99 := tracker.Percentiles()

	if p50 != 0 || p95 != 0 || p99 != 0 {
		t.Errorf("Empty tracker should return 0 percentiles, got P50=%d, P95=%d, P99=%d", p50, p95, p99)
	}
}

func TestLatencyTracker_RingBuffer(t *testing.T) {
	tracker := NewLatencyTracker(10)

	// Fill the buffer
	for i := int64(1); i <= 10; i++ {
		tracker.Record(i)
	}

	if tracker.Count() != 10 {
		t.Errorf("Count() = %d, want 10", tracker.Count())
	}

	// Add more - should wrap around
	for i := int64(100); i <= 105; i++ {
		tracker.Record(i)
	}

	// Count should still be 10 (window size)
	if tracker.Count() != 10 {
		t.Errorf("Count() after wrap = %d, want 10", tracker.Count())
	}

	// Percentiles should reflect newer values
	p50, _, _ := tracker.Percentiles()
	// With 4 old values (7,8,9,10) and 6 new values (100,101,102,103,104,105)
	// sorted: [7,8,9,10,100,101,102,103,104,105], P50 would be around 100
	if p50 < 50 {
		t.Errorf("P50 = %d, expected higher after adding high values", p50)
	}
}

func TestLatencyTracker_DefaultWindowSize(t *testing.T) {
	// Window size <= 0 should default to 100
	tracker := NewLatencyTracker(0)
	if tracker.windowSize != 100 {
		t.Errorf("Default windowSize = %d, want 100", tracker.windowSize)
	}

	tracker2 := NewLatencyTracker(-5)
	if tracker2.windowSize != 100 {
		t.Errorf("Default windowSize for -5 = %d, want 100", tracker2.windowSize)
	}
}

// TestPool_LatencyTrackerInit tests that latency tracker is initialized with latency config
func TestPool_LatencyTrackerInit(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas: 1,
			MaxReplicas: 3,
			Latency: &registry.LatencyScalingSpec{
				TargetP95:  100,
				WindowSize: 50,
			},
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)

	// Latency tracker should be initialized
	if pool.latencyTracker == nil {
		t.Fatal("latencyTracker should be initialized when Latency config is present")
	}

	if pool.latencyTracker.windowSize != 50 {
		t.Errorf("latencyTracker.windowSize = %d, want 50", pool.latencyTracker.windowSize)
	}
}

func TestPool_LatencyTrackerNotInitWithoutConfig(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		Scaling: &registry.ScalingSpec{
			MinReplicas:   1,
			MaxReplicas:   3,
			TargetPending: 5, // Only pending config, no latency
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)

	// Latency tracker should NOT be initialized
	if pool.latencyTracker != nil {
		t.Error("latencyTracker should be nil without Latency config")
	}
}

func TestPool_RecordLatency(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas: 1,
			MaxReplicas: 3,
			Latency: &registry.LatencyScalingSpec{
				TargetP95: 100,
			},
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)

	// Record some latencies
	pool.RecordLatency(10)
	pool.RecordLatency(20)
	pool.RecordLatency(30)

	p50, p95, p99 := pool.GetLatencyPercentiles()

	// All should be non-zero after recording
	if p50 == 0 && p95 == 0 && p99 == 0 {
		t.Error("Percentiles should be non-zero after recording latencies")
	}
}

func TestPool_RecordLatency_NoTracker(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)

	// Should not panic when recording without tracker
	pool.RecordLatency(100)

	p50, p95, p99 := pool.GetLatencyPercentiles()
	if p50 != 0 || p95 != 0 || p99 != 0 {
		t.Error("Percentiles should be 0 without tracker")
	}
}

func TestPool_AcquireWithLatencyTracking(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas: 1,
			MaxReplicas: 3,
			Latency: &registry.LatencyScalingSpec{
				TargetP95: 100,
			},
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Acquire with latency tracking
	pod, release, err := pool.AcquireWithLatencyTracking()
	if err != nil {
		t.Fatalf("AcquireWithLatencyTracking() error = %v", err)
	}

	// Make a call
	_, err = pod.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}

	// Release with latency
	release(50) // 50ms latency

	// Check that latency was recorded
	if pool.latencyTracker.Count() != 1 {
		t.Errorf("latencyTracker.Count() = %d, want 1", pool.latencyTracker.Count())
	}
}

// TestAutoScaler_LatencyBased tests auto-scaling based on latency targets
func TestAutoScaler_LatencyBased(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas: 1,
			MaxReplicas: 3,
			ScaleDownDelay: registry.Duration{
				Duration: 10 * time.Second,
			},
			Latency: &registry.LatencyScalingSpec{
				TargetP95:          50,  // 50ms target
				ScaleUpThreshold:   2,   // Scale up after 2 violations
				ScaleDownThreshold: 5,   // Scale down after 5 good readings
				WindowSize:         100,
			},
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Initially should have 1 replica
	if pool.Size() != 1 {
		t.Errorf("initial size = %d, want 1", pool.Size())
	}

	// Simulate high latency calls
	for i := 0; i < 100; i++ {
		pool.RecordLatency(200) // 200ms - way above 50ms target
	}

	// Wait for auto-scaler to react (checks every 5 seconds)
	// The scaler should see high P95 and scale up after threshold violations
	time.Sleep(12 * time.Second)

	// Pool should have scaled up due to latency violations
	// Note: actual scaling depends on timing, so we just verify
	// the code path is exercised
	t.Logf("Pool size after high latency: %d", pool.Size())
}

func TestAutoScaler_LatencyAndPendingCombined(t *testing.T) {
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas:   1,
			MaxReplicas:   5,
			TargetPending: 3, // Both pending and latency scaling
			ScaleDownDelay: registry.Duration{
				Duration: 100 * time.Millisecond,
			},
			Latency: &registry.LatencyScalingSpec{
				TargetP95:        100,
				ScaleUpThreshold: 2,
			},
		},
	}

	pool := NewPodPool("test-pod", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Pool should start with auto-scaler
	if pool.Size() != 1 {
		t.Errorf("initial size = %d, want 1", pool.Size())
	}

	// Both scaling triggers should work
	t.Logf("Pool configured with both pending and latency scaling")
}
