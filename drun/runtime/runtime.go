// Package runtime provides the public API for drun's runtime backends.
// This package re-exports types from internal/runtime for use by external packages.
package runtime

import (
	"context"
	"io"

	internal "drun/internal/runtime"
)

// BackendType identifies the type of runtime backend.
type BackendType = internal.BackendType

// Backend type constants
const (
	BackendDocker      = internal.BackendDocker
	BackendFirecracker = internal.BackendFirecracker
	BackendVZ          = internal.BackendVZ
	BackendSandbox     = internal.BackendSandbox
)

// RuntimeBackend is the interface for container/VM runtime implementations.
type RuntimeBackend = internal.RuntimeBackend

// InstanceSpec defines what to create.
type InstanceSpec = internal.InstanceSpec

// InstanceInfo from Inspect.
type InstanceInfo = internal.InstanceInfo

// InstanceStatus represents the lifecycle status.
type InstanceStatus = internal.InstanceStatus

// Status constants
const (
	StatusCreated = internal.StatusCreated
	StatusRunning = internal.StatusRunning
	StatusPaused  = internal.StatusPaused
	StatusStopped = internal.StatusStopped
	StatusDead    = internal.StatusDead
	StatusUnknown = internal.StatusUnknown
)

// ResourceLimits for CPU, memory, etc.
type ResourceLimits = internal.ResourceLimits

// ExecOptions for command execution.
type ExecOptions = internal.ExecOptions

// ExecResult from command execution.
type ExecResult = internal.ExecResult

// Mount describes a volume mount.
type Mount = internal.Mount

// PortBinding describes a port binding.
type PortBinding = internal.PortBinding

// NetworkConfig describes network configuration.
type NetworkConfig = internal.NetworkConfig

// Registry manages available backends.
type Registry = internal.Registry

// NewRegistry creates a new backend registry.
func NewRegistry() *Registry {
	return internal.NewRegistry()
}

// Backend constructors (re-exported from internal packages)

// NewDockerBackend creates a new Docker backend.
func NewDockerBackend(ctx context.Context, host string) (RuntimeBackend, error) {
	// Import from internal/runtime/docker
	return newDockerBackendFromInternal(ctx, host)
}

// NewSandboxBackend creates a new Sandbox backend.
func NewSandboxBackend(baseDir string) RuntimeBackend {
	return newSandboxBackendFromInternal(baseDir)
}

// NewVZBackend creates a new VZ (Virtualization.framework) backend for macOS.
func NewVZBackend() (RuntimeBackend, error) {
	return newVZBackendFromInternal()
}

// NewFirecrackerBackend creates a new Firecracker backend for Linux.
func NewFirecrackerBackend(socketPath string) (RuntimeBackend, error) {
	return newFirecrackerBackendFromInternal(socketPath)
}

// Re-export io types needed for ExecOptions
type Reader = io.Reader
type Writer = io.Writer
