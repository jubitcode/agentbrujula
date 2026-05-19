package runtime

import (
	"context"
	"io"
)

// BackendType identifies the type of runtime backend
type BackendType string

const (
	BackendDocker      BackendType = "docker"
	BackendFirecracker BackendType = "firecracker"
	BackendVZ          BackendType = "vz"      // Apple Virtualization.framework
	BackendSandbox     BackendType = "sandbox" // Lightweight process sandbox
)

// RuntimeBackend abstracts container/VM execution across different isolation technologies.
// Implementations include Docker, Firecracker (Linux), Virtualization.framework (macOS),
// and a lightweight sandbox mode.
type RuntimeBackend interface {
	// Metadata
	Name() string
	Type() BackendType
	Available() bool

	// Lifecycle
	Create(ctx context.Context, spec *InstanceSpec) (string, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string, timeout int) error
	Remove(ctx context.Context, id string) error
	Inspect(ctx context.Context, id string) (*InstanceInfo, error)

	// Execution
	Exec(ctx context.Context, id string, cmd []string, opts *ExecOptions) (*ExecResult, error)
	ExecInteractive(ctx context.Context, id string, cmd []string, opts *ExecOptions) error

	// Images (optional - not all backends use images)
	PullImage(ctx context.Context, name string) error
	HasImage(ctx context.Context, name string) (bool, error)

	// Cleanup
	Close() error
}

// InstanceSpec defines the configuration for creating a new instance.
type InstanceSpec struct {
	Name      string            // Unique instance name
	Image     string            // Docker/OCI image or VM image path
	Command   []string          // Entrypoint command
	Workdir   string            // Working directory inside the instance
	Env       map[string]string // Environment variables
	Mounts    []Mount           // Volume/directory mounts
	Ports     []PortBinding     // Port mappings
	Resources ResourceLimits    // CPU, memory, disk limits
	Network   NetworkConfig     // Network configuration
	Labels    map[string]string // Metadata labels
}

// Mount defines a directory or volume mount.
type Mount struct {
	Source   string // Host path or volume name
	Target   string // Path inside the instance
	ReadOnly bool   // Mount as read-only
	Type     string // "bind", "volume", "tmpfs"
}

// PortBinding defines a port mapping.
type PortBinding struct {
	HostIP        string // Host IP to bind (empty for all interfaces)
	HostPort      string // Host port or range
	ContainerPort string // Container/instance port
	Protocol      string // "tcp" or "udp"
}

// ResourceLimits defines resource constraints for an instance.
type ResourceLimits struct {
	CPUCores float64 // Number of CPU cores (e.g., 0.5, 1, 2)
	MemoryMB int64   // Memory limit in megabytes
	DiskMB   int64   // Disk space limit in megabytes
	MaxPIDs  int     // Maximum number of processes
}

// NetworkConfig defines network settings for an instance.
type NetworkConfig struct {
	Mode       string   // "bridge", "host", "none", "container:<name>"
	Networks   []string // Named networks to attach
	DNSServers []string // Custom DNS servers
	Hostname   string   // Instance hostname
}

// ExecOptions configures command execution within an instance.
type ExecOptions struct {
	TTY        bool      // Allocate a pseudo-TTY
	Detach     bool      // Run in background
	Privileged bool      // Run with elevated privileges
	User       string    // User to run as
	WorkingDir string    // Working directory for the command
	Env        []string  // Additional environment variables
	Stdin      io.Reader // Standard input
	Stdout     io.Writer // Standard output
	Stderr     io.Writer // Standard error
}

// ExecResult contains the result of command execution.
type ExecResult struct {
	ExitCode int    // Process exit code
	Stdout   string // Captured stdout (if not streamed)
	Stderr   string // Captured stderr (if not streamed)
}

// InstanceInfo provides information about a running instance.
type InstanceInfo struct {
	ID        string            // Instance ID
	Name      string            // Instance name
	Status    InstanceStatus    // Current status
	IPAddress string            // Primary IP address
	Ports     map[string]string // Port mappings (container:host)
	StartedAt string            // Start timestamp
	Labels    map[string]string // Instance labels
}

// InstanceStatus represents the current state of an instance.
type InstanceStatus string

const (
	StatusCreated  InstanceStatus = "created"
	StatusRunning  InstanceStatus = "running"
	StatusPaused   InstanceStatus = "paused"
	StatusStopped  InstanceStatus = "stopped"
	StatusDead     InstanceStatus = "dead"
	StatusUnknown  InstanceStatus = "unknown"
)
