package container

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	"drun/internal/config"
)

type Lifecycle struct {
	client *client.Client
}

func NewLifecycle(dockerClient *client.Client) *Lifecycle {
	return &Lifecycle{client: dockerClient}
}

type ContainerOptions struct {
	Remove      bool
	Detach      bool
	TTY         bool
	Interactive bool
	User        string
	Hostname    string
}

type ContainerInfo struct {
	ID      string
	Name    string
	Image   string
	Status  string
	Running bool
}

func (l *Lifecycle) CreateContainerConfig(cfg *config.Config, name string) (*container.Config, *container.HostConfig, error) {
	containerCfg := &container.Config{
		Image:      cfg.Image,
		WorkingDir: cfg.Workdir,
		Env:        cfg.Env,
		Labels: map[string]string{
			"drun.managed": "true",
		},
		Tty:       false,
		OpenStdin: false,
	}

	hostCfg := &container.HostConfig{
		AutoRemove: false,
	}

	if len(cfg.Mounts) > 0 {
		binds := make([]string, 0, len(cfg.Mounts))
		for _, m := range cfg.Mounts {
			bind := fmt.Sprintf("%s:%s", m.Source, m.Target)
			if m.ReadOnly {
				bind += ":ro"
			}
			binds = append(binds, bind)
		}
		hostCfg.Binds = binds
	}

	if len(cfg.Ports) > 0 {
		exposedPorts := make(nat.PortSet)
		portBindings := make(nat.PortMap)

		for _, p := range cfg.Ports {
			binding, err := parsePortBinding(p)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid port binding %q: %w", p, err)
			}

			port := nat.Port(fmt.Sprintf("%d/tcp", binding.ContainerPort))
			exposedPorts[port] = struct{}{}
			portBindings[port] = []nat.PortBinding{
				{
					HostIP:   binding.HostIP,
					HostPort: strconv.Itoa(binding.HostPort),
				},
			}
		}

		containerCfg.ExposedPorts = exposedPorts
		hostCfg.PortBindings = portBindings
	}

	if cfg.Resources != nil {
		if cfg.Resources.Memory != "" {
			mem, err := parseMemory(cfg.Resources.Memory)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid memory value %q: %w", cfg.Resources.Memory, err)
			}
			hostCfg.Memory = mem
		}

		if cfg.Resources.CPUs != "" {
			cpus, err := parseCPUs(cfg.Resources.CPUs)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid cpu value %q: %w", cfg.Resources.CPUs, err)
			}
			hostCfg.NanoCPUs = cpus
		}

		if cfg.Resources.PIDs > 0 {
			pids := int64(cfg.Resources.PIDs)
			hostCfg.PidsLimit = &pids
		}
	}

	if cfg.Process != nil && cfg.Process.Init {
		init := true
		hostCfg.Init = &init
	}

	if cfg.Network != nil && cfg.Network.Mode != "" {
		hostCfg.NetworkMode = container.NetworkMode(cfg.Network.Mode)
	}

	return containerCfg, hostCfg, nil
}

type portBindingParsed struct {
	HostIP        string
	HostPort      int
	ContainerPort int
	Protocol      string
}

func parsePortBinding(s string) (*portBindingParsed, error) {
	result := &portBindingParsed{Protocol: "tcp"}

	if idx := strings.LastIndex(s, "/"); idx != -1 {
		result.Protocol = s[idx+1:]
		s = s[:idx]
	}

	parts := strings.Split(s, ":")

	switch len(parts) {
	case 1:
		port, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid port: %s", parts[0])
		}
		result.ContainerPort = port
		result.HostPort = port
	case 2:
		hostPort, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid host port: %s", parts[0])
		}
		containerPort, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid container port: %s", parts[1])
		}
		result.HostPort = hostPort
		result.ContainerPort = containerPort
	case 3:
		result.HostIP = parts[0]
		hostPort, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid host port: %s", parts[1])
		}
		containerPort, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("invalid container port: %s", parts[2])
		}
		result.HostPort = hostPort
		result.ContainerPort = containerPort
	default:
		return nil, fmt.Errorf("invalid port format: %s", s)
	}

	return result, nil
}

func parseMemory(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	multiplier := int64(1)
	suffix := strings.ToLower(s[len(s)-1:])

	switch suffix {
	case "g":
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	case "m":
		multiplier = 1024 * 1024
		s = s[:len(s)-1]
	case "k":
		multiplier = 1024
		s = s[:len(s)-1]
	}

	value, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory value: %s", s)
	}

	return value * multiplier, nil
}

func parseCPUs(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	value, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid cpu value: %s", s)
	}

	return int64(value * 1e9), nil
}

func (l *Lifecycle) Create(ctx context.Context, cfg *config.Config, name string, opts *ContainerOptions) (string, error) {
	containerCfg, hostCfg, err := l.CreateContainerConfig(cfg, name)
	if err != nil {
		return "", err
	}

	if opts != nil {
		containerCfg.Tty = opts.TTY
		containerCfg.OpenStdin = opts.Interactive
		hostCfg.AutoRemove = opts.Remove

		if opts.User != "" {
			containerCfg.User = opts.User
		}
		if opts.Hostname != "" {
			containerCfg.Hostname = opts.Hostname
		}
	}

	resp, err := l.client.ContainerCreate(ctx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	return resp.ID, nil
}

func (l *Lifecycle) Start(ctx context.Context, containerID string) error {
	return l.client.ContainerStart(ctx, containerID, container.StartOptions{})
}

func (l *Lifecycle) Stop(ctx context.Context, containerID string, timeout *int) error {
	options := container.StopOptions{}
	if timeout != nil {
		options.Timeout = timeout
	}
	return l.client.ContainerStop(ctx, containerID, options)
}

func (l *Lifecycle) Remove(ctx context.Context, containerID string, force bool) error {
	return l.client.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         force,
		RemoveVolumes: false,
	})
}

func (l *Lifecycle) Inspect(ctx context.Context, containerID string) (*ContainerInfo, error) {
	resp, err := l.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, err
	}

	return &ContainerInfo{
		ID:      resp.ID,
		Name:    resp.Name,
		Image:   resp.Config.Image,
		Status:  resp.State.Status,
		Running: resp.State.Running,
	}, nil
}
