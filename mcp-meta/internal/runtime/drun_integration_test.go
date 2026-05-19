package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	drunruntime "drun/runtime"
	"mcp-meta/internal/registry"
)

// drunDockerAvailable checks if Docker is available for testing
func drunDockerAvailable() bool {
	// Check if docker command exists
	_, err := exec.LookPath("docker")
	if err != nil {
		return false
	}

	// Check if docker daemon is running
	cmd := exec.Command("docker", "info")
	if err := cmd.Run(); err != nil {
		return false
	}

	return true
}

// ensureTestImage ensures the alpine image is available
func ensureTestImage(t *testing.T, backend drunruntime.RuntimeBackend) {
	ctx := context.Background()
	has, err := backend.HasImage(ctx, "alpine:latest")
	if err != nil {
		t.Logf("HasImage error: %v", err)
	}
	if !has {
		if err := backend.PullImage(ctx, "alpine:latest"); err != nil {
			t.Skipf("Cannot pull alpine image: %v", err)
		}
	}
}


// TestDrunRuntime_Name tests the runtime name
func TestDrunRuntime_Name(t *testing.T) {
	if !drunDockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	backend, err := drunruntime.NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer backend.Close()

	rt := NewDrunRuntime(backend, nil)
	name := rt.Name()
	if !strings.HasPrefix(name, "drun:") {
		t.Errorf("expected name to start with 'drun:', got %q", name)
	}
	if !strings.Contains(name, "docker") {
		t.Errorf("expected name to contain 'docker', got %q", name)
	}
}

// TestDrunRuntime_SpawnRequiresPodSpec tests that Spawn fails without PodSpec
func TestDrunRuntime_SpawnRequiresPodSpec(t *testing.T) {
	if !drunDockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	backend, err := drunruntime.NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer backend.Close()

	rt := NewDrunRuntime(backend, nil)

	spec := &InstanceSpec{
		Name: "test",
		// PodSpec is nil
	}

	_, err = rt.Spawn(ctx, spec)
	if err == nil {
		t.Error("expected error when PodSpec is nil")
	}
}

// TestDrunRuntime_SpawnRequiresImage tests that Spawn fails without image
func TestDrunRuntime_SpawnRequiresImage(t *testing.T) {
	if !drunDockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	backend, err := drunruntime.NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer backend.Close()

	rt := NewDrunRuntime(backend, nil)

	spec := &InstanceSpec{
		Name: "test",
		PodSpec: &registry.PodSpec{
			Name:    "test",
			Command: []string{"echo", "hello"},
			// Image is empty
		},
	}

	_, err = rt.Spawn(ctx, spec)
	if err == nil {
		t.Error("expected error when image is empty")
	}
}

// TestDrunRuntime_BasicLifecycle tests spawning and stopping a container
func TestDrunRuntime_BasicLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !drunDockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	backend, err := drunruntime.NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer backend.Close()

	ensureTestImage(t, backend)

	rt := NewDrunRuntime(backend, nil)
	defer rt.Close()

	spec := &InstanceSpec{
		Name:  "lifecycle-test",
		Image: "alpine:latest",
		PodSpec: &registry.PodSpec{
			Name:    "lifecycle-test",
			Image:   "alpine:latest",
			Command: []string{"cat"}, // Simple command that reads stdin
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

	// Verify instance info
	if inst.ID() == "" {
		t.Error("ID() should not be empty")
	}
	if inst.Name() != "lifecycle-test" {
		t.Errorf("Name() = %q, want %q", inst.Name(), "lifecycle-test")
	}

	// Verify status shows running
	status := inst.Status()
	if status.State != StateRunning {
		t.Errorf("State = %v, want %v", status.State, StateRunning)
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
}

// TestDrunRuntime_MultipleInstances tests running multiple containers concurrently
func TestDrunRuntime_MultipleInstances(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !drunDockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	backend, err := drunruntime.NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer backend.Close()

	ensureTestImage(t, backend)

	rt := NewDrunRuntime(backend, nil)
	defer rt.Close()

	// Spawn 3 instances
	instances := make([]Instance, 3)
	for i := range 3 {
		spec := &InstanceSpec{
			Name:      fmt.Sprintf("multi-test-%d", i),
			Image:     "alpine:latest",
			ReplicaID: fmt.Sprintf("replica-%d", i),
			PodSpec: &registry.PodSpec{
				Name:    fmt.Sprintf("multi-test-%d", i),
				Image:   "alpine:latest",
				Command: []string{"cat"},
			},
		}

		inst, err := rt.Spawn(ctx, spec)
		if err != nil {
			t.Fatalf("Spawn(%d) error = %v", i, err)
		}
		instances[i] = inst
	}

	// Verify all are running
	for i, inst := range instances {
		status := inst.Status()
		if status.State != StateRunning {
			t.Errorf("Instance %d state = %v, want %v", i, status.State, StateRunning)
		}
	}

	// Stop all instances
	for i, inst := range instances {
		if err := inst.Stop(ctx); err != nil {
			t.Errorf("Stop(%d) error = %v", i, err)
		}
	}
}

// TestDrunRuntime_EnvironmentVariables tests that env vars are passed to container
func TestDrunRuntime_EnvironmentVariables(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !drunDockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	backend, err := drunruntime.NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer backend.Close()

	ensureTestImage(t, backend)

	rt := NewDrunRuntime(backend, nil)
	defer rt.Close()

	spec := &InstanceSpec{
		Name:  "env-test",
		Image: "alpine:latest",
		PodSpec: &registry.PodSpec{
			Name:    "env-test",
			Image:   "alpine:latest",
			Command: []string{"cat"},
			Env: map[string]string{
				"TEST_VAR":  "from-podspec",
				"SHARED":    "podspec-value",
			},
		},
		Env: map[string]string{
			"INSTANCE_VAR": "from-instance",
			"SHARED":       "instance-value", // Should override PodSpec
		},
		ReplicaID: "replica-0",
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Verify instance is running
	status := inst.Status()
	if status.State != StateRunning {
		t.Errorf("State = %v, want %v", status.State, StateRunning)
	}

	// Note: We can't easily verify env vars inside the container without exec,
	// but we verify the container started successfully with the env configuration
}

// TestDrunRuntime_Close stops all instances
func TestDrunRuntime_Close(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !drunDockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	backend, err := drunruntime.NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}

	ensureTestImage(t, backend)

	rt := NewDrunRuntime(backend, nil)

	// Spawn 2 instances
	for i := range 2 {
		spec := &InstanceSpec{
			Name:  fmt.Sprintf("close-test-%d", i),
			Image: "alpine:latest",
			PodSpec: &registry.PodSpec{
				Name:    fmt.Sprintf("close-test-%d", i),
				Image:   "alpine:latest",
				Command: []string{"cat"},
			},
		}

		_, err := rt.Spawn(ctx, spec)
		if err != nil {
			t.Fatalf("Spawn(%d) error = %v", i, err)
		}
	}

	// Close should stop all instances without error
	if err := rt.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// TestDrunRuntime_Status tracks call count
func TestDrunRuntime_Status(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !drunDockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	backend, err := drunruntime.NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer backend.Close()

	ensureTestImage(t, backend)

	rt := NewDrunRuntime(backend, nil)
	defer rt.Close()

	spec := &InstanceSpec{
		Name:  "status-test",
		Image: "alpine:latest",
		PodSpec: &registry.PodSpec{
			Name:    "status-test",
			Image:   "alpine:latest",
			Command: []string{"cat"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	status := inst.Status()
	if status.ID == "" {
		t.Error("Status.ID should not be empty")
	}
	if status.Name != "status-test" {
		t.Errorf("Status.Name = %q, want %q", status.Name, "status-test")
	}
	if status.State != StateRunning {
		t.Errorf("Status.State = %v, want %v", status.State, StateRunning)
	}
	if status.StartedAt.IsZero() {
		t.Error("Status.StartedAt should not be zero")
	}
}

// TestDrunRuntime_SetInterPodHandler tests inter-pod handler setup
func TestDrunRuntime_SetInterPodHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !drunDockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	backend, err := drunruntime.NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer backend.Close()

	ensureTestImage(t, backend)

	rt := NewDrunRuntime(backend, nil)
	defer rt.Close()

	spec := &InstanceSpec{
		Name:  "interpod-test",
		Image: "alpine:latest",
		PodSpec: &registry.PodSpec{
			Name:    "interpod-test",
			Image:   "alpine:latest",
			Command: []string{"cat"},
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

	// Handler should be set but not called yet
	if called {
		t.Error("Handler should not be called yet")
	}
}

// TestDrunRuntime_NoLeaks spawns and stops many instances to check for leaks
func TestDrunRuntime_NoLeaks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !drunDockerAvailable() {
		t.Skip("Docker not available")
	}

	// Get initial container count
	containersBefore := getDockerContainerCount()

	ctx := context.Background()
	backend, err := drunruntime.NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer backend.Close()

	ensureTestImage(t, backend)

	rt := NewDrunRuntime(backend, nil)

	// Run 5 cycles
	for cycle := range 5 {
		spec := &InstanceSpec{
			Name:  fmt.Sprintf("leak-test-%d", cycle),
			Image: "alpine:latest",
			PodSpec: &registry.PodSpec{
				Name:    fmt.Sprintf("leak-test-%d", cycle),
				Image:   "alpine:latest",
				Command: []string{"cat"},
			},
		}

		inst, err := rt.Spawn(ctx, spec)
		if err != nil {
			t.Fatalf("Spawn(%d) error = %v", cycle, err)
		}

		// Verify running
		status := inst.Status()
		if status.State != StateRunning {
			t.Errorf("Cycle %d: State = %v, want %v", cycle, status.State, StateRunning)
		}

		// Stop immediately
		inst.Stop(ctx)
	}

	rt.Close()

	// Wait for cleanup
	time.Sleep(500 * time.Millisecond)

	// Check container count hasn't increased
	containersAfter := getDockerContainerCount()
	if containersAfter > containersBefore+1 {
		t.Errorf("Possible container leak: before=%d, after=%d", containersBefore, containersAfter)
	}
}

// getDockerContainerCount returns the number of docker containers
func getDockerContainerCount() int {
	cmd := exec.Command("docker", "ps", "-a", "-q")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0
	}
	return len(lines)
}

// TestDrunRuntime_ConcurrentSpawnStop tests concurrent spawn/stop operations
func TestDrunRuntime_ConcurrentSpawnStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !drunDockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	backend, err := drunruntime.NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer backend.Close()

	ensureTestImage(t, backend)

	rt := NewDrunRuntime(backend, nil)
	defer rt.Close()

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := range 5 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			spec := &InstanceSpec{
				Name:  fmt.Sprintf("concurrent-test-%d", idx),
				Image: "alpine:latest",
				PodSpec: &registry.PodSpec{
					Name:    fmt.Sprintf("concurrent-test-%d", idx),
					Image:   "alpine:latest",
					Command: []string{"cat"},
				},
			}

			inst, err := rt.Spawn(ctx, spec)
			if err != nil {
				errors <- fmt.Errorf("Spawn(%d): %w", idx, err)
				return
			}

			// Small delay
			time.Sleep(50 * time.Millisecond)

			if err := inst.Stop(ctx); err != nil {
				errors <- fmt.Errorf("Stop(%d): %w", idx, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

// BenchmarkDrunRuntime_SpawnStop benchmarks spawn/stop cycle
func BenchmarkDrunRuntime_SpawnStop(b *testing.B) {
	if !drunDockerAvailable() {
		b.Skip("Docker not available")
	}

	ctx := context.Background()
	backend, err := drunruntime.NewDockerBackend(ctx, "")
	if err != nil {
		b.Fatalf("failed to create Docker backend: %v", err)
	}
	defer backend.Close()

	// Ensure image is available
	if has, _ := backend.HasImage(ctx, "alpine:latest"); !has {
		if err := backend.PullImage(ctx, "alpine:latest"); err != nil {
			b.Skipf("Cannot pull alpine image: %v", err)
		}
	}

	rt := NewDrunRuntime(backend, nil)
	defer rt.Close()

	b.ResetTimer()
	i := 0
	for b.Loop() {
		spec := &InstanceSpec{
			Name:  fmt.Sprintf("bench-%d", i),
			Image: "alpine:latest",
			PodSpec: &registry.PodSpec{
				Name:    fmt.Sprintf("bench-%d", i),
				Image:   "alpine:latest",
				Command: []string{"cat"},
			},
		}

		inst, err := rt.Spawn(ctx, spec)
		if err != nil {
			b.Fatalf("Spawn error: %v", err)
		}

		inst.Stop(ctx)
		i++
	}
}
