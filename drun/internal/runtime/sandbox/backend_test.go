package sandbox

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"drun/internal/runtime"
)

func TestSandboxBackend_ImplementsInterface(t *testing.T) {
	var _ runtime.RuntimeBackend = (*SandboxBackend)(nil)
}

func TestSandboxBackend_Name(t *testing.T) {
	b := NewSandboxBackend("")
	if b.Name() != "sandbox" {
		t.Errorf("expected name 'sandbox', got %q", b.Name())
	}
}

func TestSandboxBackend_Type(t *testing.T) {
	b := NewSandboxBackend("")
	if b.Type() != runtime.BackendSandbox {
		t.Errorf("expected type BackendSandbox, got %s", b.Type())
	}
}

func TestSandboxBackend_Available(t *testing.T) {
	b := NewSandboxBackend("")
	if !b.Available() {
		t.Error("expected sandbox backend to always be available")
	}
}

func TestSandboxBackend_Create(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-instance",
		Command: []string{"echo", "hello"},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	if id == "" {
		t.Error("expected non-empty ID")
	}

	// Verify instance was created
	info, err := b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect instance: %v", err)
	}

	if info.Name != "test-instance" {
		t.Errorf("expected name 'test-instance', got %q", info.Name)
	}

	if info.Status != runtime.StatusCreated {
		t.Errorf("expected status Created, got %s", info.Status)
	}
}

func TestSandboxBackend_CreateWithDefaultName(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Command: []string{"echo", "hello"},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	info, err := b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect instance: %v", err)
	}

	if info.Name == "" {
		t.Error("expected auto-generated name")
	}
}

func TestSandboxBackend_StartAndStop(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-start-stop",
		Command: []string{"sleep", "60"},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	// Start
	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}

	// Verify running
	info, err := b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect instance: %v", err)
	}

	if info.Status != runtime.StatusRunning {
		t.Errorf("expected status Running, got %s", info.Status)
	}

	// Stop
	if err := b.Stop(ctx, id, 5); err != nil {
		t.Fatalf("failed to stop instance: %v", err)
	}

	// Give time for process to exit
	time.Sleep(100 * time.Millisecond)

	// Verify stopped
	info, err = b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect after stop: %v", err)
	}

	if info.Status == runtime.StatusRunning {
		t.Error("expected instance to be stopped")
	}
}

func TestSandboxBackend_Remove(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-remove",
		Command: []string{"sleep", "60"},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	// Start it
	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}

	// Remove should stop and clean up
	if err := b.Remove(ctx, id); err != nil {
		t.Fatalf("failed to remove instance: %v", err)
	}

	// Verify removed
	_, err = b.Inspect(ctx, id)
	if err == nil {
		t.Error("expected error inspecting removed instance")
	}
}

func TestSandboxBackend_Exec(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-exec",
		Command: []string{"sleep", "60"},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}
	defer b.Remove(ctx, id)

	// Execute command
	result, err := b.Exec(ctx, id, []string{"echo", "hello world"}, nil)
	if err != nil {
		t.Fatalf("failed to exec: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}

	if result.Stdout != "hello world\n" {
		t.Errorf("expected stdout 'hello world\\n', got %q", result.Stdout)
	}
}

func TestSandboxBackend_ExecWithOptions(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-exec-opts",
		Command: []string{"sleep", "60"},
		Env: map[string]string{
			"INSTANCE_VAR": "from_instance",
		},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}
	defer b.Remove(ctx, id)

	// Execute with custom working directory
	workDir := t.TempDir()
	testFile := filepath.Join(workDir, "test.txt")
	os.WriteFile(testFile, []byte("test content"), 0644)

	var stdout bytes.Buffer
	opts := &runtime.ExecOptions{
		WorkingDir: workDir,
		Stdout:     &stdout,
		Env:        []string{"EXEC_VAR=from_exec"},
	}

	result, err := b.Exec(ctx, id, []string{"cat", "test.txt"}, opts)
	if err != nil {
		t.Fatalf("failed to exec: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}

	if result.Stdout != "test content" {
		t.Errorf("expected stdout 'test content', got %q", result.Stdout)
	}
}

func TestSandboxBackend_ExecFailedCommand(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-exec-fail",
		Command: []string{"sleep", "60"},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}
	defer b.Remove(ctx, id)

	// Execute command that exits with error
	result, err := b.Exec(ctx, id, []string{"sh", "-c", "exit 42"}, nil)
	if err != nil {
		t.Fatalf("exec returned error: %v", err)
	}

	if result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.ExitCode)
	}
}

func TestSandboxBackend_HasImage(t *testing.T) {
	b := NewSandboxBackend("")
	defer b.Close()

	ctx := context.Background()

	// Sandbox doesn't use images, should always return true
	has, err := b.HasImage(ctx, "any-image")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !has {
		t.Error("expected HasImage to return true for sandbox")
	}
}

func TestSandboxBackend_PullImage(t *testing.T) {
	b := NewSandboxBackend("")
	defer b.Close()

	ctx := context.Background()

	// Sandbox doesn't use images, should be no-op
	err := b.PullImage(ctx, "any-image")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSandboxBackend_Close(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)

	ctx := context.Background()

	// Create and start a few instances
	for i := 0; i < 3; i++ {
		spec := &runtime.InstanceSpec{
			Name:    "test-close-" + string(rune('a'+i)),
			Command: []string{"sleep", "60"},
		}

		id, err := b.Create(ctx, spec)
		if err != nil {
			t.Fatalf("failed to create instance %d: %v", i, err)
		}

		if err := b.Start(ctx, id); err != nil {
			t.Fatalf("failed to start instance %d: %v", i, err)
		}
	}

	// Close should clean up all instances
	if err := b.Close(); err != nil {
		t.Fatalf("failed to close backend: %v", err)
	}

	// Verify all instances are gone
	instances := b.List()
	if len(instances) != 0 {
		t.Errorf("expected 0 instances after close, got %d", len(instances))
	}
}

func TestSandboxBackend_List(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()

	// Create a few instances
	for i := 0; i < 3; i++ {
		spec := &runtime.InstanceSpec{
			Name:    "test-list-" + string(rune('a'+i)),
			Command: []string{"sleep", "60"},
		}

		_, err := b.Create(ctx, spec)
		if err != nil {
			t.Fatalf("failed to create instance %d: %v", i, err)
		}
	}

	instances := b.List()
	if len(instances) != 3 {
		t.Errorf("expected 3 instances, got %d", len(instances))
	}
}

func TestSandboxBackend_InspectNotFound(t *testing.T) {
	b := NewSandboxBackend("")
	defer b.Close()

	ctx := context.Background()
	_, err := b.Inspect(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent instance")
	}
}

func TestSandboxBackend_StartNotFound(t *testing.T) {
	b := NewSandboxBackend("")
	defer b.Close()

	ctx := context.Background()
	err := b.Start(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent instance")
	}
}

func TestSandboxBackend_StopNotFound(t *testing.T) {
	b := NewSandboxBackend("")
	defer b.Close()

	ctx := context.Background()
	err := b.Stop(ctx, "nonexistent", 5)
	if err == nil {
		t.Error("expected error for nonexistent instance")
	}
}

func TestSandboxBackend_RemoveNotFound(t *testing.T) {
	b := NewSandboxBackend("")
	defer b.Close()

	ctx := context.Background()
	err := b.Remove(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent instance")
	}
}

func TestSandboxBackend_ExecNotFound(t *testing.T) {
	b := NewSandboxBackend("")
	defer b.Close()

	ctx := context.Background()
	_, err := b.Exec(ctx, "nonexistent", []string{"echo"}, nil)
	if err == nil {
		t.Error("expected error for nonexistent instance")
	}
}

func TestSandboxBackend_StartIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-start-twice",
		Command: []string{"sleep", "60"},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	// Start twice should be idempotent
	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}

	if err := b.Start(ctx, id); err != nil {
		t.Errorf("second start should succeed: %v", err)
	}
}

func TestSandboxBackend_ExecInteractive(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-exec-interactive",
		Command: []string{"sleep", "60"},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}
	defer b.Remove(ctx, id)

	// Test interactive exec with stdin/stdout
	var stdout bytes.Buffer
	stdin := bytes.NewBufferString("")
	opts := &runtime.ExecOptions{
		Stdin:  stdin,
		Stdout: &stdout,
		Stderr: &stdout,
	}

	err = b.ExecInteractive(ctx, id, []string{"echo", "interactive"}, opts)
	if err != nil {
		t.Fatalf("failed to exec interactive: %v", err)
	}

	if stdout.String() != "interactive\n" {
		t.Errorf("expected 'interactive\\n', got %q", stdout.String())
	}
}

func TestSandboxBackend_ExecInteractiveNotFound(t *testing.T) {
	b := NewSandboxBackend("")
	defer b.Close()

	ctx := context.Background()
	err := b.ExecInteractive(ctx, "nonexistent", []string{"echo"}, nil)
	if err == nil {
		t.Error("expected error for nonexistent instance")
	}
}

func TestSandboxBackend_ExecInteractiveWithEnv(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-exec-env",
		Command: []string{"sleep", "60"},
		Env: map[string]string{
			"TEST_VAR": "from_spec",
		},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}
	defer b.Remove(ctx, id)

	var stdout bytes.Buffer
	opts := &runtime.ExecOptions{
		Stdout: &stdout,
		Env:    []string{"EXEC_VAR=from_exec"},
	}

	err = b.ExecInteractive(ctx, id, []string{"sh", "-c", "echo $TEST_VAR $EXEC_VAR"}, opts)
	if err != nil {
		t.Fatalf("failed to exec: %v", err)
	}

	output := stdout.String()
	if output != "from_spec from_exec\n" {
		t.Errorf("expected 'from_spec from_exec\\n', got %q", output)
	}
}

func TestSandboxBackend_StopWithTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-stop-timeout",
		Command: []string{"sleep", "60"},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}

	// Stop with short timeout - process should be killed
	if err := b.Stop(ctx, id, 1); err != nil {
		t.Fatalf("failed to stop instance: %v", err)
	}

	// Wait for process to exit
	time.Sleep(200 * time.Millisecond)

	info, err := b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect: %v", err)
	}

	if info.Status == runtime.StatusRunning {
		t.Error("expected instance to be stopped")
	}
}

func TestSandboxBackend_StopNotRunning(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-stop-not-running",
		Command: []string{"sleep", "60"},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	// Stop without starting - should succeed (no-op)
	if err := b.Stop(ctx, id, 1); err != nil {
		t.Errorf("expected no error stopping non-running instance: %v", err)
	}
}

func TestSandboxBackend_StopWithContextCancel(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-stop-ctx-cancel",
		Command: []string{"sh", "-c", "trap '' TERM; sleep 60"},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}

	// Create a cancellable context with short timeout
	cancelCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	// Stop with context that will be cancelled
	err = b.Stop(cancelCtx, id, 10)
	// Either context.DeadlineExceeded or nil is acceptable
	if err != nil && err != context.DeadlineExceeded {
		t.Logf("stop returned: %v", err)
	}

	// Clean up
	b.Remove(ctx, id)
}

func TestSandboxBackend_InspectWithLabels(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-labels",
		Command: []string{"sleep", "60"},
		Labels: map[string]string{
			"app":     "test",
			"version": "1.0",
		},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	info, err := b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect: %v", err)
	}

	if info.Labels["app"] != "test" {
		t.Errorf("expected label app=test, got %q", info.Labels["app"])
	}
	if info.Labels["version"] != "1.0" {
		t.Errorf("expected label version=1.0, got %q", info.Labels["version"])
	}
}

func TestSandboxBackend_InspectProcessExited(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-inspect-exited",
		Command: []string{"sh", "-c", "exit 0"},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}

	// Wait for process to exit
	time.Sleep(200 * time.Millisecond)

	info, err := b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect: %v", err)
	}

	// Process should be stopped since it exited
	if info.Status == runtime.StatusRunning {
		t.Error("expected instance to be stopped after process exited")
	}
}

func TestSandboxBackend_CreateWithEnv(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewSandboxBackend(tmpDir)
	defer b.Close()

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name:    "test-env",
		Command: []string{"sleep", "60"},
		Env: map[string]string{
			"MY_VAR":    "my_value",
			"OTHER_VAR": "other_value",
		},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}
	defer b.Remove(ctx, id)

	// Verify environment is set
	result, err := b.Exec(ctx, id, []string{"sh", "-c", "echo $MY_VAR"}, nil)
	if err != nil {
		t.Fatalf("failed to exec: %v", err)
	}

	if result.Stdout != "my_value\n" {
		t.Errorf("expected 'my_value\\n', got %q", result.Stdout)
	}
}
