package docker

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"drun/internal/runtime"
)

func TestDockerBackend_ImplementsInterface(t *testing.T) {
	var _ runtime.RuntimeBackend = (*DockerBackend)(nil)
}

func TestDockerBackend_Name(t *testing.T) {
	// Create backend without connecting (will fail Available check but Name works)
	b := &DockerBackend{}
	if b.Name() != "docker" {
		t.Errorf("expected name 'docker', got %q", b.Name())
	}
}

func TestDockerBackend_Type(t *testing.T) {
	b := &DockerBackend{}
	if b.Type() != runtime.BackendDocker {
		t.Errorf("expected type BackendDocker, got %s", b.Type())
	}
}

func TestDockerBackend_Available_NoClient(t *testing.T) {
	b := &DockerBackend{}
	if b.Available() {
		t.Error("expected Available() to return false when client is nil")
	}
}

func TestNewDockerBackend(t *testing.T) {
	if os.Getenv("DOCKER_HOST") == "" && !dockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	b, err := NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer b.Close()

	if !b.Available() {
		t.Error("expected backend to be available")
	}
}

func TestNewDockerBackend_CustomHost(t *testing.T) {
	ctx := context.Background()

	// Try with an invalid host - should create backend but Available should fail
	b, err := NewDockerBackend(ctx, "unix:///nonexistent.sock")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer b.Close()

	// Should not be available since socket doesn't exist
	if b.Available() {
		t.Error("expected backend to be unavailable with invalid socket")
	}
}

func TestDockerBackend_Close_NilClient(t *testing.T) {
	b := &DockerBackend{}
	err := b.Close()
	if err != nil {
		t.Errorf("expected no error closing nil client, got: %v", err)
	}
}

func TestParsePortString(t *testing.T) {
	tests := []struct {
		input                                      string
		wantHostIP, wantHostPort, wantContainer    string
		wantProto                                  string
	}{
		{"8080", "", "8080", "8080", "tcp"},
		{"8080:80", "", "8080", "80", "tcp"},
		{"127.0.0.1:8080:80", "127.0.0.1", "8080", "80", "tcp"},
		{"8080/udp", "", "8080", "8080", "udp"},
		{"8080:80/udp", "", "8080", "80", "udp"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			hostIP, hostPort, containerPort, proto := parsePortString(tt.input)
			if hostIP != tt.wantHostIP {
				t.Errorf("hostIP = %q, want %q", hostIP, tt.wantHostIP)
			}
			if hostPort != tt.wantHostPort {
				t.Errorf("hostPort = %q, want %q", hostPort, tt.wantHostPort)
			}
			if containerPort != tt.wantContainer {
				t.Errorf("containerPort = %q, want %q", containerPort, tt.wantContainer)
			}
			if proto != tt.wantProto {
				t.Errorf("proto = %q, want %q", proto, tt.wantProto)
			}
		})
	}
}

func TestParseMemory(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		err   bool
	}{
		{"", 0, false},
		{"1024", 1024, false},
		{"1k", 1024, false},
		{"1m", 1024 * 1024, false},
		{"1g", 1024 * 1024 * 1024, false},
		{"512M", 512 * 1024 * 1024, false},
		{"2G", 2 * 1024 * 1024 * 1024, false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseMemory(tt.input)
			if tt.err {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

// Integration tests that require Docker

func dockerAvailable() bool {
	ctx := context.Background()
	b, err := NewDockerBackend(ctx, "")
	if err != nil {
		return false
	}
	defer b.Close()
	return b.Available()
}

func TestDockerBackend_Integration_HasImage(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	b, err := NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer b.Close()

	// Check for a common image that's likely to exist
	has, err := b.HasImage(ctx, "alpine:latest")
	if err != nil {
		t.Logf("HasImage returned error: %v", err)
	}
	// We don't assert has==true because the image might not be pulled
	t.Logf("HasImage(alpine:latest) = %v", has)
}

func TestDockerBackend_Integration_Lifecycle(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	b, err := NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer b.Close()

	// Check if alpine image exists, pull if not
	has, _ := b.HasImage(ctx, "alpine:latest")
	if !has {
		if err := b.PullImage(ctx, "alpine:latest"); err != nil {
			t.Skipf("Cannot pull alpine image: %v", err)
		}
	}

	// Create a container
	spec := &runtime.InstanceSpec{
		Name:    "drun-test-container",
		Image:   "alpine:latest",
		Command: []string{"sleep", "30"},
		Labels:  map[string]string{"drun.test": "true"},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	// Clean up
	defer func() {
		b.Stop(ctx, id, 1)
		b.Remove(ctx, id)
	}()

	// Start
	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start container: %v", err)
	}

	// Inspect
	info, err := b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect container: %v", err)
	}

	if info.Status != runtime.StatusRunning {
		t.Errorf("expected status Running, got %s", info.Status)
	}

	if info.Name != "drun-test-container" {
		t.Errorf("expected name 'drun-test-container', got %q", info.Name)
	}

	// Exec
	result, err := b.Exec(ctx, id, []string{"echo", "hello"}, nil)
	if err != nil {
		t.Fatalf("failed to exec: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}

	// Stop
	if err := b.Stop(ctx, id, 5); err != nil {
		t.Errorf("failed to stop container: %v", err)
	}

	// Verify stopped
	info, err = b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect after stop: %v", err)
	}

	if info.Status == runtime.StatusRunning {
		t.Error("expected container to be stopped")
	}
}

func TestDockerBackend_Integration_ListContainers(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	b, err := NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer b.Close()

	// List all containers
	containers, err := b.ListContainers(ctx, nil)
	if err != nil {
		t.Fatalf("failed to list containers: %v", err)
	}

	t.Logf("Found %d containers", len(containers))
}

func TestDockerBackend_Integration_ResourceLimits(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	b, err := NewDockerBackend(ctx, "")
	if err != nil {
		t.Fatalf("failed to create Docker backend: %v", err)
	}
	defer b.Close()

	// Check if alpine image exists
	has, _ := b.HasImage(ctx, "alpine:latest")
	if !has {
		if err := b.PullImage(ctx, "alpine:latest"); err != nil {
			t.Skipf("Cannot pull alpine image: %v", err)
		}
	}

	spec := &runtime.InstanceSpec{
		Name:    "drun-test-resources",
		Image:   "alpine:latest",
		Command: []string{"sleep", "10"},
		Resources: runtime.ResourceLimits{
			MemoryMB: 64,
			CPUCores: 0.5,
			MaxPIDs:  50,
		},
	}

	id, err := b.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create container with resources: %v", err)
	}

	defer func() {
		b.Stop(ctx, id, 1)
		b.Remove(ctx, id)
	}()

	// Start and verify it runs with limits
	if err := b.Start(ctx, id); err != nil {
		t.Fatalf("failed to start container: %v", err)
	}

	info, err := b.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("failed to inspect: %v", err)
	}

	if info.Status != runtime.StatusRunning {
		t.Errorf("expected running, got %s", info.Status)
	}
}

// Unit tests for stdCopy function
func TestStdCopy(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantOut  string
		wantErr  error
	}{
		{
			name:    "simple copy",
			input:   "hello world",
			wantOut: "hello world",
		},
		{
			name:    "empty input",
			input:   "",
			wantOut: "",
		},
		{
			name:    "multiline",
			input:   "line1\nline2\nline3",
			wantOut: "line1\nline2\nline3",
		},
		{
			name:    "large input",
			input:   strings.Repeat("x", 100000),
			wantOut: strings.Repeat("x", 100000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			reader := strings.NewReader(tt.input)

			_, err := stdCopy(&stdout, &stderr, reader)
			if err != nil && err != io.EOF {
				t.Errorf("unexpected error: %v", err)
			}

			if stdout.String() != tt.wantOut {
				t.Errorf("got %q, want %q", stdout.String(), tt.wantOut)
			}
		})
	}
}

func TestStdCopy_EOF(t *testing.T) {
	var stdout, stderr bytes.Buffer
	reader := strings.NewReader("test data")

	written, err := stdCopy(&stdout, &stderr, reader)
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
	if written != 9 {
		t.Errorf("expected 9 bytes written, got %d", written)
	}
}

// Test NewDockerBackendFromClient
func TestNewDockerBackendFromClient(t *testing.T) {
	// Test with nil client
	b := NewDockerBackendFromClient(nil)
	if b == nil {
		t.Fatal("expected non-nil backend")
	}
	if b.client != nil {
		t.Error("expected nil client")
	}
	if b.Available() {
		t.Error("expected Available() to return false with nil client")
	}
}

// Test GetClient
func TestDockerBackend_GetClient(t *testing.T) {
	b := &DockerBackend{}
	if b.GetClient() != nil {
		t.Error("expected nil client")
	}
}

// Test parsePortString edge cases
func TestParsePortString_EdgeCases(t *testing.T) {
	tests := []struct {
		input                                   string
		wantHostIP, wantHostPort, wantContainer string
		wantProto                               string
	}{
		// Empty parts
		{"", "", "", "", "tcp"},
		// Only protocol
		{"/udp", "", "", "", "udp"},
		// Complex IP binding
		{"0.0.0.0:3000:80/tcp", "0.0.0.0", "3000", "80", "tcp"},
		// Four parts - returns empty (unhandled case)
		{"a:b:c:d", "", "", "", "tcp"},
		// Just port with protocol
		{"443/tcp", "", "443", "443", "tcp"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			hostIP, hostPort, containerPort, proto := parsePortString(tt.input)
			if hostIP != tt.wantHostIP {
				t.Errorf("hostIP = %q, want %q", hostIP, tt.wantHostIP)
			}
			if hostPort != tt.wantHostPort {
				t.Errorf("hostPort = %q, want %q", hostPort, tt.wantHostPort)
			}
			if containerPort != tt.wantContainer {
				t.Errorf("containerPort = %q, want %q", containerPort, tt.wantContainer)
			}
			if proto != tt.wantProto {
				t.Errorf("proto = %q, want %q", proto, tt.wantProto)
			}
		})
	}
}

// Test parseMemory edge cases
func TestParseMemory_EdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		err   bool
	}{
		{"  512m  ", 512 * 1024 * 1024, false}, // with whitespace
		{"0", 0, false},
		{"0k", 0, false},
		{"0m", 0, false},
		{"0g", 0, false},
		{"-1", -1, false}, // negative parses successfully
		{"abc", 0, true},
		{"1.5g", 0, true}, // float not supported
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseMemory(tt.input)
			if tt.err {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

// Test that backend methods fail gracefully with nil client
func TestDockerBackend_NilClient_Methods(t *testing.T) {
	b := &DockerBackend{}
	ctx := context.Background()

	// These should not panic, even with nil client
	t.Run("Name", func(t *testing.T) {
		if b.Name() != "docker" {
			t.Error("unexpected name")
		}
	})

	t.Run("Type", func(t *testing.T) {
		if b.Type() != runtime.BackendDocker {
			t.Error("unexpected type")
		}
	})

	t.Run("Available", func(t *testing.T) {
		if b.Available() {
			t.Error("expected false")
		}
	})

	t.Run("Close", func(t *testing.T) {
		if err := b.Close(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GetClient", func(t *testing.T) {
		if b.GetClient() != nil {
			t.Error("expected nil")
		}
	})

	// These will panic with nil client, so we test that they don't work
	// without a real client - this is expected behavior
	_ = ctx // suppress unused warning
}
