package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var (
	ErrConfigInvalid     = errors.New("invalid configuration")
	ErrMutuallyExclusive = errors.New("mutually exclusive options specified")
)

type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("config error in %s: %s", e.Field, e.Message)
}

type Config struct {
	Image            string            `yaml:"image" mapstructure:"image"`
	Workdir          string            `yaml:"workdir" mapstructure:"workdir"`
	Dockerfile       string            `yaml:"dockerfile" mapstructure:"dockerfile"`
	DockerfileInline string            `yaml:"dockerfile_inline" mapstructure:"dockerfile_inline"`
	Setup            *SetupConfig      `yaml:"setup" mapstructure:"setup"`
	Mounts           []MountConfig     `yaml:"mounts" mapstructure:"mounts"`
	Network          *NetworkConfig    `yaml:"network" mapstructure:"network"`
	Ports            []string          `yaml:"ports" mapstructure:"ports"`
	Env              []string          `yaml:"env" mapstructure:"env"`
	Resources        *ResourceConfig   `yaml:"resources" mapstructure:"resources"`
	Process          *ProcessConfig    `yaml:"process" mapstructure:"process"`
	Isolation        *IsolationConfig  `yaml:"isolation" mapstructure:"isolation"`
	IdleTimeout      string            `yaml:"idle_timeout" mapstructure:"idle_timeout"`
	Scripts          map[string]Script `yaml:"scripts" mapstructure:"scripts"`
	Sandbox          *SandboxConfig    `yaml:"sandbox" mapstructure:"sandbox"`
	WatchFiles       []string          `yaml:"watch_files" mapstructure:"watch_files"`
	Dev              *DevConfig        `yaml:"dev" mapstructure:"dev"`

	ProjectDir string `yaml:"-" mapstructure:"-"`
	PresetName string `yaml:"-" mapstructure:"-"`
}

type MountConfig struct {
	Source   string `yaml:"source" mapstructure:"source"`
	Target   string `yaml:"target" mapstructure:"target"`
	ReadOnly bool   `yaml:"readonly" mapstructure:"readonly"`
}

type ResourceConfig struct {
	Memory string `yaml:"memory" mapstructure:"memory"`
	CPUs   string `yaml:"cpus" mapstructure:"cpus"`
	PIDs   int    `yaml:"pids" mapstructure:"pids"`
}

type ProcessConfig struct {
	Init           bool   `yaml:"init" mapstructure:"init"`
	KillOnExit     bool   `yaml:"kill_on_exit" mapstructure:"kill_on_exit"`
	GracePeriod    string `yaml:"grace_period" mapstructure:"grace_period"`
	CleanupOrphans bool   `yaml:"cleanup_orphans" mapstructure:"cleanup_orphans"`
	OrphanTimeout  string `yaml:"orphan_timeout" mapstructure:"orphan_timeout"`
}

type IsolationConfig struct {
	Mode        string `yaml:"mode" mapstructure:"mode"`
	SharedCache bool   `yaml:"shared_cache" mapstructure:"shared_cache"`
}

type SandboxConfig struct {
	Enabled        bool   `yaml:"enabled" mapstructure:"enabled"`
	Network        bool   `yaml:"network" mapstructure:"network"`
	ReadOnlyRoot   bool   `yaml:"readonly_root" mapstructure:"readonly_root"`
	MountMode      string `yaml:"mount_mode" mapstructure:"mount_mode"`
	SeccompProfile string `yaml:"seccomp_profile" mapstructure:"seccomp_profile"`
	Timeout        string `yaml:"timeout" mapstructure:"timeout"`
}

// DevConfig defines development services like LSP and DAP servers.
type DevConfig struct {
	LSP *ServiceConfig `yaml:"lsp" mapstructure:"lsp"`
	DAP *ServiceConfig `yaml:"dap" mapstructure:"dap"`
}

// ServiceConfig defines a development service (LSP, DAP, etc.)
type ServiceConfig struct {
	Name    string `yaml:"name" mapstructure:"name"`
	Command string `yaml:"command" mapstructure:"command"`
	Port    int    `yaml:"port" mapstructure:"port"`
}

func NewConfig() *Config {
	cfg := &Config{}
	cfg.ApplyDefaults()
	return cfg
}

func (c *Config) ApplyDefaults() {
	if c.Workdir == "" {
		c.Workdir = "/workspace"
	}
	if c.Process == nil {
		c.Process = &ProcessConfig{
			Init:           true,
			KillOnExit:     true,
			GracePeriod:    "5s",
			CleanupOrphans: true,
			OrphanTimeout:  "30s",
		}
	}
	if c.Isolation == nil {
		c.Isolation = &IsolationConfig{
			Mode:        "branch",
			SharedCache: true,
		}
	}
	if c.IdleTimeout == "" {
		c.IdleTimeout = "30m"
	}
	if c.Network == nil {
		c.Network = &NetworkConfig{
			Mode: "bridge",
		}
	}
}

func (c *Config) Validate() error {
	exclusiveCount := 0
	if c.Dockerfile != "" {
		exclusiveCount++
	}
	if c.DockerfileInline != "" {
		exclusiveCount++
	}
	if c.Setup != nil && c.Setup.HasContent() {
		exclusiveCount++
	}
	if exclusiveCount > 1 {
		return &ConfigError{
			Field:   "dockerfile/dockerfile_inline/setup",
			Message: "options are mutually exclusive - only one can be specified",
		}
	}

	if c.Network != nil {
		if err := c.Network.Validate(); err != nil {
			return err
		}
	}

	if c.Setup != nil {
		if err := c.Setup.Validate(); err != nil {
			return err
		}
	}

	for name, script := range c.Scripts {
		if err := script.Validate(name); err != nil {
			return err
		}
	}

	for _, port := range c.Ports {
		if _, err := ParsePortBinding(port); err != nil {
			return &ConfigError{Field: "ports", Message: err.Error()}
		}
	}

	if c.IdleTimeout != "" {
		if _, err := time.ParseDuration(c.IdleTimeout); err != nil {
			return &ConfigError{Field: "idle_timeout", Message: "invalid duration format"}
		}
	}

	if c.Process != nil {
		if c.Process.GracePeriod != "" {
			if _, err := time.ParseDuration(c.Process.GracePeriod); err != nil {
				return &ConfigError{Field: "process.grace_period", Message: "invalid duration format"}
			}
		}
		if c.Process.OrphanTimeout != "" {
			if _, err := time.ParseDuration(c.Process.OrphanTimeout); err != nil {
				return &ConfigError{Field: "process.orphan_timeout", Message: "invalid duration format"}
			}
		}
	}

	return nil
}

func (c *Config) Hash() string {
	h := sha256.New()

	h.Write([]byte(c.Image))
	h.Write([]byte(c.Workdir))
	h.Write([]byte(c.Dockerfile))
	h.Write([]byte(c.DockerfileInline))

	if c.Setup != nil {
		h.Write([]byte(c.Setup.Hash()))
	}

	for _, m := range c.Mounts {
		h.Write([]byte(m.Source))
		h.Write([]byte(m.Target))
	}

	if c.Resources != nil {
		h.Write([]byte(c.Resources.Memory))
		h.Write([]byte(c.Resources.CPUs))
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}
