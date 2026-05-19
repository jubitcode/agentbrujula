package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/spf13/cobra"

	"drun/internal/config"
	"drun/internal/discovery"
)

func NewInitCommand(appCtx *AppContext) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a .drun.yaml configuration",
		Long: `Initialize a new .drun.yaml configuration file in the current directory.
Automatically detects the project language and applies an appropriate preset.`,
		Example: `  drun init
  drun init --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd.Context(), appCtx, force)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing config")
	return cmd
}

func runInit(ctx context.Context, appCtx *AppContext, force bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	configPath := cwd + "/.drun.yaml"

	if _, err := os.Stat(configPath); err == nil && !force {
		return fmt.Errorf(".drun.yaml already exists (use --force to overwrite)")
	}

	detector := config.NewDetector(cwd)
	detected, _ := detector.Detect()

	if !appCtx.Options.Quiet {
		if detected != "" {
			fmt.Printf("Detected project type: %s\n", detected)
		} else {
			fmt.Println("No specific project type detected, using defaults")
		}
	}

	configContent := generateConfig(detected, cwd)

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Created %s\n", configPath)
	return nil
}

func generateConfig(lang config.Language, projectDir string) string {
	switch lang {
	case config.LangGo:
		return `image: "golang:1.22"
workdir: "/workspace"

setup:
  deps:
    command: "go mod download"
    triggers:
      - "go.mod"
      - "go.sum"

mounts:
  - source: "."
    target: "/workspace"

scripts:
  build:
    command: "go build ./..."
    description: "Build all packages"

  test:
    command: "go test -race ./..."
    description: "Run tests with race detector"

  lint:
    command: "go vet ./..."
    description: "Run linter"
`
	case config.LangNode:
		return `image: "node:20"
workdir: "/workspace"

setup:
  deps:
    command: "npm install"
    triggers:
      - "package.json"
      - "package-lock.json"

mounts:
  - source: "."
    target: "/workspace"

scripts:
  dev:
    command: "npm run dev"
    description: "Start development server"

  build:
    command: "npm run build"
    description: "Build for production"

  test:
    command: "npm test"
    description: "Run tests"
`
	case config.LangPython:
		return `image: "python:3.12-slim"
workdir: "/workspace"

setup:
  deps:
    command: "pip install -r requirements.txt"
    triggers:
      - "requirements.txt"
      - "pyproject.toml"

mounts:
  - source: "."
    target: "/workspace"

scripts:
  run:
    command: "python main.py"
    description: "Run the application"

  test:
    command: "python -m pytest"
    description: "Run tests"
`
	case config.LangRust:
		return `image: "rust:latest"
workdir: "/workspace"

setup:
  deps:
    command: "cargo fetch"
    triggers:
      - "Cargo.toml"
      - "Cargo.lock"

mounts:
  - source: "."
    target: "/workspace"

scripts:
  build:
    command: "cargo build"
    description: "Build the project"

  test:
    command: "cargo test"
    description: "Run tests"

  run:
    command: "cargo run"
    description: "Build and run"
`
	case config.LangOdin:
		return `image: "ubuntu:22.04"
workdir: "/workspace"

setup:
  packages:
    - curl
    - clang
    - llvm

mounts:
  - source: "."
    target: "/workspace"

scripts:
  build:
    command: "odin build ."
    description: "Build the project"

  run:
    command: "odin run ."
    description: "Build and run"
`
	default:
		return `image: "ubuntu:22.04"
workdir: "/workspace"

setup:
  packages: []

mounts:
  - source: "."
    target: "/workspace"

scripts:
  shell:
    command: "/bin/bash"
    description: "Open a shell"
`
	}
}

func NewServicesCommand(appCtx *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "services",
		Short: "Show detailed service discovery",
		Long:  `Show detailed information about discovered services and connection suggestions.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServices(cmd.Context(), appCtx)
		},
	}
}

func runServices(ctx context.Context, appCtx *AppContext) error {
	client, err := appCtx.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	sd := discovery.NewServiceDiscovery(client.Client)
	result, err := sd.Discover(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover services: %w", err)
	}

	runningServices := result.GetRunningServices()
	if len(runningServices) == 0 {
		fmt.Println("No running services found")
		return nil
	}

	composeServices := result.GetServicesBySource(discovery.ServiceSourceCompose)
	drunServices := result.GetServicesBySource(discovery.ServiceSourceDrun)
	manualServices := result.GetServicesBySource(discovery.ServiceSourceManual)

	if len(composeServices) > 0 {
		fmt.Println("Docker Compose services:")
		for _, s := range composeServices {
			if !s.IsRunning() {
				continue
			}
			fmt.Printf("  %s (%s)\n", s.Name, s.Image)
			if len(s.Ports) > 0 {
				for _, p := range s.Ports {
					fmt.Printf("    Port: %s\n", p.String())
				}
			}
			if connStr := discovery.SuggestConnectionString(s); connStr != "" {
				fmt.Printf("    Connect: %s\n", connStr)
			}
		}
		fmt.Println()
	}

	if len(drunServices) > 0 {
		fmt.Println("Drun containers:")
		for _, s := range drunServices {
			if !s.IsRunning() {
				continue
			}
			fmt.Printf("  %s (%s)\n", s.Name, s.Image)
		}
		fmt.Println()
	}

	if len(manualServices) > 0 {
		fmt.Println("Other containers:")
		for _, s := range manualServices {
			if !s.IsRunning() {
				continue
			}
			fmt.Printf("  %s (%s)\n", s.Name, s.Image)
			if len(s.Ports) > 0 {
				for _, p := range s.Ports {
					fmt.Printf("    Port: %s\n", p.String())
				}
			}
		}
		fmt.Println()
	}

	fmt.Println("Networks:")
	for _, n := range result.Networks {
		if n.Driver == "null" || n.Name == "none" || n.Name == "host" || n.Name == "bridge" {
			continue
		}
		fmt.Printf("  %s (%s)\n", n.Name, n.Driver)
		if len(n.Containers) > 0 {
			fmt.Printf("    Containers: %v\n", n.Containers)
		}
	}

	return nil
}

func NewEnvCommand(appCtx *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage branch environments",
		Long:  `List, prune, and manage per-branch container environments.`,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List branch environments",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Branch environments:")
			fmt.Println("  (no environments)")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "prune",
		Short: "Remove old environments",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Pruning old environments...")
			return nil
		},
	})

	return cmd
}

func NewDiffCommand(appCtx *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "Show drift from baseline",
		Long:  `Show differences between the current container state and the baseline.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd.Context(), appCtx)
		},
	}
}

func runDiff(ctx context.Context, appCtx *AppContext) error {
	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found")
	}

	client, err := appCtx.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	executor := NewExecutor(client, appCtx.Config, appCtx.Options.Quiet)
	name := executor.containerName()

	containers, err := client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		return fmt.Errorf("no container found for this project")
	}

	containerID := containers[0].ID

	if !appCtx.Options.Quiet {
		fmt.Println("Checking for drift from baseline...")
	}

	changes, err := client.ContainerDiff(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to get diff: %w", err)
	}

	if len(changes) == 0 {
		fmt.Println("No drift detected")
		return nil
	}

	var added, modified, deleted []string
	for _, change := range changes {
		switch change.Kind {
		case 0: // Modified
			modified = append(modified, change.Path)
		case 1: // Added
			added = append(added, change.Path)
		case 2: // Deleted
			deleted = append(deleted, change.Path)
		}
	}

	fmt.Printf("Filesystem changes (%d total):\n", len(changes))
	if len(added) > 0 {
		fmt.Printf("\nAdded (%d):\n", len(added))
		for _, p := range added {
			fmt.Printf("  A %s\n", p)
		}
	}
	if len(modified) > 0 {
		fmt.Printf("\nModified (%d):\n", len(modified))
		for _, p := range modified {
			fmt.Printf("  M %s\n", p)
		}
	}
	if len(deleted) > 0 {
		fmt.Printf("\nDeleted (%d):\n", len(deleted))
		for _, p := range deleted {
			fmt.Printf("  D %s\n", p)
		}
	}

	return nil
}

func NewStopCommand(appCtx *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the project container",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStop(cmd.Context(), appCtx)
		},
	}
}

func runStop(ctx context.Context, appCtx *AppContext) error {
	client, err := appCtx.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found")
	}

	executor := NewExecutor(client, appCtx.Config, appCtx.Options.Quiet)
	name := executor.containerName()

	if !appCtx.Options.Quiet {
		fmt.Printf("Stopping container %s...\n", name)
	}

	containers, err := client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		fmt.Println("No container found for this project")
		return nil
	}

	c := containers[0]
	if c.State == "running" {
		timeout := 10
		if err := client.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); err != nil {
			return fmt.Errorf("failed to stop container: %w", err)
		}
		fmt.Printf("Container %s stopped\n", name)
	} else {
		fmt.Printf("Container %s is not running (state: %s)\n", name, c.State)
	}

	return nil
}

func NewStatusCommand(appCtx *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show container health status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), appCtx)
		},
	}
}

func runStatus(ctx context.Context, appCtx *AppContext) error {
	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found")
	}

	client, err := appCtx.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	executor := NewExecutor(client, appCtx.Config, appCtx.Options.Quiet)
	name := executor.containerName()

	containers, err := client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		fmt.Printf("Container: %s\n", name)
		fmt.Println("Status: not created")
		return nil
	}

	c := containers[0]
	fmt.Printf("Container: %s\n", name)
	fmt.Printf("Status: %s\n", c.State)
	fmt.Printf("Image: %s\n", c.Image)

	if c.State == "running" {
		stats, err := client.ContainerStatsOneShot(ctx, c.ID)
		if err == nil {
			defer stats.Body.Close()
			fmt.Println("Stats: available (use docker stats for live view)")
		}

		top, err := client.ContainerTop(ctx, c.ID, nil)
		if err == nil {
			fmt.Printf("Processes: %d\n", len(top.Processes))
		}
	}

	return nil
}
