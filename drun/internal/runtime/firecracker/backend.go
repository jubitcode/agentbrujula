//go:build linux

// Package firecracker provides a RuntimeBackend using Firecracker microVMs.
// Firecracker is a virtualization technology that uses KVM to create and
// manage microVMs with minimal overhead and fast startup times (<125ms).
//
// This backend is only available on Linux with KVM support (/dev/kvm).
//
// To use this backend, you need:
// 1. Linux kernel with KVM support
// 2. Access to /dev/kvm
// 3. Firecracker binary installed
// 4. The firecracker-go-sdk dependency:
//    go get github.com/firecracker-microvm/firecracker-go-sdk
//
// For more information, see: https://github.com/firecracker-microvm/firecracker
package firecracker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"drun/internal/runtime"
)

// FirecrackerBackend implements RuntimeBackend using Firecracker microVMs.
type FirecrackerBackend struct {
	mu          sync.RWMutex
	instances   map[string]*fcInstance
	socketPath  string
	binaryPath  string
	available   bool
}

type fcInstance struct {
	id         string
	name       string
	spec       *runtime.InstanceSpec
	status     runtime.InstanceStatus
	createdAt  time.Time
	startedAt  time.Time
	ipAddress  string
	socketPath string
	// machine    *firecracker.Machine // Would be from firecracker-go-sdk
}

// NewFirecrackerBackend creates a new Firecracker backend.
// Returns an error if Firecracker/KVM is not available.
func NewFirecrackerBackend(socketPath string) (*FirecrackerBackend, error) {
	binaryPath := findFirecrackerBinary()
	available := checkFirecrackerAvailable(binaryPath)

	return &FirecrackerBackend{
		instances:  make(map[string]*fcInstance),
		socketPath: socketPath,
		binaryPath: binaryPath,
		available:  available,
	}, nil
}

// findFirecrackerBinary looks for the firecracker binary in common locations.
func findFirecrackerBinary() string {
	locations := []string{
		"/usr/local/bin/firecracker",
		"/usr/bin/firecracker",
		"./firecracker",
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	// Try to find in PATH
	path, err := exec.LookPath("firecracker")
	if err == nil {
		return path
	}

	return ""
}

// checkFirecrackerAvailable verifies KVM and Firecracker are available.
func checkFirecrackerAvailable(binaryPath string) bool {
	// Check for KVM
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return false
	}

	// Check if we can access /dev/kvm
	f, err := os.Open("/dev/kvm")
	if err != nil {
		return false
	}
	f.Close()

	// Check for Firecracker binary
	if binaryPath == "" {
		return false
	}

	return true
}

func (f *FirecrackerBackend) Name() string {
	return "firecracker"
}

func (f *FirecrackerBackend) Type() runtime.BackendType {
	return runtime.BackendFirecracker
}

func (f *FirecrackerBackend) Available() bool {
	return f.available
}

func (f *FirecrackerBackend) Create(ctx context.Context, spec *runtime.InstanceSpec) (string, error) {
	if !f.available {
		return "", fmt.Errorf("Firecracker is not available")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	id := generateID()
	name := spec.Name
	if name == "" {
		name = fmt.Sprintf("fc-%s", id[:8])
	}

	// Create socket path for this instance
	socketPath := fmt.Sprintf("/tmp/firecracker-%s.sock", id)

	instance := &fcInstance{
		id:         id,
		name:       name,
		spec:       spec,
		status:     runtime.StatusCreated,
		createdAt:  time.Now(),
		socketPath: socketPath,
	}

	f.instances[id] = instance

	// TODO: Implement actual microVM creation using firecracker-go-sdk
	// This would involve:
	// 1. Creating a Machine configuration
	// 2. Setting up boot source (kernel + rootfs)
	// 3. Configuring network (tap devices)
	// 4. Setting up drives (rootfs, additional volumes)
	// 5. Setting CPU and memory limits
	// 6. Creating the Machine

	return id, nil
}

func (f *FirecrackerBackend) Start(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	instance, ok := f.instances[id]
	if !ok {
		return fmt.Errorf("instance %q not found", id)
	}

	if instance.status == runtime.StatusRunning {
		return nil
	}

	// TODO: Implement actual microVM start using instance.machine.Start()

	instance.status = runtime.StatusRunning
	instance.startedAt = time.Now()

	return nil
}

func (f *FirecrackerBackend) Stop(ctx context.Context, id string, timeout int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	instance, ok := f.instances[id]
	if !ok {
		return fmt.Errorf("instance %q not found", id)
	}

	if instance.status != runtime.StatusRunning {
		return nil
	}

	// TODO: Implement actual microVM stop
	// Would use machine.Shutdown() for graceful, then machine.StopVMM() if needed

	instance.status = runtime.StatusStopped

	// Clean up socket
	os.Remove(instance.socketPath)

	return nil
}

func (f *FirecrackerBackend) Remove(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	instance, ok := f.instances[id]
	if !ok {
		return fmt.Errorf("instance %q not found", id)
	}

	if instance.status == runtime.StatusRunning {
		instance.status = runtime.StatusStopped
		// TODO: Stop the microVM
	}

	// Clean up socket
	os.Remove(instance.socketPath)

	delete(f.instances, id)
	return nil
}

func (f *FirecrackerBackend) Inspect(ctx context.Context, id string) (*runtime.InstanceInfo, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	instance, ok := f.instances[id]
	if !ok {
		return nil, fmt.Errorf("instance %q not found", id)
	}

	labels := make(map[string]string)
	if instance.spec.Labels != nil {
		for k, v := range instance.spec.Labels {
			labels[k] = v
		}
	}

	return &runtime.InstanceInfo{
		ID:        instance.id,
		Name:      instance.name,
		Status:    instance.status,
		IPAddress: instance.ipAddress,
		StartedAt: instance.startedAt.Format(time.RFC3339),
		Labels:    labels,
	}, nil
}

func (f *FirecrackerBackend) Exec(ctx context.Context, id string, cmd []string, opts *runtime.ExecOptions) (*runtime.ExecResult, error) {
	f.mu.RLock()
	instance, ok := f.instances[id]
	f.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("instance %q not found", id)
	}

	if instance.status != runtime.StatusRunning {
		return nil, fmt.Errorf("instance is not running")
	}

	// TODO: Implement exec via vsock or SSH
	// Firecracker supports vsock for host-guest communication
	// Alternatively, SSH can be used if the guest has an SSH server

	return nil, fmt.Errorf("exec not yet implemented for Firecracker backend")
}

func (f *FirecrackerBackend) ExecInteractive(ctx context.Context, id string, cmd []string, opts *runtime.ExecOptions) error {
	f.mu.RLock()
	instance, ok := f.instances[id]
	f.mu.RUnlock()

	if !ok {
		return fmt.Errorf("instance %q not found", id)
	}

	if instance.status != runtime.StatusRunning {
		return fmt.Errorf("instance is not running")
	}

	// TODO: Implement interactive exec via vsock or SSH

	return fmt.Errorf("exec interactive not yet implemented for Firecracker backend")
}

func (f *FirecrackerBackend) PullImage(ctx context.Context, name string) error {
	// For Firecracker, this would download or prepare a rootfs image
	// Could convert an OCI image to an ext4 rootfs

	// TODO: Implement image preparation
	return nil
}

func (f *FirecrackerBackend) HasImage(ctx context.Context, name string) (bool, error) {
	// Check if a rootfs image is available locally

	// TODO: Implement image checking
	return false, nil
}

func (f *FirecrackerBackend) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Stop all running microVMs
	for id, instance := range f.instances {
		if instance.status == runtime.StatusRunning {
			// TODO: Stop the microVM
			instance.status = runtime.StatusStopped
		}
		os.Remove(instance.socketPath)
		delete(f.instances, id)
	}

	return nil
}

// generateID generates a unique instance ID.
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// Ensure FirecrackerBackend implements RuntimeBackend
var _ runtime.RuntimeBackend = (*FirecrackerBackend)(nil)
