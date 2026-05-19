// Package runtime provides abstractions for different execution environments.
// This enables running pods as subprocesses (local dev) or in containers (production).
package runtime

import (
	"context"
	"encoding/json"
	"time"

	"mcp-meta/internal/registry"
)

// Runtime is the interface for spawning and managing execution environments.
type Runtime interface {
	// Name returns the runtime identifier (e.g., "subprocess", "firecracker", "gvisor").
	Name() string

	// Spawn creates a new isolated execution environment for the given spec.
	Spawn(ctx context.Context, spec *InstanceSpec) (Instance, error)

	// Close releases all resources held by the runtime.
	Close() error
}

// InstanceSpec defines the configuration for spawning an instance.
type InstanceSpec struct {
	// Name is the unique identifier for this instance.
	Name string

	// PodSpec contains the pod configuration from the manifest.
	PodSpec *registry.PodSpec

	// Image is the container image to use (for container runtimes).
	// If empty, uses PodSpec.Command for subprocess runtime.
	Image string

	// Resources defines resource limits.
	Resources ResourceLimits

	// Environment variables to set.
	Env map[string]string

	// ReplicaID for scaled pods (e.g., "my-pod-0", "my-pod-1").
	ReplicaID string
}

// ResourceLimits defines resource constraints for an instance.
type ResourceLimits struct {
	// CPUCores is the number of CPU cores (e.g., 0.5 for half a core).
	CPUCores float64

	// MemoryMB is the memory limit in megabytes.
	MemoryMB int64

	// DiskMB is the ephemeral disk limit in megabytes.
	DiskMB int64

	// MaxRuntime is the maximum execution time for a single call.
	MaxRuntime time.Duration
}

// Instance represents a running execution environment.
type Instance interface {
	// ID returns the unique identifier for this instance.
	ID() string

	// Name returns the human-readable name (e.g., pod name).
	Name() string

	// Call invokes a method on the instance and returns the result.
	Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)

	// Stop gracefully stops the instance.
	Stop(ctx context.Context) error

	// Status returns the current status of the instance.
	Status() InstanceStatus

	// SetInterPodHandler sets the handler for inter-pod calls from this instance.
	SetInterPodHandler(handler InterPodHandler)
}

// InstanceStatus contains the current state of an instance.
type InstanceStatus struct {
	ID        string
	Name      string
	State     InstanceState
	PID       int           // Process ID (for subprocess runtime)
	Address   string        // Network address (for container runtimes)
	StartedAt time.Time
	CallCount int64
	LastError string
}

// InstanceState represents the lifecycle state of an instance.
type InstanceState string

const (
	StateStarting InstanceState = "starting"
	StateRunning  InstanceState = "running"
	StateStopping InstanceState = "stopping"
	StateStopped  InstanceState = "stopped"
	StateCrashed  InstanceState = "crashed"
)

// InterPodHandler handles inter-pod calls from an instance.
type InterPodHandler func(ctx context.Context, sourcePod, targetTool string, args json.RawMessage, contract *ContractExpectation) (json.RawMessage, error)

// ContractExpectation for verifying inter-pod call targets.
type ContractExpectation struct {
	ID         string `json:"id,omitempty"`
	Version    string `json:"version,omitempty"`
	SchemaHash string `json:"schema_hash,omitempty"`
}

// ParseResourceLimits converts manifest resource spec to ResourceLimits.
func ParseResourceLimits(spec *registry.ResourceSpec) ResourceLimits {
	if spec == nil {
		return ResourceLimits{
			CPUCores:   0.5,
			MemoryMB:   256,
			DiskMB:     1024,
			MaxRuntime: 5 * time.Minute,
		}
	}

	limits := ResourceLimits{
		CPUCores:   0.5,
		MemoryMB:   256,
		DiskMB:     1024,
		MaxRuntime: 5 * time.Minute,
	}

	// Parse CPU (e.g., "0.5", "1", "2")
	if spec.CPU != "" {
		var cpu float64
		if err := json.Unmarshal([]byte(spec.CPU), &cpu); err == nil {
			limits.CPUCores = cpu
		}
	}

	// Parse Memory (e.g., "256Mi", "1Gi")
	if spec.Memory != "" {
		limits.MemoryMB = parseMemory(spec.Memory)
	}

	// Parse Disk
	if spec.EphemeralStorage != "" {
		limits.DiskMB = parseMemory(spec.EphemeralStorage)
	}

	// Parse timeout
	if spec.Timeout.Duration > 0 {
		limits.MaxRuntime = spec.Timeout.Duration
	}

	return limits
}

// parseMemory converts memory strings like "256Mi", "1Gi" to megabytes.
func parseMemory(s string) int64 {
	if s == "" {
		return 256
	}

	// Simple parser for common formats
	var value int64
	var unit string

	// Extract numeric part
	for i, c := range s {
		if c < '0' || c > '9' {
			// Parse the numeric prefix
			if i > 0 {
				var v int64
				_ = json.Unmarshal([]byte(s[:i]), &v)
				value = v
			}
			unit = s[i:]
			break
		}
		if i == len(s)-1 {
			// All digits
			_ = json.Unmarshal([]byte(s), &value)
		}
	}

	// Convert to MB
	switch unit {
	case "Ki", "K":
		return value / 1024
	case "Mi", "M", "":
		return value
	case "Gi", "G":
		return value * 1024
	case "Ti", "T":
		return value * 1024 * 1024
	default:
		return value
	}
}
