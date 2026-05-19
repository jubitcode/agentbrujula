package vz

import (
	"context"
	"runtime"
	"testing"

	druntime "drun/internal/runtime"
)

func TestVZBackend_ImplementsInterface(t *testing.T) {
	var _ druntime.RuntimeBackend = (*VZBackend)(nil)
}

func TestVZBackend_Name(t *testing.T) {
	b := &VZBackend{}
	if b.Name() != "vz" {
		t.Errorf("expected name 'vz', got %q", b.Name())
	}
}

func TestVZBackend_Type(t *testing.T) {
	b := &VZBackend{}
	if b.Type() != druntime.BackendVZ {
		t.Errorf("expected type BackendVZ, got %s", b.Type())
	}
}

func TestNewVZBackend_Darwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}

	if b == nil {
		t.Fatal("expected non-nil backend")
	}

	// Availability depends on macOS version
	t.Logf("VZ backend available: %v", b.Available())
}

func TestNewVZBackend_NonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("skipping non-macOS test")
	}

	_, err := NewVZBackend()
	if err == nil {
		t.Error("expected error on non-macOS platform")
	}
}

func TestVZBackend_Available_Darwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}

	// Just verify the method runs without panic
	available := b.Available()
	t.Logf("Available: %v", available)
}

func TestVZBackend_CreateInspectRemove(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}

	if !b.Available() {
		t.Skip("VZ backend not available on this system")
	}

	ctx := context.Background()
	spec := &druntime.InstanceSpec{
		Name: "test-vz-instance",
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	info, err := b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect instance: %v", err)
	}

	if info.Name != "test-vz-instance" {
		t.Errorf("expected name 'test-vz-instance', got %q", info.Name)
	}

	if info.Status != druntime.StatusCreated {
		t.Errorf("expected status Created, got %s", info.Status)
	}

	if err := b.Remove(ctx, id); err != nil {
		t.Fatalf("failed to remove instance: %v", err)
	}

	_, err = b.Inspect(ctx, id)
	if err == nil {
		t.Error("expected error inspecting removed instance")
	}
}

func TestVZBackend_StartStop(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}

	if !b.Available() {
		t.Skip("VZ backend not available on this system")
	}

	ctx := context.Background()
	spec := &druntime.InstanceSpec{
		Name: "test-vz-lifecycle",
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}
	defer b.Remove(ctx, id)

	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}

	info, err := b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect instance: %v", err)
	}

	if info.Status != druntime.StatusRunning {
		t.Errorf("expected status Running, got %s", info.Status)
	}

	if err := b.Stop(ctx, id, 5); err != nil {
		t.Fatalf("failed to stop instance: %v", err)
	}

	info, err = b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect after stop: %v", err)
	}

	if info.Status != druntime.StatusStopped {
		t.Errorf("expected status Stopped, got %s", info.Status)
	}
}

func TestVZBackend_NotFound(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}

	ctx := context.Background()

	if _, err := b.Inspect(ctx, "nonexistent"); err == nil {
		t.Error("expected error for nonexistent instance")
	}

	if err := b.Start(ctx, "nonexistent"); err == nil {
		t.Error("expected error for nonexistent instance")
	}

	if err := b.Stop(ctx, "nonexistent", 5); err == nil {
		t.Error("expected error for nonexistent instance")
	}

	if err := b.Remove(ctx, "nonexistent"); err == nil {
		t.Error("expected error for nonexistent instance")
	}
}

func TestVZBackend_Close(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}

	if !b.Available() {
		t.Skip("VZ backend not available on this system")
	}

	ctx := context.Background()
	spec := &druntime.InstanceSpec{
		Name: "test-vz-close",
	}

	_, err = b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Fatalf("failed to close backend: %v", err)
	}
}

func TestGetMacOSVersion(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	version, err := getMacOSVersion()
	if err != nil {
		t.Fatalf("failed to get macOS version: %v", err)
	}

	t.Logf("macOS version: %d.%d.%d", version.major, version.minor, version.patch)

	if version.major < 10 {
		t.Errorf("unexpected major version: %d", version.major)
	}
}

func TestVZBackend_Exec(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}
	defer b.Close()

	if !b.Available() {
		t.Skip("VZ backend not available on this system")
	}

	ctx := context.Background()
	spec := &druntime.InstanceSpec{
		Name: "test-vz-exec",
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}
	defer b.Remove(ctx, id)

	// Start the instance
	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}

	// Exec should return an error (not yet implemented)
	_, err = b.Exec(ctx, id, []string{"echo", "hello"}, nil)
	if err == nil {
		t.Error("expected error from unimplemented Exec")
	}
}

func TestVZBackend_ExecNotRunning(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}
	defer b.Close()

	if !b.Available() {
		t.Skip("VZ backend not available on this system")
	}

	ctx := context.Background()
	spec := &druntime.InstanceSpec{
		Name: "test-vz-exec-not-running",
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}
	defer b.Remove(ctx, id)

	// Don't start - exec on non-running instance should fail
	_, err = b.Exec(ctx, id, []string{"echo"}, nil)
	if err == nil {
		t.Error("expected error exec on non-running instance")
	}
}

func TestVZBackend_ExecNotFound(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}
	defer b.Close()

	ctx := context.Background()
	_, err = b.Exec(ctx, "nonexistent", []string{"echo"}, nil)
	if err == nil {
		t.Error("expected error for nonexistent instance")
	}
}

func TestVZBackend_ExecInteractive(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}
	defer b.Close()

	if !b.Available() {
		t.Skip("VZ backend not available on this system")
	}

	ctx := context.Background()
	spec := &druntime.InstanceSpec{
		Name: "test-vz-exec-interactive",
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}
	defer b.Remove(ctx, id)

	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}

	// ExecInteractive should return an error (not yet implemented)
	err = b.ExecInteractive(ctx, id, []string{"sh"}, nil)
	if err == nil {
		t.Error("expected error from unimplemented ExecInteractive")
	}
}

func TestVZBackend_ExecInteractiveNotRunning(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}
	defer b.Close()

	if !b.Available() {
		t.Skip("VZ backend not available on this system")
	}

	ctx := context.Background()
	spec := &druntime.InstanceSpec{
		Name: "test-vz-exec-int-not-running",
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}
	defer b.Remove(ctx, id)

	// ExecInteractive on non-running instance should fail
	err = b.ExecInteractive(ctx, id, []string{"sh"}, nil)
	if err == nil {
		t.Error("expected error for non-running instance")
	}
}

func TestVZBackend_ExecInteractiveNotFound(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}
	defer b.Close()

	ctx := context.Background()
	err = b.ExecInteractive(ctx, "nonexistent", []string{"sh"}, nil)
	if err == nil {
		t.Error("expected error for nonexistent instance")
	}
}

func TestVZBackend_PullImage(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}
	defer b.Close()

	ctx := context.Background()

	// PullImage is currently a no-op
	err = b.PullImage(ctx, "test-image")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVZBackend_HasImage(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}
	defer b.Close()

	ctx := context.Background()

	// HasImage currently returns false
	has, err := b.HasImage(ctx, "test-image")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if has {
		t.Error("expected HasImage to return false")
	}
}

func TestVZBackend_CreateNotAvailable(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	// Create a backend and set it to unavailable
	b := &VZBackend{
		instances: make(map[string]*vzInstance),
		available: false,
	}

	ctx := context.Background()
	_, err := b.Create(ctx, &druntime.InstanceSpec{Name: "test"})
	if err == nil {
		t.Error("expected error creating instance when backend unavailable")
	}
}

func TestVZBackend_StopAlreadyStopped(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}
	defer b.Close()

	if !b.Available() {
		t.Skip("VZ backend not available on this system")
	}

	ctx := context.Background()
	spec := &druntime.InstanceSpec{
		Name: "test-vz-stop-stopped",
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}
	defer b.Remove(ctx, id)

	// Stop without starting - should be no-op
	err = b.Stop(ctx, id, 5)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVZBackend_RemoveRunning(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}
	defer b.Close()

	if !b.Available() {
		t.Skip("VZ backend not available on this system")
	}

	ctx := context.Background()
	spec := &druntime.InstanceSpec{
		Name: "test-vz-remove-running",
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start instance: %v", err)
	}

	// Remove running instance should work
	err = b.Remove(ctx, id)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify it's gone
	_, err = b.Inspect(ctx, id)
	if err == nil {
		t.Error("expected error inspecting removed instance")
	}
}

func TestVZBackend_InspectWithLabels(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS-specific test")
	}

	b, err := NewVZBackend()
	if err != nil {
		t.Fatalf("failed to create VZ backend: %v", err)
	}
	defer b.Close()

	if !b.Available() {
		t.Skip("VZ backend not available on this system")
	}

	ctx := context.Background()
	spec := &druntime.InstanceSpec{
		Name: "test-vz-labels",
		Labels: map[string]string{
			"app":     "test",
			"version": "1.0",
		},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}
	defer b.Remove(ctx, id)

	info, err := b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect: %v", err)
	}

	if info.Labels["app"] != "test" {
		t.Errorf("expected label app=test, got %q", info.Labels["app"])
	}
}
