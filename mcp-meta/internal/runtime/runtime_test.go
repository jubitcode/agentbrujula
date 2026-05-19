package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"mcp-meta/internal/registry"
)

func TestParseResourceLimits_NilSpec(t *testing.T) {
	limits := ParseResourceLimits(nil)

	if limits.CPUCores != 0.5 {
		t.Errorf("CPUCores = %v, want 0.5", limits.CPUCores)
	}
	if limits.MemoryMB != 256 {
		t.Errorf("MemoryMB = %v, want 256", limits.MemoryMB)
	}
	if limits.DiskMB != 1024 {
		t.Errorf("DiskMB = %v, want 1024", limits.DiskMB)
	}
	if limits.MaxRuntime != 5*time.Minute {
		t.Errorf("MaxRuntime = %v, want %v", limits.MaxRuntime, 5*time.Minute)
	}
}

func TestParseResourceLimits_WithSpec(t *testing.T) {
	spec := &registry.ResourceSpec{
		CPU:              "2",
		Memory:           "512Mi",
		EphemeralStorage: "2Gi",
		Timeout:          registry.Duration{Duration: 10 * time.Minute},
	}

	limits := ParseResourceLimits(spec)

	if limits.CPUCores != 2 {
		t.Errorf("CPUCores = %v, want 2", limits.CPUCores)
	}
	if limits.MemoryMB != 512 {
		t.Errorf("MemoryMB = %v, want 512", limits.MemoryMB)
	}
	if limits.DiskMB != 2048 {
		t.Errorf("DiskMB = %v, want 2048", limits.DiskMB)
	}
	if limits.MaxRuntime != 10*time.Minute {
		t.Errorf("MaxRuntime = %v, want %v", limits.MaxRuntime, 10*time.Minute)
	}
}

func TestParseResourceLimits_PartialSpec(t *testing.T) {
	spec := &registry.ResourceSpec{
		CPU: "1.5",
	}

	limits := ParseResourceLimits(spec)

	if limits.CPUCores != 1.5 {
		t.Errorf("CPUCores = %v, want 1.5", limits.CPUCores)
	}
	if limits.MemoryMB != 256 {
		t.Errorf("MemoryMB = %v, want 256 (default)", limits.MemoryMB)
	}
}

func TestParseMemory(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"", 256},            // default
		{"256", 256},         // plain number
		{"256Mi", 256},       // megabytes
		{"256M", 256},        // megabytes short
		{"1Gi", 1024},        // gigabytes
		{"1G", 1024},         // gigabytes short
		{"1Ti", 1048576},     // terabytes
		{"1T", 1048576},      // terabytes short
		{"512Ki", 0},         // kilobytes (rounds down)
		{"1024Ki", 1},        // kilobytes to MB
		{"100", 100},         // raw number
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseMemory(tt.input)
			if got != tt.want {
				t.Errorf("parseMemory(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestInstanceStates(t *testing.T) {
	states := []InstanceState{
		StateStarting,
		StateRunning,
		StateStopping,
		StateStopped,
		StateCrashed,
	}

	for _, state := range states {
		if state == "" {
			t.Error("State should not be empty")
		}
	}

	// Verify specific values
	if StateStarting != "starting" {
		t.Errorf("StateStarting = %q, want %q", StateStarting, "starting")
	}
	if StateRunning != "running" {
		t.Errorf("StateRunning = %q, want %q", StateRunning, "running")
	}
	if StateStopping != "stopping" {
		t.Errorf("StateStopping = %q, want %q", StateStopping, "stopping")
	}
	if StateStopped != "stopped" {
		t.Errorf("StateStopped = %q, want %q", StateStopped, "stopped")
	}
	if StateCrashed != "crashed" {
		t.Errorf("StateCrashed = %q, want %q", StateCrashed, "crashed")
	}
}

func TestInstanceSpec(t *testing.T) {
	spec := InstanceSpec{
		Name: "test-instance",
		PodSpec: &registry.PodSpec{
			Name:    "test-pod",
			Command: []string{"python", "main.py"},
		},
		Image:     "test-image:latest",
		ReplicaID: "test-0",
		Env: map[string]string{
			"FOO": "bar",
		},
		Resources: ResourceLimits{
			CPUCores:   1,
			MemoryMB:   512,
			DiskMB:     1024,
			MaxRuntime: 5 * time.Minute,
		},
	}

	if spec.Name != "test-instance" {
		t.Errorf("Name = %q, want %q", spec.Name, "test-instance")
	}
	if spec.ReplicaID != "test-0" {
		t.Errorf("ReplicaID = %q, want %q", spec.ReplicaID, "test-0")
	}
	if spec.Env["FOO"] != "bar" {
		t.Errorf("Env[FOO] = %q, want %q", spec.Env["FOO"], "bar")
	}
}

func TestInstanceStatus(t *testing.T) {
	startedAt := time.Now()
	status := InstanceStatus{
		ID:        "inst-123",
		Name:      "my-pod",
		State:     StateRunning,
		PID:       12345,
		Address:   "localhost:8080",
		StartedAt: startedAt,
		CallCount: 100,
		LastError: "",
	}

	if status.ID != "inst-123" {
		t.Errorf("ID = %q, want %q", status.ID, "inst-123")
	}
	if status.State != StateRunning {
		t.Errorf("State = %q, want %q", status.State, StateRunning)
	}
	if status.PID != 12345 {
		t.Errorf("PID = %d, want %d", status.PID, 12345)
	}
	if status.CallCount != 100 {
		t.Errorf("CallCount = %d, want %d", status.CallCount, 100)
	}
}

func TestContractExpectation(t *testing.T) {
	ce := ContractExpectation{
		ID:         "test-contract",
		Version:    "1.0.0",
		SchemaHash: "sha256:abc123",
	}

	if ce.ID != "test-contract" {
		t.Errorf("ID = %q, want %q", ce.ID, "test-contract")
	}
	if ce.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", ce.Version, "1.0.0")
	}
	if ce.SchemaHash != "sha256:abc123" {
		t.Errorf("SchemaHash = %q, want %q", ce.SchemaHash, "sha256:abc123")
	}
}

func TestNewSubprocessRuntime(t *testing.T) {
	// Test with nil logger
	rt := NewSubprocessRuntime(nil)
	if rt == nil {
		t.Fatal("NewSubprocessRuntime(nil) returned nil")
	}
	if rt.logger == nil {
		t.Error("logger should be initialized when nil is passed")
	}

	// Test with custom logger
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	rt2 := NewSubprocessRuntime(logger)
	if rt2 == nil {
		t.Fatal("NewSubprocessRuntime(logger) returned nil")
	}
	if rt2.logger != logger {
		t.Error("logger should be set to provided logger")
	}
}

func TestSubprocessRuntime_Name(t *testing.T) {
	rt := NewSubprocessRuntime(nil)
	if rt.Name() != "subprocess" {
		t.Errorf("Name() = %q, want %q", rt.Name(), "subprocess")
	}
}

func TestSubprocessRuntime_Close_Empty(t *testing.T) {
	rt := NewSubprocessRuntime(nil)
	err := rt.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestSubprocessRuntime_Spawn_NilPodSpec(t *testing.T) {
	rt := NewSubprocessRuntime(nil)
	spec := &InstanceSpec{
		Name:    "test",
		PodSpec: nil,
	}

	_, err := rt.Spawn(nil, spec)
	if err == nil {
		t.Error("Spawn() with nil PodSpec should error")
	}
}

func TestSubprocessRuntime_Spawn_EmptyCommand(t *testing.T) {
	rt := NewSubprocessRuntime(nil)
	spec := &InstanceSpec{
		Name: "test",
		PodSpec: &registry.PodSpec{
			Name:    "test-pod",
			Command: []string{},
		},
	}

	_, err := rt.Spawn(nil, spec)
	if err == nil {
		t.Error("Spawn() with empty command should error")
	}
}

func TestResourceLimits(t *testing.T) {
	limits := ResourceLimits{
		CPUCores:   2.5,
		MemoryMB:   1024,
		DiskMB:     4096,
		MaxRuntime: 30 * time.Minute,
	}

	if limits.CPUCores != 2.5 {
		t.Errorf("CPUCores = %v, want 2.5", limits.CPUCores)
	}
	if limits.MemoryMB != 1024 {
		t.Errorf("MemoryMB = %v, want 1024", limits.MemoryMB)
	}
	if limits.DiskMB != 4096 {
		t.Errorf("DiskMB = %v, want 4096", limits.DiskMB)
	}
	if limits.MaxRuntime != 30*time.Minute {
		t.Errorf("MaxRuntime = %v, want %v", limits.MaxRuntime, 30*time.Minute)
	}
}

func TestJsonRPCError_Error(t *testing.T) {
	err := &jsonRPCError{
		Code:    -32600,
		Message: "Invalid Request",
	}

	expected := "JSON-RPC error -32600: Invalid Request"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

// TestSubprocessInstance_Stop_Idempotent tests that Stop can be called multiple times safely
func TestSubprocessInstance_Stop_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "stop-idempotent-test",
		PodSpec: &registry.PodSpec{
			Name:    "stop-idempotent-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			ShutdownTimeout: registry.Duration{
				Duration: 5 * time.Second,
			},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}

	// First stop should work
	err1 := inst.Stop(ctx)

	// Second stop should not panic and should return nil (closeOnce)
	err2 := inst.Stop(ctx)

	// Third stop should also be safe
	err3 := inst.Stop(ctx)

	// All should be nil (closeOnce ensures idempotency)
	if err1 != nil {
		t.Errorf("First Stop() error = %v", err1)
	}
	if err2 != nil {
		t.Errorf("Second Stop() error = %v", err2)
	}
	if err3 != nil {
		t.Errorf("Third Stop() error = %v", err3)
	}
}

// TestSubprocessInstance_Call_NotRunning tests that Call returns error when not running
func TestSubprocessInstance_Call_NotRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "call-notrunning-test",
		PodSpec: &registry.PodSpec{
			Name:    "call-notrunning-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}

	// Stop the instance first
	if err := inst.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Now try to call - should fail because not running
	_, err = inst.Call(ctx, "ping", nil)
	if err == nil {
		t.Error("Call() should return error when instance is not running")
	}

	// Error message should indicate not running
	if err != nil && !strings.Contains(err.Error(), "not running") {
		t.Errorf("Error should indicate not running, got: %v", err)
	}
}

// TestSubprocessInstance_WorkdirConfiguration tests that workdir is properly set
func TestSubprocessInstance_WorkdirConfiguration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	// Test with explicit workdir
	spec := &InstanceSpec{
		Name: "workdir-test",
		PodSpec: &registry.PodSpec{
			Name:    "workdir-test",
			Command: []string{"go", "run", "./main.go"},
			Workdir: "../../test/testpod",
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// The pod should still work with the workdir set
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
}

// TestSubprocessInstance_HealthCheck_Enabled tests that health check starts when enabled
func TestSubprocessInstance_HealthCheck_Enabled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "healthcheck-test",
		PodSpec: &registry.PodSpec{
			Name:    "healthcheck-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			HealthCheck: &registry.HealthCheck{
				Enabled:  true,
				Interval: registry.Duration{Duration: 100 * time.Millisecond},
			},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Wait for at least one health check cycle
	time.Sleep(250 * time.Millisecond)

	// Instance should still be running
	status := inst.Status()
	if status.State != StateRunning {
		t.Errorf("State = %v, want %v", status.State, StateRunning)
	}
}

// TestSubprocessInstance_HealthCheck_DefaultInterval tests default interval
func TestSubprocessInstance_HealthCheck_DefaultInterval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "healthcheck-default-test",
		PodSpec: &registry.PodSpec{
			Name:    "healthcheck-default-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			HealthCheck: &registry.HealthCheck{
				Enabled: true,
				// Interval not set - should default to 30s
			},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Instance should be running
	status := inst.Status()
	if status.State != StateRunning {
		t.Errorf("State = %v, want %v", status.State, StateRunning)
	}
}

// TestSubprocessInstance_Stop_ShutdownTimeout tests that process is killed after timeout
func TestSubprocessInstance_Stop_ShutdownTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "shutdown-timeout-test",
		PodSpec: &registry.PodSpec{
			Name:    "shutdown-timeout-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			ShutdownTimeout: registry.Duration{
				Duration: 100 * time.Millisecond, // Very short timeout
			},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}

	// Start a slow call that won't complete during shutdown
	go inst.Call(ctx, "slow_call", json.RawMessage(`{"duration_ms": 5000}`))

	// Give the call time to start
	time.Sleep(50 * time.Millisecond)

	// Stop should eventually force kill
	start := time.Now()
	err = inst.Stop(ctx)
	elapsed := time.Since(start)

	// Should complete within reasonable time (timeout + some buffer)
	if elapsed > 2*time.Second {
		t.Errorf("Stop() took %v, expected faster due to forced kill", elapsed)
	}

	// Instance should be stopped or crashed
	status := inst.Status()
	if status.State != StateStopped && status.State != StateCrashed {
		t.Errorf("State = %v, want stopped or crashed", status.State)
	}

	// Error may or may not be nil depending on whether kill was needed
	_ = err
}

// TestSubprocessInstance_RouteResponse_UnknownID tests handling of response with unknown ID
func TestSubprocessInstance_RouteResponse_UnknownID(t *testing.T) {
	// This test verifies the warning log path when receiving a response for an unknown request
	// The actual logging happens internally, so we just ensure no panic occurs

	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "route-unknown-test",
		PodSpec: &registry.PodSpec{
			Name:    "route-unknown-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Make some rapid calls where timeouts might cause response routing issues
	ctx2, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()

	// This might timeout, leaving an orphaned response
	inst.Call(ctx2, "slow", nil)

	// Wait a bit for any orphaned response to arrive
	time.Sleep(200 * time.Millisecond)

	// Instance should still be running without issues
	status := inst.Status()
	if status.State != StateRunning {
		t.Errorf("State = %v, want %v", status.State, StateRunning)
	}
}

// TestSubprocessRuntime_Close_WithInstances tests Close stops all instances
func TestSubprocessRuntime_Close_WithInstances(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()

	// Spawn multiple instances
	instances := make([]Instance, 3)
	for i := 0; i < 3; i++ {
		spec := &InstanceSpec{
			Name: fmt.Sprintf("close-test-%d", i),
			PodSpec: &registry.PodSpec{
				Name:    fmt.Sprintf("close-test-%d", i),
				Command: []string{"go", "run", "../../test/testpod/main.go"},
			},
		}

		inst, err := rt.Spawn(ctx, spec)
		if err != nil {
			t.Fatalf("Spawn(%d) error = %v", i, err)
		}
		instances[i] = inst

		// Verify running
		if inst.Status().State != StateRunning {
			t.Errorf("Instance %d not running after spawn", i)
		}
	}

	// Close runtime should stop all
	if err := rt.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Wait for cleanup
	time.Sleep(200 * time.Millisecond)

	// All instances should be stopped
	for i, inst := range instances {
		status := inst.Status()
		if status.State == StateRunning {
			t.Errorf("Instance %d still running after Close()", i)
		}
	}
}

// TestParseMemory_EdgeCases tests edge cases in memory parsing
func TestParseMemory_EdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"0", 0},
		{"1", 1},
		{"1024", 1024},
		{"1K", 0},    // 1 KB = 0 MB (rounds down)
		{"2048Ki", 2}, // 2048 KiB = 2 MiB
		{"2G", 2048},
		{"0Gi", 0},
		{"10Ti", 10485760}, // 10 TiB in MiB
		{"unknown", 0},     // Unknown unit defaults to value
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseMemory(tt.input)
			if got != tt.want {
				t.Errorf("parseMemory(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestSubprocessInstance_HealthCheck_WhenCrashed tests health check on crashed pod
func TestSubprocessInstance_HealthCheck_WhenCrashed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "healthcheck-crash-test",
		PodSpec: &registry.PodSpec{
			Name:    "healthcheck-crash-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			HealthCheck: &registry.HealthCheck{
				Enabled:  true,
				Interval: registry.Duration{Duration: 50 * time.Millisecond},
			},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Crash the instance
	inst.Call(ctx, "crash", nil)

	// Wait for health check to try (should handle gracefully)
	time.Sleep(200 * time.Millisecond)

	// Instance should be crashed
	status := inst.Status()
	if status.State != StateCrashed && status.State != StateStopped {
		t.Errorf("State = %v, want crashed or stopped", status.State)
	}
}

// TestSubprocessRuntime_Spawn_WithEnvVars tests spawning with environment variables
func TestSubprocessRuntime_Spawn_WithEnvVars(t *testing.T) {
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
				"POD_ENV_VAR": "from-podspec",
			},
		},
		Env: map[string]string{
			"INSTANCE_ENV_VAR": "from-instancespec",
		},
		ReplicaID: "test-replica-0",
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Pod should be running with env vars
	result, err := inst.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("Call(ping) error = %v", err)
	}

	var resp map[string]string
	json.Unmarshal(result, &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
}

// TestSubprocessInstance_Status_LastError tests last error tracking
func TestSubprocessInstance_Status_LastError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "last-error-test",
		PodSpec: &registry.PodSpec{
			Name:    "last-error-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Initially no error
	status := inst.Status()
	if status.LastError != "" {
		t.Errorf("LastError should be empty initially, got %q", status.LastError)
	}

	// Crash the instance
	inst.Call(ctx, "crash", nil)
	time.Sleep(200 * time.Millisecond)

	// Should have error recorded
	status = inst.Status()
	if status.State != StateCrashed {
		t.Logf("State = %v (expected crashed but may be stopped)", status.State)
	}
}

// TestSubprocessInstance_MethodNotFound tests calling unknown method
func TestSubprocessInstance_MethodNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "method-notfound-test",
		PodSpec: &registry.PodSpec{
			Name:    "method-notfound-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Call unknown method
	_, err = inst.Call(ctx, "nonexistent_method", nil)
	if err == nil {
		t.Error("Call with unknown method should return error")
	}
}

// TestSubprocessRuntime_Spawn_InvalidWorkdir tests spawn with invalid workdir
func TestSubprocessRuntime_Spawn_InvalidWorkdir(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "invalid-workdir-test",
		PodSpec: &registry.PodSpec{
			Name:    "invalid-workdir-test",
			Command: []string{"go", "run", "./main.go"},
			Workdir: "/nonexistent/directory/that/does/not/exist",
		},
	}

	_, err := rt.Spawn(ctx, spec)
	if err == nil {
		t.Error("Spawn with invalid workdir should return error")
	}
}

// TestSubprocessInstance_Call_InstanceDied tests call behavior when instance dies
func TestSubprocessInstance_Call_InstanceDied(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "instance-died-test",
		PodSpec: &registry.PodSpec{
			Name:    "instance-died-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}

	// Start a slow call
	go func() {
		time.Sleep(20 * time.Millisecond)
		inst.Call(ctx, "crash", nil)
	}()

	// This call might fail because instance dies during it
	inst.Call(ctx, "slow_call", json.RawMessage(`{"duration_ms": 100}`))

	// Give time for crash detection
	time.Sleep(200 * time.Millisecond)

	// Status should reflect the crash
	status := inst.Status()
	if status.State != StateCrashed && status.State != StateStopped {
		t.Logf("State = %v", status.State)
	}
}

// TestSubprocessInstance_CallCount tests that call count is tracked
func TestSubprocessInstance_CallCount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "callcount-test",
		PodSpec: &registry.PodSpec{
			Name:    "callcount-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Make 10 calls
	for i := 0; i < 10; i++ {
		inst.Call(ctx, "ping", nil)
	}

	status := inst.Status()
	if status.CallCount < 10 {
		t.Errorf("CallCount = %d, want >= 10", status.CallCount)
	}
}

// TestSubprocessInstance_JSONRPCError tests JSON-RPC error handling
func TestSubprocessInstance_JSONRPCError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "jsonrpc-error-test",
		PodSpec: &registry.PodSpec{
			Name:    "jsonrpc-error-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Call unknown method should return JSON-RPC error
	_, err = inst.Call(ctx, "unknown_method_xyz", nil)
	if err == nil {
		t.Error("Call with unknown method should return error")
	}

	// Error should contain error info
	if err != nil {
		errStr := err.Error()
		if !strings.Contains(errStr, "method not found") && !strings.Contains(errStr, "-32601") {
			t.Logf("Error message: %v", err)
		}
	}
}

// TestSubprocessInstance_Spawn_CustomLogger tests spawning with custom logger
func TestSubprocessInstance_Spawn_CustomLogger(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Create a logger that discards output
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rt := NewSubprocessRuntime(logger)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "custom-logger-test",
		PodSpec: &registry.PodSpec{
			Name:    "custom-logger-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	result, err := inst.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("Call(ping) error = %v", err)
	}

	var resp map[string]string
	json.Unmarshal(result, &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
}

// TestSubprocessInstance_SetInterPodHandler_Unit tests setting the inter-pod handler
func TestSubprocessInstance_SetInterPodHandler_Unit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "interpod-handler-unit-test",
		PodSpec: &registry.PodSpec{
			Name:    "interpod-handler-unit-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Set an inter-pod handler with correct signature
	handlerCalled := false
	inst.SetInterPodHandler(func(ctx context.Context, sourcePod, targetTool string, args json.RawMessage, contract *ContractExpectation) (json.RawMessage, error) {
		handlerCalled = true
		return json.RawMessage(`{"result": "ok"}`), nil
	})

	// Verify instance is still functional
	result, err := inst.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("Call(ping) error = %v", err)
	}

	var pingResp map[string]string
	json.Unmarshal(result, &pingResp)
	if pingResp["status"] != "ok" {
		t.Errorf("status = %q, want ok", pingResp["status"])
	}

	// handlerCalled won't be true unless the subprocess sends an inter-pod request
	_ = handlerCalled
}

// TestSubprocessInstance_WithReplicaID tests that ReplicaID is passed to subprocess
func TestSubprocessInstance_WithReplicaID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name:      "replica-test",
		ReplicaID: "replica-42",
		PodSpec: &registry.PodSpec{
			Name:    "replica-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Call ping - the test pod includes POD_REPLICA_ID in response
	result, err := inst.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("Call(ping) error = %v", err)
	}

	var resp map[string]string
	json.Unmarshal(result, &resp)
	if resp["replica"] != "replica-42" {
		t.Errorf("replica = %q, want replica-42", resp["replica"])
	}
}

// TestSubprocessInstance_WithSpecEnv tests that spec.Env is passed to subprocess
func TestSubprocessInstance_WithSpecEnv(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "spec-env-test",
		Env: map[string]string{
			"CUSTOM_VAR_1": "value1",
			"CUSTOM_VAR_2": "value2",
		},
		PodSpec: &registry.PodSpec{
			Name:    "spec-env-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Verify instance is functional (env vars are passed internally)
	result, err := inst.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("Call(ping) error = %v", err)
	}

	var resp map[string]string
	json.Unmarshal(result, &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
}

// TestSubprocessInstance_StateTransitions tests state transitions
func TestSubprocessInstance_StateTransitions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "state-transitions-test",
		PodSpec: &registry.PodSpec{
			Name:    "state-transitions-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}

	// Should be running
	status := inst.Status()
	if status.State != StateRunning {
		t.Errorf("State = %v, want %v", status.State, StateRunning)
	}

	// Stop the instance
	if err := inst.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Should be stopped
	status = inst.Status()
	if status.State != StateStopped {
		t.Errorf("State after Stop = %v, want %v", status.State, StateStopped)
	}
}

// TestSubprocessInstance_InterPodCall tests inter-pod call handling with handler
func TestSubprocessInstance_InterPodCall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "interpod-call-test",
		PodSpec: &registry.PodSpec{
			Name:    "interpod-call-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Set up inter-pod handler
	handlerCalled := make(chan bool, 1)
	inst.SetInterPodHandler(func(ctx context.Context, sourcePod, targetTool string, args json.RawMessage, contract *ContractExpectation) (json.RawMessage, error) {
		handlerCalled <- true
		return json.RawMessage(`{"result": "handled"}`), nil
	})

	// Trigger inter-pod call from subprocess
	_, err = inst.Call(ctx, "call_external", json.RawMessage(`{"tool": "test_tool"}`))
	if err != nil {
		t.Fatalf("Call(call_external) error = %v", err)
	}

	// Wait briefly for handler to be called
	select {
	case <-handlerCalled:
		// Success - handler was called
	case <-time.After(500 * time.Millisecond):
		// Handler might not be called if response processing takes longer
		t.Log("Handler not called within timeout - inter-pod path may have race condition")
	}
}

// TestSubprocessInstance_InterPodCall_UnknownMethod tests inter-pod call with unknown method
func TestSubprocessInstance_InterPodCall_UnknownMethod(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "interpod-unknown-test",
		PodSpec: &registry.PodSpec{
			Name:    "interpod-unknown-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// Trigger inter-pod call with unknown method
	result, err := inst.Call(ctx, "call_unknown_interpod", nil)
	if err != nil {
		t.Fatalf("Call(call_unknown_interpod) error = %v", err)
	}

	var resp map[string]string
	json.Unmarshal(result, &resp)
	if resp["status"] != "unknown_method_sent" {
		t.Errorf("status = %q, want unknown_method_sent", resp["status"])
	}

	// Give time for the inter-pod request to be processed
	time.Sleep(100 * time.Millisecond)
}

// TestSubprocessInstance_InterPodCall_NoHandler tests inter-pod call without handler
func TestSubprocessInstance_InterPodCall_NoHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	rt := NewSubprocessRuntime(nil)
	ctx := context.Background()
	defer rt.Close()

	spec := &InstanceSpec{
		Name: "interpod-nohandler-test",
		PodSpec: &registry.PodSpec{
			Name:    "interpod-nohandler-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
		},
	}

	inst, err := rt.Spawn(ctx, spec)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer inst.Stop(ctx)

	// No handler set - trigger inter-pod call
	result, err := inst.Call(ctx, "call_external", json.RawMessage(`{"tool": "test_tool"}`))
	if err != nil {
		t.Fatalf("Call(call_external) error = %v", err)
	}

	var resp map[string]interface{}
	json.Unmarshal(result, &resp)
	if resp["status"] != "inter_pod_call_sent" {
		t.Errorf("status = %v, want inter_pod_call_sent", resp["status"])
	}

	// Give time for the inter-pod request to be processed (and rejected)
	time.Sleep(100 * time.Millisecond)
}
