package runtime

import (
	"context"
	"runtime"
	"testing"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if r.backends == nil {
		t.Error("expected backends map to be initialized")
	}
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()
	backend := newMockBackend("docker", BackendDocker, true)

	r.Register(BackendDocker, backend)

	b, ok := r.Get(BackendDocker)
	if !ok {
		t.Error("expected backend to be registered")
	}
	if b.Name() != "docker" {
		t.Errorf("expected name 'docker', got %q", b.Name())
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()
	backend := newMockBackend("docker", BackendDocker, true)

	r.Register(BackendDocker, backend)
	r.Unregister(BackendDocker)

	_, ok := r.Get(BackendDocker)
	if ok {
		t.Error("expected backend to be unregistered")
	}
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()

	// Get non-existent backend
	_, ok := r.Get(BackendDocker)
	if ok {
		t.Error("expected false for non-existent backend")
	}

	// Register and get
	backend := newMockBackend("docker", BackendDocker, true)
	r.Register(BackendDocker, backend)

	b, ok := r.Get(BackendDocker)
	if !ok {
		t.Error("expected true for registered backend")
	}
	if b == nil {
		t.Error("expected non-nil backend")
	}
}

func TestRegistry_GetOrError(t *testing.T) {
	r := NewRegistry()

	// Non-existent backend
	_, err := r.GetOrError(BackendDocker)
	if err == nil {
		t.Error("expected error for non-existent backend")
	}

	// Unavailable backend
	unavailable := newMockBackend("docker", BackendDocker, false)
	r.Register(BackendDocker, unavailable)

	_, err = r.GetOrError(BackendDocker)
	if err == nil {
		t.Error("expected error for unavailable backend")
	}

	// Available backend
	r.Unregister(BackendDocker)
	available := newMockBackend("docker", BackendDocker, true)
	r.Register(BackendDocker, available)

	b, err := r.GetOrError(BackendDocker)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if b == nil {
		t.Error("expected non-nil backend")
	}
}

func TestRegistry_SetPreferred(t *testing.T) {
	r := NewRegistry()

	// Register multiple backends
	r.Register(BackendDocker, newMockBackend("docker", BackendDocker, true))
	r.Register(BackendSandbox, newMockBackend("sandbox", BackendSandbox, true))

	// Set preferred
	r.SetPreferred(BackendSandbox)

	ctx := context.Background()
	b, err := r.AutoSelect(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if b.Type() != BackendSandbox {
		t.Errorf("expected sandbox backend, got %s", b.Type())
	}
}

func TestRegistry_AutoSelect_PreferredUnavailable(t *testing.T) {
	r := NewRegistry()

	// Register multiple backends
	r.Register(BackendDocker, newMockBackend("docker", BackendDocker, true))
	r.Register(BackendSandbox, newMockBackend("sandbox", BackendSandbox, false)) // unavailable

	// Set preferred to unavailable backend
	r.SetPreferred(BackendSandbox)

	ctx := context.Background()
	b, err := r.AutoSelect(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should fall back to docker
	if b.Type() != BackendDocker {
		t.Errorf("expected docker backend as fallback, got %s", b.Type())
	}
}

func TestRegistry_AutoSelect_PlatformPriority(t *testing.T) {
	r := NewRegistry()

	// Register all backends as available
	r.Register(BackendDocker, newMockBackend("docker", BackendDocker, true))
	r.Register(BackendSandbox, newMockBackend("sandbox", BackendSandbox, true))

	ctx := context.Background()
	b, err := r.AutoSelect(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Docker should be preferred over sandbox when no platform-specific backend available
	if b.Type() != BackendDocker {
		t.Errorf("expected docker backend, got %s", b.Type())
	}
}

func TestRegistry_AutoSelect_NoBackends(t *testing.T) {
	r := NewRegistry()

	ctx := context.Background()
	_, err := r.AutoSelect(ctx)
	if err == nil {
		t.Error("expected error when no backends available")
	}
}

func TestRegistry_AutoSelect_AllUnavailable(t *testing.T) {
	r := NewRegistry()

	r.Register(BackendDocker, newMockBackend("docker", BackendDocker, false))
	r.Register(BackendSandbox, newMockBackend("sandbox", BackendSandbox, false))

	ctx := context.Background()
	_, err := r.AutoSelect(ctx)
	if err == nil {
		t.Error("expected error when all backends unavailable")
	}
}

func TestRegistry_AutoSelect_LinuxPriority(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping Linux-specific test")
	}

	r := NewRegistry()

	r.Register(BackendDocker, newMockBackend("docker", BackendDocker, true))
	r.Register(BackendFirecracker, newMockBackend("firecracker", BackendFirecracker, true))
	r.Register(BackendSandbox, newMockBackend("sandbox", BackendSandbox, true))

	ctx := context.Background()
	b, err := r.AutoSelect(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Firecracker should be preferred on Linux
	if b.Type() != BackendFirecracker {
		t.Errorf("expected firecracker backend on Linux, got %s", b.Type())
	}
}

func TestRegistry_AutoSelect_DarwinPriority(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping Darwin-specific test")
	}

	r := NewRegistry()

	r.Register(BackendDocker, newMockBackend("docker", BackendDocker, true))
	r.Register(BackendVZ, newMockBackend("vz", BackendVZ, true))
	r.Register(BackendSandbox, newMockBackend("sandbox", BackendSandbox, true))

	ctx := context.Background()
	b, err := r.AutoSelect(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// VZ should be preferred on macOS
	if b.Type() != BackendVZ {
		t.Errorf("expected vz backend on Darwin, got %s", b.Type())
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()

	r.Register(BackendDocker, newMockBackend("docker", BackendDocker, true))
	r.Register(BackendSandbox, newMockBackend("sandbox", BackendSandbox, false))

	types := r.List()
	if len(types) != 2 {
		t.Errorf("expected 2 backends, got %d", len(types))
	}
}

func TestRegistry_ListAvailable(t *testing.T) {
	r := NewRegistry()

	r.Register(BackendDocker, newMockBackend("docker", BackendDocker, true))
	r.Register(BackendSandbox, newMockBackend("sandbox", BackendSandbox, false))

	types := r.ListAvailable()
	if len(types) != 1 {
		t.Errorf("expected 1 available backend, got %d", len(types))
	}
	if types[0] != BackendDocker {
		t.Errorf("expected docker backend, got %s", types[0])
	}
}

func TestRegistry_Close(t *testing.T) {
	r := NewRegistry()

	docker := newMockBackend("docker", BackendDocker, true)
	sandbox := newMockBackend("sandbox", BackendSandbox, true)

	r.Register(BackendDocker, docker)
	r.Register(BackendSandbox, sandbox)

	err := r.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !docker.closed {
		t.Error("expected docker backend to be closed")
	}
	if !sandbox.closed {
		t.Error("expected sandbox backend to be closed")
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()

	// Concurrent registration and access
	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			r.Register(BackendDocker, newMockBackend("docker", BackendDocker, true))
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			r.Get(BackendDocker)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			r.List()
		}
		done <- true
	}()

	// Wait for all goroutines
	<-done
	<-done
	<-done
}
