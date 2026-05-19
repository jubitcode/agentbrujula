package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/spf13/cobra"

	"drun/internal/runtime"
)

type CleanupOptions struct {
	Restart bool
	Force   bool
}

func NewCleanupCommand(appCtx *AppContext) *cobra.Command {
	opts := &CleanupOptions{}

	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Clean up orphaned processes",
		Long: `Clean up orphaned processes in the container.
Use --restart to restart the container after cleanup.`,
		Example: `  drun cleanup
  drun cleanup --restart
  drun prune`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCleanup(cmd.Context(), appCtx, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Restart, "restart", false, "Restart container after cleanup")
	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "Force cleanup")

	cmd.AddCommand(newPruneCommand(appCtx))

	return cmd
}

func newPruneCommand(appCtx *AppContext) *cobra.Command {
	var images, networks, all bool

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove idle containers and resources",
		Long:  `Remove idle drun containers, optionally including images and networks.`,
		Example: `  drun cleanup prune
  drun cleanup prune --images
  drun cleanup prune --all`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrune(cmd.Context(), appCtx, images, networks, all)
		},
	}

	cmd.Flags().BoolVar(&images, "images", false, "Also remove images")
	cmd.Flags().BoolVar(&networks, "networks", false, "Also remove networks")
	cmd.Flags().BoolVar(&all, "all", false, "Remove everything")

	return cmd
}

func runCleanup(ctx context.Context, appCtx *AppContext, opts *CleanupOptions) error {
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

	if !appCtx.Options.Quiet {
		fmt.Printf("Cleaning up container %s...\n", name)
	}

	containers, err := client.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		fmt.Println("No container found for this project")
		return nil
	}

	if opts.Restart {
		if !appCtx.Options.Quiet {
			fmt.Println("Restarting container...")
		}
		if err := client.ContainerRestart(ctx, containers[0].ID, container.StopOptions{}); err != nil {
			return fmt.Errorf("failed to restart container: %w", err)
		}
		fmt.Println("Container restarted")
	} else {
		fmt.Println("Cleanup complete")
	}

	return nil
}

func runPrune(ctx context.Context, appCtx *AppContext, images, networks, all bool) error {
	client, err := appCtx.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	if !appCtx.Options.Quiet {
		fmt.Println("Pruning drun resources...")
	}

	containers, err := client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "drun.managed=true")),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	removed := 0
	if len(containers) == 0 {
		fmt.Println("No drun containers found")
	} else {
		for _, c := range containers {
			name := c.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}

			if c.State == "running" {
				if !appCtx.Options.Quiet {
					fmt.Printf("  Stopping %s...\n", name)
				}
				timeout := 5
				if err := client.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); err != nil {
					fmt.Printf("  Warning: failed to stop %s: %v\n", name, err)
					continue
				}
			}

			if !appCtx.Options.Quiet {
				fmt.Printf("  Removing %s...\n", name)
			}
			if err := client.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
				fmt.Printf("  Warning: failed to remove %s: %v\n", name, err)
				continue
			}
			removed++
		}
		fmt.Printf("Removed %d container(s)\n", removed)
	}

	if images || all {
		if err := pruneImages(ctx, client, appCtx.Options.Quiet); err != nil {
			fmt.Printf("Warning: image cleanup failed: %v\n", err)
		}
	}

	if networks || all {
		if err := pruneNetworks(ctx, client, appCtx.Options.Quiet); err != nil {
			fmt.Printf("Warning: network cleanup failed: %v\n", err)
		}
	}

	return nil
}

func pruneImages(ctx context.Context, client *runtime.Client, quiet bool) error {
	images, err := client.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", "drun-snapshot/*")),
	})
	if err != nil {
		return fmt.Errorf("failed to list images: %w", err)
	}

	if len(images) == 0 {
		if !quiet {
			fmt.Println("No drun images found")
		}
		return nil
	}

	removed := 0
	for _, img := range images {
		for _, tag := range img.RepoTags {
			if !quiet {
				fmt.Printf("  Removing image %s...\n", tag)
			}
			_, err := client.ImageRemove(ctx, tag, image.RemoveOptions{Force: true})
			if err != nil {
				fmt.Printf("  Warning: failed to remove %s: %v\n", tag, err)
				continue
			}
			removed++
		}
	}

	fmt.Printf("Removed %d image(s)\n", removed)
	return nil
}

func pruneNetworks(ctx context.Context, client *runtime.Client, quiet bool) error {
	networks, err := client.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	removed := 0
	for _, net := range networks {
		if !strings.HasPrefix(net.Name, "drun-") {
			if net.Labels["drun.managed"] != "true" {
				continue
			}
		}

		if !quiet {
			fmt.Printf("  Removing network %s...\n", net.Name)
		}

		if err := client.NetworkRemove(ctx, net.ID); err != nil {
			fmt.Printf("  Warning: failed to remove %s: %v\n", net.Name, err)
			continue
		}
		removed++
	}

	if removed == 0 {
		if !quiet {
			fmt.Println("No drun networks found")
		}
	} else {
		fmt.Printf("Removed %d network(s)\n", removed)
	}

	return nil
}

func NewPruneCommand(appCtx *AppContext) *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove all drun containers",
		Long:  `Stop and remove all drun-managed containers.`,
		Example: `  drun prune
  drun prune --all`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrune(cmd.Context(), appCtx, false, false, all)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Remove everything including images")

	return cmd
}
