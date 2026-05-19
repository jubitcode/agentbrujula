package supervisor

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"mcp-meta/internal/jsonrpc"
	"mcp-meta/internal/registry"
)

func TestNew(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sup := New(logger)
	if sup == nil {
		t.Fatal("New() returned nil")
	}
}

func TestSupervisor_GetPool_Empty(t *testing.T) {
	sup := New(nil)
	pool := sup.GetPool("nonexistent")
	if pool != nil {
		t.Error("GetPool() should return nil for nonexistent pool")
	}
}

func TestSupervisor_GetPod_Empty(t *testing.T) {
	sup := New(nil)
	pod := sup.GetPod("nonexistent")
	if pod != nil {
		t.Error("GetPod() should return nil for nonexistent pod")
	}
}

func TestSupervisor_Status_Empty(t *testing.T) {
	sup := New(nil)
	status := sup.Status()
	if status == nil {
		t.Error("Status() should not return nil")
	}
	if len(status) != 0 {
		t.Errorf("Status() should be empty, got %d", len(status))
	}
}

func TestSupervisor_PoolStatus_Empty(t *testing.T) {
	sup := New(nil)
	status := sup.PoolStatus()
	if status == nil {
		t.Error("PoolStatus() should not return nil")
	}
	if len(status) != 0 {
		t.Errorf("PoolStatus() should be empty, got %d", len(status))
	}
}

func TestSupervisor_Shutdown_Empty(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
	err := sup.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

func TestSupervisor_TerminatePod_NotFound(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
	err := sup.TerminatePod(ctx, "nonexistent")
	if err == nil {
		t.Error("TerminatePod() should return error for nonexistent pod")
	}
}

func TestSupervisor_SetInterPodHandler(t *testing.T) {
	sup := New(nil)
	handler := func(ctx context.Context, sourcePod, targetTool string, args json.RawMessage) (json.RawMessage, error) {
		return nil, nil
	}
	// Should not panic
	sup.SetInterPodHandler(handler)
}

func TestSupervisor_SetInterPodHandlerWithContract(t *testing.T) {
	sup := New(nil)
	handler := func(ctx context.Context, sourcePod, targetTool string, args json.RawMessage, contract *jsonrpc.ContractExpectation) (json.RawMessage, error) {
		return nil, nil
	}
	// Should not panic
	sup.SetInterPodHandlerWithContract(handler)
}

func TestSupervisor_EnsurePool(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
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

	pool, err := sup.EnsurePool(ctx, "test-pool", spec)
	if err != nil {
		t.Fatalf("EnsurePool() error = %v", err)
	}
	if pool == nil {
		t.Fatal("EnsurePool() returned nil pool")
	}
	defer sup.Shutdown(ctx)

	// Getting the same pool should return same instance
	pool2, err := sup.EnsurePool(ctx, "test-pool", spec)
	if err != nil {
		t.Fatalf("EnsurePool() second call error = %v", err)
	}
	if pool2 != pool {
		t.Error("EnsurePool() should return same pool instance")
	}

	// Check pool status
	status := sup.PoolStatus()
	if len(status) != 1 {
		t.Errorf("PoolStatus() should have 1 entry, got %d", len(status))
	}
}

func TestSupervisor_GetPool_AfterEnsure(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
	}

	pool1, err := sup.EnsurePool(ctx, "test-pool", spec)
	if err != nil {
		t.Fatalf("EnsurePool() error = %v", err)
	}
	defer sup.Shutdown(ctx)

	pool2 := sup.GetPool("test-pool")
	if pool2 == nil {
		t.Fatal("GetPool() returned nil after EnsurePool()")
	}
	if pool2 != pool1 {
		t.Error("GetPool() should return same pool")
	}
}

func TestSupervisor_PreWarmPods_Empty(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
	err := sup.PreWarmPods(ctx, nil)
	if err != nil {
		t.Errorf("PreWarmPods(nil) error = %v", err)
	}

	err = sup.PreWarmPods(ctx, []*registry.PodSpec{})
	if err != nil {
		t.Errorf("PreWarmPods([]) error = %v", err)
	}
}

func TestSupervisor_PreWarmPods(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
	specs := []*registry.PodSpec{
		{
			Name:    "test-pod-1",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			CallTimeout: registry.Duration{
				Duration: 30 * time.Second,
			},
		},
		{
			Name:    "test-pod-2",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			CallTimeout: registry.Duration{
				Duration: 30 * time.Second,
			},
		},
	}

	err := sup.PreWarmPods(ctx, specs)
	if err != nil {
		t.Fatalf("PreWarmPods() error = %v", err)
	}
	defer sup.Shutdown(ctx)

	// Both pods should be available
	status := sup.PoolStatus()
	if len(status) != 2 {
		t.Errorf("PoolStatus() should have 2 entries, got %d", len(status))
	}
}

func TestSupervisor_PreWarmPods_WithLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sup := New(logger)
	ctx := context.Background()
	specs := []*registry.PodSpec{
		{
			Name:    "test-pod",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			CallTimeout: registry.Duration{
				Duration: 30 * time.Second,
			},
			StartupTimeout: registry.Duration{
				Duration: 10 * time.Second,
			},
		},
	}

	err := sup.PreWarmPods(ctx, specs)
	if err != nil {
		t.Fatalf("PreWarmPods() error = %v", err)
	}
	defer sup.Shutdown(ctx)

	status := sup.PoolStatus()
	if len(status) != 1 {
		t.Errorf("PoolStatus() should have 1 entry, got %d", len(status))
	}
}

func TestSupervisor_Shutdown_WithPools(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
	}

	_, err := sup.EnsurePool(ctx, "test-pool", spec)
	if err != nil {
		t.Fatalf("EnsurePool() error = %v", err)
	}

	err = sup.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}

	// Status should be empty after shutdown
	status := sup.PoolStatus()
	if len(status) != 0 {
		t.Errorf("PoolStatus() should be empty after shutdown, got %d", len(status))
	}
}

func TestSupervisor_Status_WithPods(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas: 2,
			MaxReplicas: 2,
		},
	}

	_, err := sup.EnsurePool(ctx, "test-pool", spec)
	if err != nil {
		t.Fatalf("EnsurePool() error = %v", err)
	}
	defer sup.Shutdown(ctx)

	// Wait for all pods to start
	time.Sleep(100 * time.Millisecond)

	status := sup.Status()
	if len(status) < 1 {
		t.Errorf("Status() should have at least 1 pod entry, got %d", len(status))
	}
}

func TestSupervisor_EnsurePod(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
	}

	pod, err := sup.EnsurePod(ctx, "test-pool", spec)
	if err != nil {
		t.Fatalf("EnsurePod() error = %v", err)
	}
	if pod == nil {
		t.Fatal("EnsurePod() returned nil pod")
	}
	defer sup.Shutdown(ctx)

	// Pod should have a name
	name := pod.Name()
	if name == "" {
		t.Error("Pod.Name() should not be empty")
	}

	// Status should show the pod
	status := pod.Status()
	if status.State != PodStateRunning {
		t.Errorf("Pod state = %s, want running", status.State)
	}
}

func TestSupervisor_GetPod_AfterEnsure(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
	}

	_, err := sup.EnsurePool(ctx, "test-pool", spec)
	if err != nil {
		t.Fatalf("EnsurePool() error = %v", err)
	}
	defer sup.Shutdown(ctx)

	// GetPod should return a pod
	pod := sup.GetPod("test-pool")
	if pod == nil {
		t.Fatal("GetPod() returned nil after EnsurePool()")
	}
}

func TestSupervisor_TerminatePod_Success(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
	}

	_, err := sup.EnsurePool(ctx, "test-pool", spec)
	if err != nil {
		t.Fatalf("EnsurePool() error = %v", err)
	}

	// Terminate the pool
	err = sup.TerminatePod(ctx, "test-pool")
	if err != nil {
		t.Errorf("TerminatePod() error = %v", err)
	}

	// Pool should be gone
	pool := sup.GetPool("test-pool")
	if pool != nil {
		t.Error("GetPool() should return nil after termination")
	}
}

func TestSupervisor_SetInterPodHandler_WithPools(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
	}

	// Create pool first
	_, err := sup.EnsurePool(ctx, "test-pool", spec)
	if err != nil {
		t.Fatalf("EnsurePool() error = %v", err)
	}
	defer sup.Shutdown(ctx)

	// Set handler after pool exists
	handler := func(ctx context.Context, sourcePod, targetTool string, args json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"handled": true}`), nil
	}

	// Should propagate to existing pool without panic
	sup.SetInterPodHandler(handler)
}

func TestSupervisor_SetInterPodHandlerWithContract_WithPools(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
	}

	// Create pool first
	_, err := sup.EnsurePool(ctx, "test-pool", spec)
	if err != nil {
		t.Fatalf("EnsurePool() error = %v", err)
	}
	defer sup.Shutdown(ctx)

	// Set handler with contract after pool exists
	handler := func(ctx context.Context, sourcePod, targetTool string, args json.RawMessage, contract *jsonrpc.ContractExpectation) (json.RawMessage, error) {
		return json.RawMessage(`{"handled": true}`), nil
	}

	// Should propagate to existing pool without panic
	sup.SetInterPodHandlerWithContract(handler)
}

func TestSupervisor_EnsurePool_WithInterPodHandler(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()

	// Set handler before creating pool
	handler := func(ctx context.Context, sourcePod, targetTool string, args json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"handled": true}`), nil
	}
	sup.SetInterPodHandler(handler)

	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
	}

	pool, err := sup.EnsurePool(ctx, "test-pool", spec)
	if err != nil {
		t.Fatalf("EnsurePool() error = %v", err)
	}
	defer sup.Shutdown(ctx)

	if pool == nil {
		t.Fatal("EnsurePool() returned nil")
	}
}

func TestSupervisor_EnsurePool_WithInterPodHandlerWithContract(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()

	// Set handler with contract before creating pool
	handler := func(ctx context.Context, sourcePod, targetTool string, args json.RawMessage, contract *jsonrpc.ContractExpectation) (json.RawMessage, error) {
		return json.RawMessage(`{"handled": true}`), nil
	}
	sup.SetInterPodHandlerWithContract(handler)

	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
	}

	pool, err := sup.EnsurePool(ctx, "test-pool", spec)
	if err != nil {
		t.Fatalf("EnsurePool() error = %v", err)
	}
	defer sup.Shutdown(ctx)

	if pool == nil {
		t.Fatal("EnsurePool() returned nil")
	}
}

// TestEnsurePool_RecreatesUnhealthyPool tests that EnsurePool recreates a pool when all pods are unhealthy
func TestEnsurePool_RecreatesUnhealthyPool(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
	defer sup.Shutdown(ctx)

	spec := &registry.PodSpec{
		Name:    "test-pod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
	}

	// Create initial pool
	pool1, err := sup.EnsurePool(ctx, "test-pool", spec)
	if err != nil {
		t.Fatalf("First EnsurePool() error = %v", err)
	}

	// Crash the pod in the pool
	pod, err := pool1.Acquire()
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	pod.Call(ctx, "crash", nil)

	// Wait for crash detection
	time.Sleep(300 * time.Millisecond)

	// HealthyCount should be 0
	if pool1.HealthyCount() != 0 {
		t.Errorf("HealthyCount = %d, want 0 after crash", pool1.HealthyCount())
	}

	// EnsurePool again should recreate the pool
	pool2, err := sup.EnsurePool(ctx, "test-pool", spec)
	if err != nil {
		t.Fatalf("Second EnsurePool() error = %v", err)
	}

	// Should have healthy pods now
	if pool2.HealthyCount() == 0 {
		t.Error("New pool should have healthy pods")
	}
}

// TestPreWarmPods_CapturesFirstError tests that PreWarmPods captures and returns the first error
func TestPreWarmPods_CapturesFirstError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sup := New(logger)
	ctx := context.Background()
	defer sup.Shutdown(ctx)

	specs := []*registry.PodSpec{
		{
			Name:    "bad-pod",
			Command: []string{"nonexistent-command-that-fails"},
			StartupTimeout: registry.Duration{
				Duration: 1 * time.Second,
			},
		},
		{
			Name:    "good-pod",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			CallTimeout: registry.Duration{
				Duration: 30 * time.Second,
			},
		},
	}

	err := sup.PreWarmPods(ctx, specs)
	if err == nil {
		t.Error("PreWarmPods() should return error when first pod fails")
	}

	// Error should mention the bad pod
	if err != nil && !strings.Contains(err.Error(), "bad-pod") {
		t.Errorf("Error should mention bad-pod, got: %v", err)
	}
}

// TestPreWarmPods_WithScaling tests prewarm with scaled pods
func TestPreWarmPods_WithScaling(t *testing.T) {
	sup := New(nil)
	ctx := context.Background()
	defer sup.Shutdown(ctx)

	specs := []*registry.PodSpec{
		{
			Name:    "scaled-pod",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			CallTimeout: registry.Duration{
				Duration: 30 * time.Second,
			},
			Scaling: &registry.ScalingSpec{
				MinReplicas: 2,
				MaxReplicas: 2,
			},
		},
	}

	err := sup.PreWarmPods(ctx, specs)
	if err != nil {
		t.Fatalf("PreWarmPods() error = %v", err)
	}

	// Pool should exist with 2 replicas
	pool := sup.GetPool("scaled-pod")
	if pool == nil {
		t.Fatal("Pool should exist after prewarm")
	}
	if pool.Size() != 2 {
		t.Errorf("Pool size = %d, want 2", pool.Size())
	}
}
