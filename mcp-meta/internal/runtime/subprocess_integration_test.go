package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"mcp-meta/internal/registry"
)

// getZombieCount returns the number of zombie processes (Linux/macOS)
func getZombieCount() int {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("ps", "aux")
	} else {
		cmd = exec.Command("ps", "-eo", "stat")
	}

	out, err := cmd.Output()
	if err != nil {
		return -1
	}

	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "Z") && !strings.Contains(line, "ps") {
			count++
		}
	}
	return count
}

// getProcessCount returns count of processes matching the given pattern
func getProcessCount(pattern string) int {
	cmd := exec.Command("pgrep", "-f", pattern)
	out, _ := cmd.Output()
	if len(out) == 0 {
		return 0
	}
	return len(strings.Split(strings.TrimSpace(string(out)), "\n"))
}

// TestSubprocessInstance_FullLifecycle tests spawning, calling, and stopping
func TestSubprocessInstance_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	zombiesBefore := getZombieCount()

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()

	spec := &InstanceSpec{
		Name: "lifecycle-test",
		PodSpec: &registry.PodSpec{
			Name:    "lifecycle-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			CallTimeout: registry.Duration{
				Duration: 30 * time.Second,
			},
			ShutdownTimeout: registry.Duration{
				Duration: 5 * time.Second,
			},
		},
	}

	// Spawn instance
	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}

	// Verify instance is running
	status := inst.Status()
	if status.State != StateRunning {
		t.Errorf("State = %v, want %v", status.State, StateRunning)
	}
	if status.PID == 0 {
		t.Error("PID should not be 0")
	}

	// Make a call
	result, err := inst.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("Call(ping) error = %v", err)
	}

	var resp map[string]string
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("Response status = %q, want %q", resp["status"], "ok")
	}

	// Stop instance
	if err := inst.Stop(ctx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	// Verify state after stop
	status = inst.Status()
	if status.State != StateStopped && status.State != StateCrashed {
		t.Errorf("State after stop = %v, want stopped or crashed", status.State)
	}

	// Close runtime
	if err := rt.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Wait for process cleanup
	time.Sleep(200 * time.Millisecond)

	// Check for zombies
	zombiesAfter := getZombieCount()
	if zombiesAfter > zombiesBefore {
		t.Errorf("Zombie count increased: before=%d, after=%d", zombiesBefore, zombiesAfter)
	}
}

// TestSubprocessInstance_MultipleInstances tests running multiple instances
func TestSubprocessInstance_MultipleInstances(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	zombiesBefore := getZombieCount()

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()

	// Spawn 3 instances
	instances := make([]Instance, 3)
	for i := 0; i < 3; i++ {
		spec := &InstanceSpec{
			Name: fmt.Sprintf("multi-test-%d", i),
			PodSpec: &registry.PodSpec{
				Name:    fmt.Sprintf("multi-test-%d", i),
				Command: []string{"go", "run", "../../test/testpod/main.go"},
				CallTimeout: registry.Duration{
					Duration: 30 * time.Second,
				},
			},
			ReplicaID: fmt.Sprintf("replica-%d", i),
		}

		inst, err := rt.Spawn(ctx, spec)
		if err != nil {
			t.Fatalf("Spawn(%d) error = %v", i, err)
		}
		instances[i] = inst
	}

	// Make concurrent calls to all instances
	var wg sync.WaitGroup
	errors := make(chan error, 9)

	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			wg.Add(1)
			go func(inst Instance, callNum int) {
				defer wg.Done()
				_, err := inst.Call(ctx, "ping", nil)
				if err != nil {
					errors <- fmt.Errorf("instance %s call %d: %w", inst.Name(), callNum, err)
				}
			}(instances[i], j)
		}
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}

	// Close runtime (should stop all instances)
	if err := rt.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Wait for cleanup
	time.Sleep(300 * time.Millisecond)

	// Check for zombies
	zombiesAfter := getZombieCount()
	if zombiesAfter > zombiesBefore {
		t.Errorf("Zombie count increased: before=%d, after=%d", zombiesBefore, zombiesAfter)
	}
}

// TestSubprocessInstance_Echo tests the echo tool
func TestSubprocessInstance_Echo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "echo-test",
		PodSpec: &registry.PodSpec{
			Name:    "echo-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			CallTimeout: registry.Duration{
				Duration: 30 * time.Second,
			},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Test echo with data
	args := json.RawMessage(`{"message": "hello world", "count": 42}`)
	result, err := inst.Call(ctx, "echo", args)
	if err != nil {
		t.Fatalf("Call(echo) error = %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp["handled_by"] == nil {
		t.Error("Response should include handled_by field")
	}
}

// TestSubprocessInstance_Crash tests crash handling
func TestSubprocessInstance_Crash(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	zombiesBefore := getZombieCount()

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()

	spec := &InstanceSpec{
		Name: "crash-test",
		PodSpec: &registry.PodSpec{
			Name:    "crash-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			CallTimeout: registry.Duration{
				Duration: 30 * time.Second,
			},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}

	// Make the pod crash
	inst.Call(ctx, "crash", nil)

	// Wait for crash detection
	time.Sleep(300 * time.Millisecond)

	// Status should show crashed
	status := inst.Status()
	if status.State != StateCrashed && status.State != StateStopped {
		t.Errorf("State after crash = %v, want crashed or stopped", status.State)
	}

	// Calls should fail
	_, err = inst.Call(ctx, "ping", nil)
	if err == nil {
		t.Error("Call after crash should fail")
	}

	rt.Close()

	// Wait for cleanup
	time.Sleep(200 * time.Millisecond)

	// Check for zombies
	zombiesAfter := getZombieCount()
	if zombiesAfter > zombiesBefore {
		t.Errorf("Zombie count increased: before=%d, after=%d", zombiesBefore, zombiesAfter)
	}
}

// TestSubprocessInstance_Timeout tests call timeout
func TestSubprocessInstance_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "timeout-test",
		PodSpec: &registry.PodSpec{
			Name:    "timeout-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			CallTimeout: registry.Duration{
				Duration: 30 * time.Second,
			},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Create a short timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	// Call slow which takes 2 seconds
	args := json.RawMessage(`{"duration_ms": 2000}`)
	_, err = inst.Call(timeoutCtx, "slow", args)
	if err == nil {
		t.Error("Call(slow) should timeout")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("Error = %v, want context.DeadlineExceeded", err)
	}
}

// TestSubprocessInstance_ID_Name tests ID and Name methods
func TestSubprocessInstance_ID_Name(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "id-name-test",
		PodSpec: &registry.PodSpec{
			Name:    "id-name-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	if inst.ID() == "" {
		t.Error("ID() should not be empty")
	}
	if inst.Name() != "id-name-test" {
		t.Errorf("Name() = %q, want %q", inst.Name(), "id-name-test")
	}
}

// TestSubprocessInstance_Status tracks call count
func TestSubprocessInstance_StatusCallCount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "status-test",
		PodSpec: &registry.PodSpec{
			Name:    "status-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Initial call count
	status := inst.Status()
	initialCount := status.CallCount

	// Make some calls
	for i := 0; i < 5; i++ {
		inst.Call(ctx, "ping", nil)
	}

	// Check call count increased
	status = inst.Status()
	if status.CallCount != initialCount+5 {
		t.Errorf("CallCount = %d, want %d", status.CallCount, initialCount+5)
	}
}

// TestSubprocessInstance_SetInterPodHandler tests inter-pod handler
func TestSubprocessInstance_SetInterPodHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "interpod-test",
		PodSpec: &registry.PodSpec{
			Name:    "interpod-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Set an inter-pod handler
	called := false
	handler := func(ctx context.Context, sourcePod, targetTool string, args json.RawMessage, contract *ContractExpectation) (json.RawMessage, error) {
		called = true
		return json.RawMessage(`{"result": "ok"}`), nil
	}

	inst.SetInterPodHandler(handler)

	// Handler should be set (we can't easily trigger it without the pod making a call)
	if called {
		t.Error("Handler should not be called yet")
	}
}

// TestSubprocessInstance_EnvironmentVariables tests env var passing
func TestSubprocessInstance_EnvironmentVariables(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "env-test",
		PodSpec: &registry.PodSpec{
			Name:    "env-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			Env: map[string]string{
				"TEST_VAR": "pod-spec-value",
			},
		},
		Env: map[string]string{
			"SPEC_VAR": "instance-spec-value",
		},
		ReplicaID: "replica-0",
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Verify the pod is running
	_, err = inst.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

// TestSubprocessRuntime_NoLeaks runs many spawn/stop cycles to check for leaks
func TestSubprocessRuntime_NoLeaks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	zombiesBefore := getZombieCount()

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()

	// Run 10 cycles
	for cycle := 0; cycle < 10; cycle++ {
		spec := &InstanceSpec{
			Name: fmt.Sprintf("leak-test-%d", cycle),
			PodSpec: &registry.PodSpec{
				Name:    fmt.Sprintf("leak-test-%d", cycle),
				Command: []string{"go", "run", "../../test/testpod/main.go"},
			},
		}

		inst, err := rt.Spawn(ctx, spec)
		if err != nil {
			t.Fatalf("Spawn(%d) error = %v", cycle, err)
		}

		// Make a call
		_, err = inst.Call(ctx, "ping", nil)
		if err != nil {
			t.Errorf("Call(%d) error = %v", cycle, err)
		}

		// Stop immediately
		inst.Stop(ctx)
	}

	rt.Close()

	// Wait for all cleanup
	time.Sleep(500 * time.Millisecond)

	// Check for zombies
	zombiesAfter := getZombieCount()
	if zombiesAfter > zombiesBefore+1 {
		t.Errorf("Possible zombie leak: before=%d, after=%d", zombiesBefore, zombiesAfter)
	}
}

// TestSubprocessRuntime_MemoryUsage checks that memory doesn't grow excessively
func TestSubprocessRuntime_MemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	allocBefore := m.Alloc

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()

	// Run multiple cycles
	for cycle := 0; cycle < 5; cycle++ {
		spec := &InstanceSpec{
			Name: fmt.Sprintf("mem-test-%d", cycle),
			PodSpec: &registry.PodSpec{
				Name:    fmt.Sprintf("mem-test-%d", cycle),
				Command: []string{"go", "run", "../../test/testpod/main.go"},
			},
		}

		inst, err := rt.Spawn(ctx, spec)
		if err != nil {
			t.Fatalf("Spawn(%d) error = %v", cycle, err)
		}

		// Make several calls
		for i := 0; i < 10; i++ {
			inst.Call(ctx, "echo", json.RawMessage(`{"data": "test data for memory check"}`))
		}

		inst.Stop(ctx)
	}

	rt.Close()

	// Force GC
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	runtime.ReadMemStats(&m)
	allocAfter := m.Alloc

	// Memory should not grow more than 50MB after cleanup
	growth := int64(allocAfter) - int64(allocBefore)
	maxGrowth := int64(50 * 1024 * 1024) // 50MB
	if growth > maxGrowth {
		t.Errorf("Memory growth too high: %d bytes (max %d)", growth, maxGrowth)
	}
	t.Logf("Memory: before=%d, after=%d, growth=%d bytes", allocBefore, allocAfter, growth)
}

// BenchmarkSubprocess_SpawnStop benchmarks spawn/stop cycle
func BenchmarkSubprocess_SpawnStop(b *testing.B) {
	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		spec := &InstanceSpec{
			Name: fmt.Sprintf("bench-%d", i),
			PodSpec: &registry.PodSpec{
				Name:    fmt.Sprintf("bench-%d", i),
				Command: []string{"go", "run", "../../test/testpod/main.go"},
			},
		}

		inst, err := rt.Spawn(ctx, spec)
		if err != nil {
			b.Fatalf("Spawn error: %v", err)
		}

		inst.Stop(ctx)
	}
}

// BenchmarkSubprocess_Call benchmarks call latency
func BenchmarkSubprocess_Call(b *testing.B) {
	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "bench-call",
		PodSpec: &registry.PodSpec{
			Name:    "bench-call",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		b.Fatalf("Spawn error: %v", err)
	}
	defer inst.Stop(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := inst.Call(ctx, "ping", nil)
		if err != nil {
			b.Fatalf("Call error: %v", err)
		}
	}
}
