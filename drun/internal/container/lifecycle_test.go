package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/internal/config"
)

func TestLifecycleNew(t *testing.T) {
	lc := NewLifecycle(nil)
	assert.NotNil(t, lc)
}

func TestLifecycleCreateContainerConfig(t *testing.T) {
	lc := NewLifecycle(nil)

	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
		Env:     []string{"DEBUG=true", "PATH=/usr/local/go/bin"},
	}

	containerCfg, hostCfg, err := lc.CreateContainerConfig(cfg, "test-container")
	require.NoError(t, err)

	assert.Equal(t, "golang:1.22", containerCfg.Image)
	assert.Equal(t, "/workspace", containerCfg.WorkingDir)
	assert.Contains(t, containerCfg.Env, "DEBUG=true")
	assert.NotNil(t, hostCfg)
}

func TestLifecycleCreateContainerConfigWithMounts(t *testing.T) {
	lc := NewLifecycle(nil)

	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
		Mounts: []config.MountConfig{
			{Source: "/home/user/project", Target: "/workspace"},
			{Source: "/home/user/.cache", Target: "/cache", ReadOnly: true},
		},
	}

	_, hostCfg, err := lc.CreateContainerConfig(cfg, "test-container")
	require.NoError(t, err)

	assert.Len(t, hostCfg.Binds, 2)
	assert.Contains(t, hostCfg.Binds[0], "/workspace")
	assert.Contains(t, hostCfg.Binds[1], ":ro")
}

func TestLifecycleCreateContainerConfigWithPorts(t *testing.T) {
	lc := NewLifecycle(nil)

	cfg := &config.Config{
		Image:   "node:20",
		Workdir: "/app",
		Ports:   []string{"3000:3000", "8080:80"},
	}

	containerCfg, hostCfg, err := lc.CreateContainerConfig(cfg, "test-container")
	require.NoError(t, err)

	assert.NotEmpty(t, containerCfg.ExposedPorts)
	assert.NotEmpty(t, hostCfg.PortBindings)
}

func TestLifecycleCreateContainerConfigWithLabels(t *testing.T) {
	lc := NewLifecycle(nil)

	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
	}

	containerCfg, _, err := lc.CreateContainerConfig(cfg, "test-container")
	require.NoError(t, err)

	assert.Equal(t, "true", containerCfg.Labels["drun.managed"])
}

func TestLifecycleCreateContainerConfigWithResources(t *testing.T) {
	lc := NewLifecycle(nil)

	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
		Resources: &config.ResourceConfig{
			Memory: "2g",
			CPUs:   "2",
			PIDs:   100,
		},
	}

	_, hostCfg, err := lc.CreateContainerConfig(cfg, "test-container")
	require.NoError(t, err)

	// Memory should be 2GB in bytes
	assert.Equal(t, int64(2*1024*1024*1024), hostCfg.Memory)
	// CPUs as nano CPUs (2 * 1e9)
	assert.Equal(t, int64(2*1e9), hostCfg.NanoCPUs)
	// PIDs limit
	require.NotNil(t, hostCfg.PidsLimit)
	assert.Equal(t, int64(100), *hostCfg.PidsLimit)
}

func TestLifecycleCreateContainerConfigWithInit(t *testing.T) {
	lc := NewLifecycle(nil)

	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
		Process: &config.ProcessConfig{
			Init: true,
		},
	}

	_, hostCfg, err := lc.CreateContainerConfig(cfg, "test-container")
	require.NoError(t, err)

	assert.True(t, *hostCfg.Init)
}

func TestLifecycleCreateContainerConfigWithHostNetwork(t *testing.T) {
	lc := NewLifecycle(nil)

	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
		Network: &config.NetworkConfig{
			Mode: "host",
		},
	}

	_, hostCfg, err := lc.CreateContainerConfig(cfg, "test-container")
	require.NoError(t, err)

	assert.Equal(t, "host", string(hostCfg.NetworkMode))
}

func TestContainerOptionsStruct(t *testing.T) {
	opts := &ContainerOptions{}

	assert.False(t, opts.Remove)
	assert.False(t, opts.Detach)
	assert.False(t, opts.TTY)
	assert.False(t, opts.Interactive)
}

func TestContainerOptionsAllFields(t *testing.T) {
	opts := &ContainerOptions{
		Remove:      true,
		Detach:      true,
		TTY:         true,
		Interactive: true,
		User:        "1000:1000",
		Hostname:    "test-host",
	}

	assert.True(t, opts.Remove)
	assert.True(t, opts.Detach)
	assert.True(t, opts.TTY)
	assert.True(t, opts.Interactive)
	assert.Equal(t, "1000:1000", opts.User)
	assert.Equal(t, "test-host", opts.Hostname)
}

func TestContainerInfoStruct(t *testing.T) {
	info := &ContainerInfo{
		ID:      "abc123",
		Name:    "test-container",
		Image:   "golang:1.22",
		Status:  "running",
		Running: true,
	}

	assert.Equal(t, "abc123", info.ID)
	assert.Equal(t, "test-container", info.Name)
	assert.Equal(t, "golang:1.22", info.Image)
	assert.Equal(t, "running", info.Status)
	assert.True(t, info.Running)
}
