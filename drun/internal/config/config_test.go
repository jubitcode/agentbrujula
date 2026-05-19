package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigDefaults(t *testing.T) {
	cfg := NewConfig()

	assert.Equal(t, "/workspace", cfg.Workdir)
	assert.NotNil(t, cfg.Process)
	assert.True(t, cfg.Process.Init)
	assert.True(t, cfg.Process.KillOnExit)
	assert.Equal(t, "5s", cfg.Process.GracePeriod)
	assert.True(t, cfg.Process.CleanupOrphans)
	assert.Equal(t, "30s", cfg.Process.OrphanTimeout)
	assert.NotNil(t, cfg.Isolation)
	assert.Equal(t, "branch", cfg.Isolation.Mode)
	assert.True(t, cfg.Isolation.SharedCache)
	assert.Equal(t, "30m", cfg.IdleTimeout)
}

func TestConfigValidationMinimal(t *testing.T) {
	cfg := &Config{
		Image: "golang:1.22",
	}
	cfg.ApplyDefaults()

	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestConfigValidationFull(t *testing.T) {
	cfg := &Config{
		Image:   "golang:1.22",
		Workdir: "/app",
		Setup: &SetupConfig{
			Packages: []string{"git", "make"},
			Run:      []string{"go install something"},
		},
		Network: &NetworkConfig{
			Mode: "bridge",
			Join: []string{"mynet"},
		},
		Ports: []string{"8080:8080", "9090:9090"},
		Env:   []string{"DEBUG=true"},
	}
	cfg.ApplyDefaults()

	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestConfigValidationMutuallyExclusiveDockerfileAndSetup(t *testing.T) {
	cfg := &Config{
		Image:      "golang:1.22",
		Dockerfile: "./Dockerfile",
		Setup: &SetupConfig{
			Packages: []string{"git"},
		},
	}

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestConfigValidationMutuallyExclusiveDockerfileInlineAndSetup(t *testing.T) {
	cfg := &Config{
		Image:            "golang:1.22",
		DockerfileInline: "FROM golang:1.22",
		Setup: &SetupConfig{
			Packages: []string{"git"},
		},
	}

	err := cfg.Validate()
	assert.Error(t, err)
}

func TestConfigValidationMutuallyExclusiveDockerfiles(t *testing.T) {
	cfg := &Config{
		Image:            "golang:1.22",
		Dockerfile:       "./Dockerfile",
		DockerfileInline: "FROM golang:1.22",
	}

	err := cfg.Validate()
	assert.Error(t, err)
}

func TestConfigValidationInvalidNetworkMode(t *testing.T) {
	cfg := &Config{
		Image: "golang:1.22",
		Network: &NetworkConfig{
			Mode: "invalid",
		},
	}

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "network.mode")
}

func TestConfigValidationInvalidPortFormat(t *testing.T) {
	cfg := &Config{
		Image: "golang:1.22",
		Ports: []string{"invalid-port"},
	}

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ports")
}

func TestConfigValidationInvalidDuration(t *testing.T) {
	cfg := &Config{
		Image:       "golang:1.22",
		IdleTimeout: "invalid-duration",
	}

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "idle_timeout")
}

func TestConfigHash(t *testing.T) {
	cfg := &Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
		Setup: &SetupConfig{
			Packages: []string{"git", "make"},
		},
	}

	hash1 := cfg.Hash()
	hash2 := cfg.Hash()

	assert.Equal(t, hash1, hash2, "same config should produce same hash")
	assert.Len(t, hash1, 16, "hash should be 16 characters")
}

func TestConfigHashChanges(t *testing.T) {
	cfg1 := &Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
	}

	cfg2 := &Config{
		Image:   "golang:1.23",
		Workdir: "/workspace",
	}

	assert.NotEqual(t, cfg1.Hash(), cfg2.Hash(), "different image should produce different hash")

	cfg3 := &Config{
		Image:   "golang:1.22",
		Workdir: "/app",
	}

	assert.NotEqual(t, cfg1.Hash(), cfg3.Hash(), "different workdir should produce different hash")
}

func TestConfigHashIgnoresNonBuildFields(t *testing.T) {
	cfg1 := &Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
		Env:     []string{"DEBUG=true"},
		Ports:   []string{"8080:8080"},
	}

	cfg2 := &Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
		Env:     []string{"DEBUG=false"},
		Ports:   []string{"9090:9090"},
	}

	assert.Equal(t, cfg1.Hash(), cfg2.Hash(), "env and ports should not affect build hash")
}

func TestConfigApplyDefaults(t *testing.T) {
	cfg := &Config{
		Image: "golang:1.22",
	}

	cfg.ApplyDefaults()

	assert.Equal(t, "/workspace", cfg.Workdir)
	assert.NotNil(t, cfg.Process)
	assert.NotNil(t, cfg.Isolation)
	assert.NotNil(t, cfg.Network)
	assert.Equal(t, "bridge", cfg.Network.Mode)
}

func TestConfigApplyDefaultsPreservesExisting(t *testing.T) {
	cfg := &Config{
		Image:   "golang:1.22",
		Workdir: "/custom",
		Process: &ProcessConfig{
			Init:        false,
			GracePeriod: "10s",
		},
	}

	cfg.ApplyDefaults()

	assert.Equal(t, "/custom", cfg.Workdir)
	assert.False(t, cfg.Process.Init)
	assert.Equal(t, "10s", cfg.Process.GracePeriod)
}

func TestConfigError(t *testing.T) {
	err := &ConfigError{
		Field:   "network.mode",
		Message: "invalid mode",
	}

	assert.Contains(t, err.Error(), "network.mode")
	assert.Contains(t, err.Error(), "invalid mode")
}

func TestMountConfig(t *testing.T) {
	mount := MountConfig{
		Source: "/Users/dev/project",
		Target: "/workspace",
	}

	assert.Equal(t, "/Users/dev/project", mount.Source)
	assert.Equal(t, "/workspace", mount.Target)
}

func TestResourceConfig(t *testing.T) {
	cfg := &Config{
		Image: "golang:1.22",
		Resources: &ResourceConfig{
			Memory: "4g",
			CPUs:   "4",
			PIDs:   100,
		},
	}
	cfg.ApplyDefaults()

	err := cfg.Validate()
	require.NoError(t, err)

	assert.Equal(t, "4g", cfg.Resources.Memory)
	assert.Equal(t, "4", cfg.Resources.CPUs)
	assert.Equal(t, 100, cfg.Resources.PIDs)
}

func TestSandboxConfig(t *testing.T) {
	cfg := &Config{
		Image: "golang:1.22",
		Sandbox: &SandboxConfig{
			Enabled:        true,
			Network:        false,
			ReadOnlyRoot:   true,
			MountMode:      "ro",
			SeccompProfile: "strict",
			Timeout:        "30s",
		},
	}
	cfg.ApplyDefaults()

	err := cfg.Validate()
	require.NoError(t, err)

	assert.True(t, cfg.Sandbox.Enabled)
	assert.False(t, cfg.Sandbox.Network)
	assert.True(t, cfg.Sandbox.ReadOnlyRoot)
}
