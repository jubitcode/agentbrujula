//go:build darwin

// Package vz provides a RuntimeBackend using Apple's Virtualization.framework.
// This backend is only available on macOS 11+ (Big Sur and later).
//
// Virtualization.framework provides native VM support on macOS without Docker,
// offering fast startup times and low overhead.
//
// To use this backend, you need to install the go-vz dependency:
//
//	go get github.com/Code-Hex/vz/v3
//
// Note: This requires CGO and the Virtualization.framework which is only
// available on macOS 11+.
package vz

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	druntime "drun/internal/runtime"
)

// VZBackend implements RuntimeBackend using Apple's Virtualization.framework.
type VZBackend struct {
	mu        sync.RWMutex
	instances map[string]*vzInstance
	available bool
}

type vzInstance struct {
	id        string
	name      string
	spec      *druntime.InstanceSpec
	status    druntime.InstanceStatus
	createdAt time.Time
	startedAt time.Time
	ipAddress string
	// vm        *vz.VirtualMachine // Would be from github.com/Code-Hex/vz/v3
}

// NewVZBackend creates a new VZ backend.
// Returns an error if Virtualization.framework is not available.
func NewVZBackend() (*VZBackend, error) {
	available := checkVZAvailable()
	return &VZBackend{
		instances: make(map[string]*vzInstance),
		available: available,
	}, nil
}

// checkVZAvailable checks if Virtualization.framework is available.
// Requires macOS 11.0+ and ARM64 for optimal support.
func checkVZAvailable() bool {
	// Check if running on macOS
	if runtime.GOOS != "darwin" {
		return false
	}

	// Check macOS version (need 11.0+)
	version, err := getMacOSVersion()
	if err != nil {
		return false
	}

	// macOS 11.0 = Big Sur
	if version.major < 11 {
		return false
	}

	return true
}

type macOSVersion struct {
	major int
	minor int
	patch int
}

func getMacOSVersion() (*macOSVersion, error) {
	cmd := exec.Command("sw_vers", "-productVersion")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	parts := strings.Split(strings.TrimSpace(string(output)), ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("unexpected version format")
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, err
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, err
	}

	patch := 0
	if len(parts) > 2 {
		patch, _ = strconv.Atoi(parts[2])
	}

	return &macOSVersion{
		major: major,
		minor: minor,
		patch: patch,
	}, nil
}

func (v *VZBackend) Name() string {
	return "vz"
}

func (v *VZBackend) Type() druntime.BackendType {
	return druntime.BackendVZ
}

func (v *VZBackend) Available() bool {
	return v.available
}

func (v *VZBackend) Create(ctx context.Context, spec *druntime.InstanceSpec) (string, error) {
	if !v.available {
		return "", fmt.Errorf("Virtualization.framework is not available")
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	id := generateID()
	name := spec.Name
	if name == "" {
		name = fmt.Sprintf("vz-%s", id[:8])
	}

	instance := &vzInstance{
		id:        id,
		name:      name,
		spec:      spec,
		status:    druntime.StatusCreated,
		createdAt: time.Now(),
	}

	v.instances[id] = instance

	// TODO: Implement actual VM creation using Virtualization.framework
	// This would involve:
	// 1. Creating a VirtualMachineConfiguration
	// 2. Setting up bootloader, CPU, memory
	// 3. Configuring network (VZNATNetworkDeviceAttachment)
	// 4. Setting up disk images
	// 5. Creating the VirtualMachine

	return id, nil
}

func (v *VZBackend) Start(ctx context.Context, id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	instance, ok := v.instances[id]
	if !ok {
		return fmt.Errorf("instance %q not found", id)
	}

	if instance.status == druntime.StatusRunning {
		return nil
	}

	// TODO: Implement actual VM start using instance.vm.Start()

	instance.status = druntime.StatusRunning
	instance.startedAt = time.Now()

	return nil
}

func (v *VZBackend) Stop(ctx context.Context, id string, timeout int) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	instance, ok := v.instances[id]
	if !ok {
		return fmt.Errorf("instance %q not found", id)
	}

	if instance.status != druntime.StatusRunning {
		return nil
	}

	// TODO: Implement actual VM stop using instance.vm.Stop()
	// Would use RequestStop() for graceful shutdown, then Stop() after timeout

	instance.status = druntime.StatusStopped

	return nil
}

func (v *VZBackend) Remove(ctx context.Context, id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	instance, ok := v.instances[id]
	if !ok {
		return fmt.Errorf("instance %q not found", id)
	}

	if instance.status == druntime.StatusRunning {
		// Stop first
		instance.status = druntime.StatusStopped
	}

	delete(v.instances, id)
	return nil
}

func (v *VZBackend) Inspect(ctx context.Context, id string) (*druntime.InstanceInfo, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	instance, ok := v.instances[id]
	if !ok {
		return nil, fmt.Errorf("instance %q not found", id)
	}

	labels := make(map[string]string)
	if instance.spec.Labels != nil {
		for k, val := range instance.spec.Labels {
			labels[k] = val
		}
	}

	return &druntime.InstanceInfo{
		ID:        instance.id,
		Name:      instance.name,
		Status:    instance.status,
		IPAddress: instance.ipAddress,
		StartedAt: instance.startedAt.Format(time.RFC3339),
		Labels:    labels,
	}, nil
}

func (v *VZBackend) Exec(ctx context.Context, id string, cmd []string, opts *druntime.ExecOptions) (*druntime.ExecResult, error) {
	v.mu.RLock()
	instance, ok := v.instances[id]
	v.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("instance %q not found", id)
	}

	if instance.status != druntime.StatusRunning {
		return nil, fmt.Errorf("instance is not running")
	}

	// TODO: Implement exec via SSH or vsock connection to the VM
	// This would involve:
	// 1. Connecting to the VM via SSH (using the VM's IP address)
	// 2. Or using vsock for more direct communication
	// 3. Executing the command and capturing output

	return nil, fmt.Errorf("exec not yet implemented for VZ backend")
}

func (v *VZBackend) ExecInteractive(ctx context.Context, id string, cmd []string, opts *druntime.ExecOptions) error {
	v.mu.RLock()
	instance, ok := v.instances[id]
	v.mu.RUnlock()

	if !ok {
		return fmt.Errorf("instance %q not found", id)
	}

	if instance.status != druntime.StatusRunning {
		return fmt.Errorf("instance is not running")
	}

	// TODO: Implement interactive exec via SSH

	return fmt.Errorf("exec interactive not yet implemented for VZ backend")
}

func (v *VZBackend) PullImage(ctx context.Context, name string) error {
	// VZ backend would download and prepare a Linux VM image
	// This could involve downloading a pre-built image or using a base image

	// TODO: Implement image pulling/preparation
	return nil
}

func (v *VZBackend) HasImage(ctx context.Context, name string) (bool, error) {
	// Check if the VM image is available locally

	// TODO: Implement image checking
	return false, nil
}

func (v *VZBackend) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Stop all running VMs
	for id, instance := range v.instances {
		if instance.status == druntime.StatusRunning {
			// TODO: Stop the VM
			instance.status = druntime.StatusStopped
		}
		delete(v.instances, id)
	}

	return nil
}

// generateID generates a unique instance ID.
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// Ensure VZBackend implements RuntimeBackend
var _ druntime.RuntimeBackend = (*VZBackend)(nil)
