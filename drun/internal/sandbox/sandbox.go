package sandbox

import (
	"context"
	"fmt"

	"github.com/docker/docker/client"
)

type MountMode string

const (
	MountModeReadOnly  MountMode = "ro"
	MountModeReadWrite MountMode = "rw"
)

const (
	SeccompProfileDefault    = "default"
	SeccompProfileStrict     = "strict"
	SeccompProfileUnconfined = "unconfined"
)

type SandboxManager struct {
	client *client.Client
}

func NewSandboxManager(dockerClient *client.Client) *SandboxManager {
	return &SandboxManager{client: dockerClient}
}

type SandboxConfig struct {
	Network        bool
	ReadOnlyRoot   bool
	MountMode      MountMode
	TimeoutSeconds int
	SeccompProfile string
	Limits         *ResourceLimits
}

func (c *SandboxConfig) Validate() error {
	if c.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout cannot be negative")
	}
	return nil
}

type ResourceLimits struct {
	MemoryBytes int64
	CPUCount    int64
	PidsLimit   int
}

type SandboxResult struct {
	ExitCode int
	Output   string
	Error    string
	TimedOut bool
}

type SeccompPolicy struct {
	AllowedSyscalls []string
	BlockedSyscalls []string
}

var StrictSeccompBlocklist = []string{
	"ptrace",
	"mount",
	"umount",
	"umount2",
	"pivot_root",
	"chroot",
	"setns",
	"unshare",
	"clone",
	"reboot",
	"kexec_load",
	"init_module",
	"delete_module",
}

func (m *SandboxManager) Run(ctx context.Context, image string, cmd []string, config *SandboxConfig) (*SandboxResult, error) {
	if config == nil {
		config = &SandboxConfig{
			Network:        false,
			ReadOnlyRoot:   true,
			MountMode:      MountModeReadOnly,
			TimeoutSeconds: 30,
			SeccompProfile: SeccompProfileDefault,
			Limits: &ResourceLimits{
				MemoryBytes: 256 * 1024 * 1024,
				CPUCount:    1,
				PidsLimit:   100,
			},
		}
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &SandboxResult{
		ExitCode: 0,
		Output:   "",
	}, nil
}
