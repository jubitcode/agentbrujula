package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	"drun/internal/runtime"
)

// DockerBackend implements RuntimeBackend using Docker containers.
type DockerBackend struct {
	client *client.Client
	host   string
}

// NewDockerBackend creates a new Docker backend.
// If host is empty, it tries DOCKER_HOST env, then common socket paths (Orbstack, Colima, etc.)
func NewDockerBackend(ctx context.Context, host string) (*DockerBackend, error) {
	// If explicit host provided, use it
	if host != "" {
		return newDockerBackendWithHost(ctx, host)
	}

	// Try DOCKER_HOST from environment first
	backend, err := newDockerBackendFromEnv(ctx)
	if err == nil && backend.Available() {
		return backend, nil
	}

	// Try common socket paths for Docker Desktop alternatives
	socketPaths := getDockerSocketPaths()
	for _, socketPath := range socketPaths {
		backend, err := newDockerBackendWithHost(ctx, "unix://"+socketPath)
		if err == nil && backend.Available() {
			return backend, nil
		}
	}

	// Return the env-based client even if not available (for better error messages)
	if backend != nil {
		return backend, nil
	}

	return newDockerBackendFromEnv(ctx)
}

func newDockerBackendFromEnv(ctx context.Context) (*DockerBackend, error) {
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}

	c, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &DockerBackend{
		client: c,
	}, nil
}

func newDockerBackendWithHost(ctx context.Context, host string) (*DockerBackend, error) {
	opts := []client.Opt{
		client.WithHost(host),
		client.WithAPIVersionNegotiation(),
	}

	c, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &DockerBackend{
		client: c,
		host:   host,
	}, nil
}

// getDockerSocketPaths returns common Docker socket paths for various Docker alternatives.
// These are tried in order when DOCKER_HOST is not set and the default socket is unavailable.
func getDockerSocketPaths() []string {
	home, _ := os.UserHomeDir()
	paths := []string{
		"/var/run/docker.sock",                            // Default Linux/Docker Desktop
		filepath.Join(home, ".orbstack/run/docker.sock"),  // Orbstack (macOS)
		filepath.Join(home, ".colima/default/docker.sock"), // Colima (macOS/Linux)
		filepath.Join(home, ".colima/docker.sock"),        // Colima alternative
		filepath.Join(home, ".docker/run/docker.sock"),    // Docker Desktop (macOS)
		"/run/docker.sock",                                // Alternative Linux path
	}

	// Filter to only paths that exist
	var existing []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			existing = append(existing, p)
		}
	}
	return existing
}

// NewDockerBackendFromClient creates a Docker backend from an existing client.
func NewDockerBackendFromClient(c *client.Client) *DockerBackend {
	return &DockerBackend{
		client: c,
	}
}

func (d *DockerBackend) Name() string {
	return "docker"
}

func (d *DockerBackend) Type() runtime.BackendType {
	return runtime.BackendDocker
}

func (d *DockerBackend) Available() bool {
	if d.client == nil {
		return false
	}
	ctx := context.Background()
	_, err := d.client.Ping(ctx)
	return err == nil
}

func (d *DockerBackend) Create(ctx context.Context, spec *runtime.InstanceSpec) (string, error) {
	containerCfg := &container.Config{
		Image:      spec.Image,
		WorkingDir: spec.Workdir,
		Cmd:        spec.Command,
		Labels:     spec.Labels,
		Tty:        false,
		OpenStdin:  false,
	}

	if len(spec.Env) > 0 {
		env := make([]string, 0, len(spec.Env))
		for k, v := range spec.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		containerCfg.Env = env
	}

	if containerCfg.Labels == nil {
		containerCfg.Labels = make(map[string]string)
	}
	containerCfg.Labels["drun.managed"] = "true"

	hostCfg := &container.HostConfig{
		AutoRemove: false,
	}

	// Handle mounts
	if len(spec.Mounts) > 0 {
		binds := make([]string, 0, len(spec.Mounts))
		for _, m := range spec.Mounts {
			bind := fmt.Sprintf("%s:%s", m.Source, m.Target)
			if m.ReadOnly {
				bind += ":ro"
			}
			binds = append(binds, bind)
		}
		hostCfg.Binds = binds
	}

	// Handle ports
	if len(spec.Ports) > 0 {
		exposedPorts := make(nat.PortSet)
		portBindings := make(nat.PortMap)

		for _, p := range spec.Ports {
			protocol := p.Protocol
			if protocol == "" {
				protocol = "tcp"
			}
			port := nat.Port(fmt.Sprintf("%s/%s", p.ContainerPort, protocol))
			exposedPorts[port] = struct{}{}
			portBindings[port] = []nat.PortBinding{
				{
					HostIP:   p.HostIP,
					HostPort: p.HostPort,
				},
			}
		}

		containerCfg.ExposedPorts = exposedPorts
		hostCfg.PortBindings = portBindings
	}

	// Handle resource limits
	if spec.Resources.MemoryMB > 0 {
		hostCfg.Memory = spec.Resources.MemoryMB * 1024 * 1024
	}
	if spec.Resources.CPUCores > 0 {
		hostCfg.NanoCPUs = int64(spec.Resources.CPUCores * 1e9)
	}
	if spec.Resources.MaxPIDs > 0 {
		pids := int64(spec.Resources.MaxPIDs)
		hostCfg.PidsLimit = &pids
	}

	// Handle network
	if spec.Network.Mode != "" {
		hostCfg.NetworkMode = container.NetworkMode(spec.Network.Mode)
	}

	resp, err := d.client.ContainerCreate(ctx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, spec.Name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	return resp.ID, nil
}

func (d *DockerBackend) Start(ctx context.Context, id string) error {
	return d.client.ContainerStart(ctx, id, container.StartOptions{})
}

func (d *DockerBackend) Stop(ctx context.Context, id string, timeout int) error {
	options := container.StopOptions{}
	if timeout > 0 {
		options.Timeout = &timeout
	}
	return d.client.ContainerStop(ctx, id, options)
}

func (d *DockerBackend) Remove(ctx context.Context, id string) error {
	return d.client.ContainerRemove(ctx, id, container.RemoveOptions{
		Force:         true,
		RemoveVolumes: false,
	})
}

func (d *DockerBackend) Inspect(ctx context.Context, id string) (*runtime.InstanceInfo, error) {
	resp, err := d.client.ContainerInspect(ctx, id)
	if err != nil {
		return nil, err
	}

	status := runtime.StatusUnknown
	switch resp.State.Status {
	case "created":
		status = runtime.StatusCreated
	case "running":
		status = runtime.StatusRunning
	case "paused":
		status = runtime.StatusPaused
	case "exited", "stopped":
		status = runtime.StatusStopped
	case "dead":
		status = runtime.StatusDead
	}

	ports := make(map[string]string)
	for containerPort, bindings := range resp.NetworkSettings.Ports {
		if len(bindings) > 0 {
			ports[string(containerPort)] = bindings[0].HostPort
		}
	}

	ipAddress := ""
	if resp.NetworkSettings != nil && resp.NetworkSettings.IPAddress != "" {
		ipAddress = resp.NetworkSettings.IPAddress
	}

	return &runtime.InstanceInfo{
		ID:        resp.ID,
		Name:      strings.TrimPrefix(resp.Name, "/"),
		Status:    status,
		IPAddress: ipAddress,
		Ports:     ports,
		StartedAt: resp.State.StartedAt,
		Labels:    resp.Config.Labels,
	}, nil
}

func (d *DockerBackend) Exec(ctx context.Context, id string, cmd []string, opts *runtime.ExecOptions) (*runtime.ExecResult, error) {
	if opts == nil {
		opts = &runtime.ExecOptions{}
	}

	execConfig := container.ExecOptions{
		Cmd:          cmd,
		Tty:          opts.TTY,
		Privileged:   opts.Privileged,
		User:         opts.User,
		WorkingDir:   opts.WorkingDir,
		Env:          opts.Env,
		AttachStdin:  opts.Stdin != nil,
		AttachStdout: true,
		AttachStderr: true,
	}

	execResp, err := d.client.ContainerExecCreate(ctx, id, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	attachResp, err := d.client.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{
		Tty: opts.TTY,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()

	var stdout, stderr bytes.Buffer
	outWriter := io.Writer(&stdout)
	errWriter := io.Writer(&stderr)

	if opts.Stdout != nil {
		outWriter = io.MultiWriter(&stdout, opts.Stdout)
	}
	if opts.Stderr != nil {
		errWriter = io.MultiWriter(&stderr, opts.Stderr)
	}

	if opts.TTY {
		_, err = io.Copy(outWriter, attachResp.Reader)
	} else {
		_, err = stdCopy(outWriter, errWriter, attachResp.Reader)
	}
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}

	inspect, err := d.client.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect exec: %w", err)
	}

	return &runtime.ExecResult{
		ExitCode: inspect.ExitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

func (d *DockerBackend) ExecInteractive(ctx context.Context, id string, cmd []string, opts *runtime.ExecOptions) error {
	if opts == nil {
		opts = &runtime.ExecOptions{}
	}

	execConfig := container.ExecOptions{
		Cmd:          cmd,
		Tty:          opts.TTY,
		Privileged:   opts.Privileged,
		User:         opts.User,
		WorkingDir:   opts.WorkingDir,
		Env:          opts.Env,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	}

	execResp, err := d.client.ContainerExecCreate(ctx, id, execConfig)
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}

	attachResp, err := d.client.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{
		Tty: opts.TTY,
	})
	if err != nil {
		return fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()

	if opts.Stdin != nil {
		go func() {
			io.Copy(attachResp.Conn, opts.Stdin)
			attachResp.CloseWrite()
		}()
	}

	if opts.Stdout != nil {
		if opts.TTY {
			// TTY mode: raw stream, no multiplexing
			_, err = io.Copy(opts.Stdout, attachResp.Reader)
		} else {
			// Non-TTY: Docker multiplexes stdout/stderr with 8-byte headers
			stderr := opts.Stderr
			if stderr == nil {
				stderr = io.Discard
			}
			_, err = stdcopy.StdCopy(opts.Stdout, stderr, attachResp.Reader)
		}
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read exec output: %w", err)
		}
	}

	return nil
}

func (d *DockerBackend) PullImage(ctx context.Context, name string) error {
	reader, err := d.client.ImagePull(ctx, name, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer reader.Close()

	// Consume the reader to complete the pull
	io.Copy(io.Discard, reader)
	return nil
}

func (d *DockerBackend) HasImage(ctx context.Context, name string) (bool, error) {
	images, err := d.client.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", name)),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list images: %w", err)
	}
	return len(images) > 0, nil
}

func (d *DockerBackend) Close() error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}

// ListContainers returns containers matching the filter.
func (d *DockerBackend) ListContainers(ctx context.Context, filterArgs map[string]string) ([]runtime.InstanceInfo, error) {
	f := filters.NewArgs()
	for k, v := range filterArgs {
		f.Add(k, v)
	}

	containers, err := d.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: f,
	})
	if err != nil {
		return nil, err
	}

	result := make([]runtime.InstanceInfo, len(containers))
	for i, c := range containers {
		status := runtime.StatusUnknown
		switch c.State {
		case "created":
			status = runtime.StatusCreated
		case "running":
			status = runtime.StatusRunning
		case "paused":
			status = runtime.StatusPaused
		case "exited", "stopped":
			status = runtime.StatusStopped
		case "dead":
			status = runtime.StatusDead
		}

		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		result[i] = runtime.InstanceInfo{
			ID:     c.ID,
			Name:   name,
			Status: status,
			Labels: c.Labels,
		}
	}
	return result, nil
}

// GetClient returns the underlying Docker client.
func (d *DockerBackend) GetClient() *client.Client {
	return d.client
}

// stdCopy copies data from src to stdout, handling Docker's multiplexed stream format.
func stdCopy(stdout, stderr io.Writer, src io.Reader) (int64, error) {
	var written int64
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			nw, ew := stdout.Write(buf[:n])
			written += int64(nw)
			if ew != nil {
				return written, ew
			}
		}
		if err != nil {
			return written, err
		}
	}
}

// Ensure DockerBackend implements RuntimeBackend
var _ runtime.RuntimeBackend = (*DockerBackend)(nil)

// parsePortString parses a port string like "8080", "8080:80", or "127.0.0.1:8080:80"
func parsePortString(s string) (hostIP, hostPort, containerPort, protocol string) {
	protocol = "tcp"
	if idx := strings.LastIndex(s, "/"); idx != -1 {
		protocol = s[idx+1:]
		s = s[:idx]
	}

	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		containerPort = parts[0]
		hostPort = parts[0]
	case 2:
		hostPort = parts[0]
		containerPort = parts[1]
	case 3:
		hostIP = parts[0]
		hostPort = parts[1]
		containerPort = parts[2]
	}
	return
}

// parseMemory converts a memory string like "1g", "512m" to bytes.
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
