package runtime

import (
	"context"
	"testing"
)

// mockBackend is a test implementation of RuntimeBackend
type mockBackend struct {
	name      string
	backendType BackendType
	available bool
	closed    bool
}

func newMockBackend(name string, t BackendType, available bool) *mockBackend {
	return &mockBackend{
		name:        name,
		backendType: t,
		available:   available,
	}
}

func (m *mockBackend) Name() string        { return m.name }
func (m *mockBackend) Type() BackendType   { return m.backendType }
func (m *mockBackend) Available() bool     { return m.available }

func (m *mockBackend) Create(ctx context.Context, spec *InstanceSpec) (string, error) {
	return "mock-instance-id", nil
}

func (m *mockBackend) Start(ctx context.Context, id string) error { return nil }
func (m *mockBackend) Stop(ctx context.Context, id string, timeout int) error { return nil }
func (m *mockBackend) Remove(ctx context.Context, id string) error { return nil }

func (m *mockBackend) Inspect(ctx context.Context, id string) (*InstanceInfo, error) {
	return &InstanceInfo{
		ID:     id,
		Name:   "mock-instance",
		Status: StatusRunning,
	}, nil
}

func (m *mockBackend) Exec(ctx context.Context, id string, cmd []string, opts *ExecOptions) (*ExecResult, error) {
	return &ExecResult{ExitCode: 0}, nil
}

func (m *mockBackend) ExecInteractive(ctx context.Context, id string, cmd []string, opts *ExecOptions) error {
	return nil
}

func (m *mockBackend) PullImage(ctx context.Context, name string) error {
	return nil
}

func (m *mockBackend) HasImage(ctx context.Context, name string) (bool, error) {
	return true, nil
}

func (m *mockBackend) Close() error {
	m.closed = true
	return nil
}

// Test that mockBackend implements RuntimeBackend
func TestMockBackend_ImplementsInterface(t *testing.T) {
	var _ RuntimeBackend = (*mockBackend)(nil)
}

func TestBackendType_Constants(t *testing.T) {
	tests := []struct {
		name     string
		backend  BackendType
		expected string
	}{
		{"docker", BackendDocker, "docker"},
		{"firecracker", BackendFirecracker, "firecracker"},
		{"vz", BackendVZ, "vz"},
		{"sandbox", BackendSandbox, "sandbox"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.backend) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(tt.backend))
			}
		})
	}
}

func TestInstanceStatus_Constants(t *testing.T) {
	tests := []struct {
		status   InstanceStatus
		expected string
	}{
		{StatusCreated, "created"},
		{StatusRunning, "running"},
		{StatusPaused, "paused"},
		{StatusStopped, "stopped"},
		{StatusDead, "dead"},
		{StatusUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(tt.status))
			}
		})
	}
}

func TestInstanceSpec_Defaults(t *testing.T) {
	spec := &InstanceSpec{
		Name:    "test",
		Image:   "alpine:latest",
		Command: []string{"echo", "hello"},
	}

	if spec.Name != "test" {
		t.Errorf("expected name 'test', got %q", spec.Name)
	}
	if spec.Image != "alpine:latest" {
		t.Errorf("expected image 'alpine:latest', got %q", spec.Image)
	}
	if len(spec.Command) != 2 {
		t.Errorf("expected 2 command args, got %d", len(spec.Command))
	}
}

func TestResourceLimits(t *testing.T) {
	limits := ResourceLimits{
		CPUCores: 2.5,
		MemoryMB: 1024,
		DiskMB:   10240,
		MaxPIDs:  100,
	}

	if limits.CPUCores != 2.5 {
		t.Errorf("expected 2.5 CPUCores, got %f", limits.CPUCores)
	}
	if limits.MemoryMB != 1024 {
		t.Errorf("expected 1024 MemoryMB, got %d", limits.MemoryMB)
	}
}

func TestMount(t *testing.T) {
	mount := Mount{
		Source:   "/host/path",
		Target:   "/container/path",
		ReadOnly: true,
		Type:     "bind",
	}

	if mount.Source != "/host/path" {
		t.Errorf("expected source '/host/path', got %q", mount.Source)
	}
	if !mount.ReadOnly {
		t.Error("expected ReadOnly to be true")
	}
}

func TestPortBinding(t *testing.T) {
	port := PortBinding{
		HostIP:        "127.0.0.1",
		HostPort:      "8080",
		ContainerPort: "80",
		Protocol:      "tcp",
	}

	if port.HostIP != "127.0.0.1" {
		t.Errorf("expected HostIP '127.0.0.1', got %q", port.HostIP)
	}
	if port.Protocol != "tcp" {
		t.Errorf("expected Protocol 'tcp', got %q", port.Protocol)
	}
}
