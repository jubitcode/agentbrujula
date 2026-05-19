package cli

import (
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
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	"drun/internal/config"
	cont "drun/internal/container"
	"drun/internal/runtime"
)

type Executor struct {
	client    *client.Client
	rtClient  *runtime.Client
	config    *config.Config
	lifecycle *cont.Lifecycle
	exec      *cont.ExecHandler
	quiet     bool
}

func NewExecutor(rtClient *runtime.Client, cfg *config.Config, quiet bool) *Executor {
	return &Executor{
		client:    rtClient.Client,
		rtClient:  rtClient,
		config:    cfg,
		lifecycle: cont.NewLifecycle(rtClient.Client),
		exec:      cont.NewExecHandler(rtClient.Client),
		quiet:     quiet,
	}
}

func (e *Executor) EnsureImage(ctx context.Context) error {
	imageName := e.config.Image
	if imageName == "" {
		return fmt.Errorf("no image specified in config")
	}

	images, err := e.client.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", imageName)),
	})
	if err != nil {
		return fmt.Errorf("failed to list images: %w", err)
	}

	if len(images) > 0 {
		if !e.quiet {
			fmt.Printf("Image %s found locally\n", imageName)
		}
		return nil
	}

	if !e.quiet {
		fmt.Printf("Pulling image %s...\n", imageName)
	}

	reader, err := e.client.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer reader.Close()

	if !e.quiet {
		io.Copy(os.Stdout, reader)
	} else {
		io.Copy(io.Discard, reader)
	}

	return nil
}

func (e *Executor) containerName() string {
	cwd, _ := os.Getwd()
	base := filepath.Base(cwd)
	name := strings.ReplaceAll(base, " ", "-")
	name = strings.ToLower(name)

	// Check if branch isolation is enabled and we're in a git repo
	if e.config.Isolation != nil && e.config.Isolation.Mode == "branch" {
		if branch := e.getCurrentBranch(cwd); branch != "" {
			// Sanitize branch name for container naming
			branch = strings.ReplaceAll(branch, "/", "-")
			branch = strings.ReplaceAll(branch, " ", "-")
			branch = strings.ToLower(branch)
			return fmt.Sprintf("drun-%s-%s", name, branch)
		}
	}

	return fmt.Sprintf("drun-%s", name)
}

// getCurrentBranch returns the current git branch, or empty string if not in a git repo.
func (e *Executor) getCurrentBranch(dir string) string {
	// Try to read from .git/HEAD
	gitHeadPath := filepath.Join(dir, ".git", "HEAD")
	data, err := os.ReadFile(gitHeadPath)
	if err != nil {
		return ""
	}

	head := strings.TrimSpace(string(data))
	// Format is typically: "ref: refs/heads/branch-name"
	if strings.HasPrefix(head, "ref: refs/heads/") {
		return strings.TrimPrefix(head, "ref: refs/heads/")
	}

	// Detached HEAD - return short commit hash
	if len(head) >= 8 {
		return head[:8]
	}
	return ""
}

func (e *Executor) GetOrCreateContainer(ctx context.Context) (string, error) {
	name := e.containerName()

	containers, err := e.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) > 0 {
		c := containers[0]
		if c.State != "running" {
			if !e.quiet {
				fmt.Printf("Starting existing container %s...\n", name)
			}
			if err := e.lifecycle.Start(ctx, c.ID); err != nil {
				return "", fmt.Errorf("failed to start container: %w", err)
			}
		}
		return c.ID, nil
	}

	if !e.quiet {
		fmt.Printf("Creating container %s...\n", name)
	}

	cwd, _ := os.Getwd()
	if e.config.Mounts != nil {
		for i := range e.config.Mounts {
			src := e.config.Mounts[i].Source
			if src == "." {
				e.config.Mounts[i].Source = cwd
			} else if strings.HasPrefix(src, "~") {
				home, _ := os.UserHomeDir()
				e.config.Mounts[i].Source = filepath.Join(home, src[1:])
			} else if !filepath.IsAbs(src) {
				e.config.Mounts[i].Source = filepath.Join(cwd, src)
			}
		}
	}

	containerID, err := e.createContainer(ctx, name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	if err := e.lifecycle.Start(ctx, containerID); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	return containerID, nil
}

func (e *Executor) createContainer(ctx context.Context, name string) (string, error) {
	cfg := e.config

	containerCfg := &container.Config{
		Image:      cfg.Image,
		WorkingDir: cfg.Workdir,
		Env:        cfg.Env,
		Cmd:        []string{"sleep", "infinity"},
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

	// Handle port bindings
	if len(cfg.Ports) > 0 {
		exposedPorts := make(nat.PortSet)
		portBindings := make(nat.PortMap)

		for _, p := range cfg.Ports {
			port, err := strconv.Atoi(p)
			if err != nil {
				// Handle complex port bindings like "8080:80"
				parts := strings.Split(p, ":")
				if len(parts) == 2 {
					hostPort := parts[0]
					containerPort := parts[1]
					natPort := nat.Port(containerPort + "/tcp")
					exposedPorts[natPort] = struct{}{}
					portBindings[natPort] = []nat.PortBinding{
						{HostPort: hostPort},
					}
				}
				continue
			}
			// Simple port binding (same port on host and container)
			natPort := nat.Port(fmt.Sprintf("%d/tcp", port))
			exposedPorts[natPort] = struct{}{}
			portBindings[natPort] = []nat.PortBinding{
				{HostPort: strconv.Itoa(port)},
			}
		}

		containerCfg.ExposedPorts = exposedPorts
		hostCfg.PortBindings = portBindings
	}

	resp, err := e.client.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	return resp.ID, nil
}

func (e *Executor) RunSetup(ctx context.Context, containerID string) error {
	if e.config.Setup == nil {
		return nil
	}

	if len(e.config.Setup.Packages) > 0 {
		if !e.quiet {
			fmt.Println("Installing packages...")
		}

		pmCmd := e.detectPackageManager(ctx, containerID)
		installCmd := fmt.Sprintf("%s %s", pmCmd, strings.Join(e.config.Setup.Packages, " "))

		opts := &cont.ExecOptions{
			WorkingDir: e.config.Workdir,
			Stdout:     os.Stdout,
			Stderr:     os.Stderr,
		}

		result, err := e.exec.Exec(ctx, containerID, []string{"/bin/sh", "-c", installCmd}, opts)
		if err != nil {
			return fmt.Errorf("failed to install packages: %w", err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("package installation failed with exit code %d", result.ExitCode)
		}
	}

	for _, cmd := range e.config.Setup.Run {
		if !e.quiet {
			fmt.Printf("Running: %s\n", cmd)
		}

		opts := &cont.ExecOptions{
			WorkingDir: e.config.Workdir,
			Stdout:     os.Stdout,
			Stderr:     os.Stderr,
		}

		result, err := e.exec.Exec(ctx, containerID, []string{"/bin/sh", "-c", cmd}, opts)
		if err != nil {
			return fmt.Errorf("setup command failed: %w", err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("setup command failed with exit code %d", result.ExitCode)
		}
	}

	if e.config.Setup.Deps != nil && e.config.Setup.Deps.Command != "" {
		if !e.quiet {
			fmt.Printf("Running deps: %s\n", e.config.Setup.Deps.Command)
		}

		opts := &cont.ExecOptions{
			WorkingDir: e.config.Workdir,
			Stdout:     os.Stdout,
			Stderr:     os.Stderr,
		}

		result, err := e.exec.Exec(ctx, containerID, []string{"/bin/sh", "-c", e.config.Setup.Deps.Command}, opts)
		if err != nil {
			return fmt.Errorf("deps command failed: %w", err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("deps command failed with exit code %d", result.ExitCode)
		}
	}

	return nil
}

func (e *Executor) detectPackageManager(ctx context.Context, containerID string) string {
	checks := []struct {
		cmd   string
		check string
	}{
		{"apt-get update && apt-get install -y", "which apt-get"},
		{"apk add --no-cache", "which apk"},
		{"dnf install -y", "which dnf"},
		{"yum install -y", "which yum"},
	}

	for _, check := range checks {
		result, err := e.exec.Exec(ctx, containerID, []string{"/bin/sh", "-c", check.check}, nil)
		if err == nil && result.ExitCode == 0 {
			return check.cmd
		}
	}

	return "apt-get update && apt-get install -y"
}

func (e *Executor) Run(ctx context.Context, containerID string, cmd []string, interactive bool) (int, error) {
	opts := &cont.ExecOptions{
		WorkingDir: e.config.Workdir,
		Env:        e.config.Env,
	}

	if interactive {
		opts.TTY = true
		opts.Stdin = os.Stdin
		opts.Stdout = os.Stdout
		opts.Stderr = os.Stderr

		err := e.exec.ExecInteractive(ctx, containerID, cmd, opts)
		if err != nil {
			return 1, err
		}
		return 0, nil
	}

	opts.Stdout = os.Stdout
	opts.Stderr = os.Stderr
	result, err := e.exec.Exec(ctx, containerID, cmd, opts)
	if err != nil {
		return 1, err
	}
	return result.ExitCode, nil
}

func (e *Executor) RunScript(ctx context.Context, containerID string, scriptName string, args []string) (int, error) {
	script, ok := e.config.Scripts[scriptName]
	if !ok {
		return 1, fmt.Errorf("script not found: %s", scriptName)
	}

	if len(script.Composite) > 0 {
		for _, subScript := range script.Composite {
			exitCode, err := e.RunScript(ctx, containerID, subScript, nil)
			if err != nil || exitCode != 0 {
				return exitCode, err
			}
		}
		return 0, nil
	}

	cmd := script.Command
	if len(args) > 0 {
		cmd = cmd + " " + strings.Join(args, " ")
	}

	if !e.quiet {
		fmt.Printf("Running script '%s': %s\n", scriptName, cmd)
	}

	var env []string
	env = append(env, script.Env...)
	env = append(env, e.config.Env...)

	opts := &cont.ExecOptions{
		WorkingDir: e.config.Workdir,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Env:        env,
	}

	result, err := e.exec.Exec(ctx, containerID, []string{"/bin/sh", "-c", cmd}, opts)
	if err != nil {
		return 1, err
	}

	return result.ExitCode, nil
}

func (e *Executor) StopContainer(ctx context.Context, containerID string) error {
	timeout := 10
	return e.lifecycle.Stop(ctx, containerID, &timeout)
}

func (e *Executor) RemoveContainer(ctx context.Context, containerID string) error {
	return e.lifecycle.Remove(ctx, containerID, true)
}
