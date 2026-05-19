package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

type BuildOptions struct {
	DryRun  bool
	Rebuild bool
	NoCache bool
}

func NewBuildCommand(appCtx *AppContext) *cobra.Command {
	opts := &BuildOptions{}

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the container environment",
		Long: `Build or rebuild the container environment based on .drun.yaml.
This pulls the image, creates the container, and runs setup commands.`,
		Example: `  drun build
  drun build --rebuild
  drun build --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(cmd.Context(), appCtx, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be built")
	cmd.Flags().BoolVar(&opts.Rebuild, "rebuild", false, "Force rebuild")
	cmd.Flags().BoolVar(&opts.NoCache, "no-cache", false, "Disable Docker build cache")

	return cmd
}

func runBuild(ctx context.Context, appCtx *AppContext, opts *BuildOptions) error {
	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found")
	}

	if opts.DryRun {
		return runBuildDryRun(appCtx)
	}

	client, err := appCtx.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	executor := NewExecutor(client, appCtx.Config, appCtx.Options.Quiet)

	if opts.Rebuild {
		name := executor.containerName()
		if !appCtx.Options.Quiet {
			fmt.Printf("Removing existing container %s...\n", name)
		}
		containerID, _ := executor.GetOrCreateContainer(ctx)
		if containerID != "" {
			executor.StopContainer(ctx, containerID)
			executor.RemoveContainer(ctx, containerID)
		}
	}

	if !appCtx.Options.Quiet {
		fmt.Println("Checking image...")
	}
	if err := executor.EnsureImage(ctx); err != nil {
		return err
	}

	if !appCtx.Options.Quiet {
		fmt.Println("Setting up container...")
	}
	containerID, err := executor.GetOrCreateContainer(ctx)
	if err != nil {
		return err
	}

	if err := executor.RunSetup(ctx, containerID); err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}

	if !appCtx.Options.Quiet {
		fmt.Println("Build complete")
	}

	return nil
}

func runBuildDryRun(appCtx *AppContext) error {
	cfg := appCtx.Config

	fmt.Println("Build configuration (dry run):")
	fmt.Println()
	fmt.Printf("Image: %s\n", cfg.Image)
	fmt.Printf("Workdir: %s\n", cfg.Workdir)

	if len(cfg.Mounts) > 0 {
		fmt.Println("\nMounts:")
		for _, m := range cfg.Mounts {
			fmt.Printf("  %s -> %s\n", m.Source, m.Target)
		}
	}

	if len(cfg.Ports) > 0 {
		fmt.Println("\nPorts:")
		for _, p := range cfg.Ports {
			fmt.Printf("  %s\n", p)
		}
	}

	if len(cfg.Env) > 0 {
		fmt.Println("\nEnvironment:")
		for _, e := range cfg.Env {
			fmt.Printf("  %s\n", e)
		}
	}

	if cfg.Setup != nil {
		if len(cfg.Setup.Packages) > 0 {
			fmt.Println("\nPackages to install:")
			for _, p := range cfg.Setup.Packages {
				fmt.Printf("  %s\n", p)
			}
		}

		if len(cfg.Setup.Run) > 0 {
			fmt.Println("\nSetup commands:")
			for _, r := range cfg.Setup.Run {
				fmt.Printf("  %s\n", r)
			}
		}

		if cfg.Setup.Deps != nil && cfg.Setup.Deps.Command != "" {
			fmt.Printf("\nDeps command: %s\n", cfg.Setup.Deps.Command)
			if len(cfg.Setup.Deps.Triggers) > 0 {
				fmt.Println("Triggers:")
				for _, t := range cfg.Setup.Deps.Triggers {
					fmt.Printf("  %s\n", t)
				}
			}
		}
	}

	return nil
}
