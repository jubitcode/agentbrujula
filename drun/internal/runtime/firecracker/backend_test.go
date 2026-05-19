package firecracker

import (
	"context"
	goruntime "runtime"
	"testing"

	"drun/internal/runtime"
)

func TestFirecrackerBackend_ImplementsInterface(t *testing.T) {
	var _ runtime.RuntimeBackend = (*FirecrackerBackend)(nil)
}

func TestFirecrackerBackend_Name(t *testing.T) {
	b := &FirecrackerBackend{}
	if b.Name() != "firecracker" {
		t.Errorf("expected name 'firecracker', got %q", b.Name())
	}
}

func TestFirecrackerBackend_Type(t *testing.T) {
	b := &FirecrackerBackend{}
	if b.Type() != runtime.BackendFirecracker {
		t.Errorf("expected type BackendFirecracker, got %s", b.Type())
	}
}

func TestNewFirecrackerBackend_Linux(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("skipping Linux-specific test")
	}

	b, err := NewFirecrackerBackend("")
	if err != nil {
		t.Fatalf("failed to create Firecracker backend: %v", err)
	}

	if b == nil {
		t.Fatal("expected non-nil backend")
	}

	// Availability depends on KVM and Firecracker binary
	t.Logf("Firecracker backend available: %v", b.Available())
}

func TestNewFirecrackerBackend_NonLinux(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping non-Linux test")
	}

	_, err := NewFirecrackerBackend("")
	if err == nil {
		t.Error("expected error on non-Linux platform")
	}
}

func TestFirecrackerBackend_Available_NonLinux(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping non-Linux test")
	}

	b := &FirecrackerBackend{}
	if b.Available() {
		t.Error("expected Available() to return false on non-Linux")
	}
}

func TestFirecrackerBackend_CreateInspectRemove(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("skipping Linux-specific test")
	}

	b, err := NewFirecrackerBackend("")
	if err != nil {
		t.Fatalf("failed to create Firecracker backend: %v", err)
	}

	if !b.Available() {
		t.Skip("Firecracker backend not available on this system")
	}

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name: "test-fc-instance",
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	info, err := b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect instance: %v", err)
	}

	if info.Name != "test-fc-instance" {
		t.Errorf("expected name 'test-fc-instance', got %q", info.Name)
	}

	if info.Status != runtime.StatusCreated {
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

func TestFirecrackerBackend_StartStop(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("skipping Linux-specific test")
	}

	b, err := NewFirecrackerBackend("")
	if err != nil {
		t.Fatalf("failed to create Firecracker backend: %v", err)
	}

	if !b.Available() {
		t.Skip("Firecracker backend not available on this system")
	}

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name: "test-fc-lifecycle",
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

	if info.Status != runtime.StatusRunning {
		t.Errorf("expected status Running, got %s", info.Status)
	}

	if err := b.Stop(ctx, id, 5); err != nil {
		t.Fatalf("failed to stop instance: %v", err)
	}

	info, err = b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect after stop: %v", err)
	}

	if info.Status != runtime.StatusStopped {
		t.Errorf("expected status Stopped, got %s", info.Status)
	}
}

func TestFirecrackerBackend_NotFound(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("skipping Linux-specific test")
	}

	b, err := NewFirecrackerBackend("")
	if err != nil {
		t.Fatalf("failed to create Firecracker backend: %v", err)
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

func TestFirecrackerBackend_Close(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("skipping Linux-specific test")
	}

	b, err := NewFirecrackerBackend("")
	if err != nil {
		t.Fatalf("failed to create Firecracker backend: %v", err)
	}

	if !b.Available() {
		t.Skip("Firecracker backend not available on this system")
	}

	ctx := context.Background()
	spec := &runtime.InstanceSpec{
		Name: "test-fc-close",
	}

	_, err = b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Fatalf("failed to close backend: %v", err)
	}
}

// Tests for stub implementation on non-Linux platforms

func TestFirecrackerBackend_Stub_Name(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping stub test on Linux")
	}

	b := &FirecrackerBackend{}
	if b.Name() != "firecracker" {
		t.Errorf("expected name 'firecracker', got %q", b.Name())
	}
}

func TestFirecrackerBackend_Stub_Type(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping stub test on Linux")
	}

	b := &FirecrackerBackend{}
	if b.Type() != runtime.BackendFirecracker {
		t.Errorf("expected type BackendFirecracker, got %s", b.Type())
	}
}

func TestFirecrackerBackend_Stub_Available(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping stub test on Linux")
	}

	b := &FirecrackerBackend{}
	if b.Available() {
		t.Error("expected Available() to return false on non-Linux")
	}
}

func TestFirecrackerBackend_Stub_Create(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping stub test on Linux")
	}

	b := &FirecrackerBackend{}
	ctx := context.Background()
	spec := &runtime.InstanceSpec{Name: "test"}

	_, err := b.Create(ctx, spec)
	if err == nil {
		t.Error("expected error from stub Create")
	}
}

func TestFirecrackerBackend_Stub_Start(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping stub test on Linux")
	}

	b := &FirecrackerBackend{}
	ctx := context.Background()

	err := b.Start(ctx, "test-id")
	if err == nil {
		t.Error("expected error from stub Start")
	}
}

func TestFirecrackerBackend_Stub_Stop(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping stub test on Linux")
	}

	b := &FirecrackerBackend{}
	ctx := context.Background()

	err := b.Stop(ctx, "test-id", 5)
	if err == nil {
		t.Error("expected error from stub Stop")
	}
}

func TestFirecrackerBackend_Stub_Remove(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping stub test on Linux")
	}

	b := &FirecrackerBackend{}
	ctx := context.Background()

	err := b.Remove(ctx, "test-id")
	if err == nil {
		t.Error("expected error from stub Remove")
	}
}

func TestFirecrackerBackend_Stub_Inspect(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping stub test on Linux")
	}

	b := &FirecrackerBackend{}
	ctx := context.Background()

	_, err := b.Inspect(ctx, "test-id")
	if err == nil {
		t.Error("expected error from stub Inspect")
	}
}

func TestFirecrackerBackend_Stub_Exec(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping stub test on Linux")
	}

	b := &FirecrackerBackend{}
	ctx := context.Background()

	_, err := b.Exec(ctx, "test-id", []string{"echo", "hello"}, nil)
	if err == nil {
		t.Error("expected error from stub Exec")
	}
}

func TestFirecrackerBackend_Stub_ExecInteractive(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping stub test on Linux")
	}

	b := &FirecrackerBackend{}
	ctx := context.Background()

	err := b.ExecInteractive(ctx, "test-id", []string{"sh"}, nil)
	if err == nil {
		t.Error("expected error from stub ExecInteractive")
	}
}

func TestFirecrackerBackend_Stub_PullImage(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping stub test on Linux")
	}

	b := &FirecrackerBackend{}
	ctx := context.Background()

	err := b.PullImage(ctx, "alpine:latest")
	if err == nil {
		t.Error("expected error from stub PullImage")
	}
}

func TestFirecrackerBackend_Stub_HasImage(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping stub test on Linux")
	}

	b := &FirecrackerBackend{}
	ctx := context.Background()

	_, err := b.HasImage(ctx, "alpine:latest")
	if err == nil {
		t.Error("expected error from stub HasImage")
	}
}

func TestFirecrackerBackend_Stub_Close(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("skipping stub test on Linux")
	}

	b := &FirecrackerBackend{}

	err := b.Close()
	if err != nil {
		t.Errorf("expected no error from stub Close, got %v", err)
	}
}
