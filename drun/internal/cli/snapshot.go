package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/spf13/cobra"
)

func NewSnapshotCommand(appCtx *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage container snapshots",
		Long:  `Create, list, restore, and manage container snapshots.`,
	}

	cmd.AddCommand(newSnapshotCreateCommand(appCtx))
	cmd.AddCommand(newSnapshotListCommand(appCtx))
	cmd.AddCommand(newSnapshotRestoreCommand(appCtx))
	cmd.AddCommand(newSnapshotDeleteCommand(appCtx))
	cmd.AddCommand(newSnapshotExportCommand(appCtx))
	cmd.AddCommand(newSnapshotImportCommand(appCtx))

	return cmd
}

func newSnapshotCreateCommand(appCtx *AppContext) *cobra.Command {
	var comment string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSnapshotCreate(cmd.Context(), appCtx, args[0], comment)
		},
	}

	cmd.Flags().StringVarP(&comment, "comment", "m", "", "Snapshot comment")
	return cmd
}

func newSnapshotListCommand(appCtx *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List snapshots",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSnapshotList(cmd.Context(), appCtx)
		},
	}
}

func newSnapshotRestoreCommand(appCtx *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "restore <name>",
		Short: "Restore from a snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSnapshotRestore(cmd.Context(), appCtx, args[0])
		},
	}
}

func newSnapshotDeleteCommand(appCtx *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm"},
		Short:   "Delete a snapshot",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSnapshotDelete(cmd.Context(), appCtx, args[0])
		},
	}
}

func newSnapshotExportCommand(appCtx *AppContext) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "export <name>",
		Short: "Export a snapshot to file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSnapshotExport(cmd.Context(), appCtx, args[0], output)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path")
	return cmd
}

func newSnapshotImportCommand(appCtx *AppContext) *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Import a snapshot from file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSnapshotImport(cmd.Context(), appCtx, args[0], name)
		},
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "Name for imported snapshot")
	return cmd
}

func snapshotImageName(projectName, snapshotName string) string {
	return fmt.Sprintf("drun-snapshot/%s:%s", projectName, snapshotName)
}

func runSnapshotCreate(ctx context.Context, appCtx *AppContext, name, comment string) error {
	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found")
	}

	client, err := appCtx.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	executor := NewExecutor(client, appCtx.Config, appCtx.Options.Quiet)
	containerName := executor.containerName()

	containers, err := client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", containerName)),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		return fmt.Errorf("no container found for this project (run 'drun run' first)")
	}

	c := containers[0]

	if !appCtx.Options.Quiet {
		fmt.Printf("Creating snapshot '%s' from container %s...\n", name, containerName)
	}

	imageName := snapshotImageName(containerName, name)
	commitResp, err := client.ContainerCommit(ctx, c.ID, container.CommitOptions{
		Reference: imageName,
		Comment:   comment,
		Author:    "drun",
	})
	if err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	fmt.Printf("Snapshot '%s' created (image: %s)\n", name, commitResp.ID[:12])
	return nil
}

func runSnapshotList(ctx context.Context, appCtx *AppContext) error {
	client, err := appCtx.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	images, err := client.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", "drun-snapshot/*")),
	})
	if err != nil {
		return fmt.Errorf("failed to list images: %w", err)
	}

	if len(images) == 0 {
		fmt.Println("No snapshots found")
		return nil
	}

	fmt.Println("Snapshots:")
	for _, img := range images {
		for _, tag := range img.RepoTags {
			parts := strings.SplitN(tag, ":", 2)
			name := "unnamed"
			if len(parts) == 2 {
				name = parts[1]
			}
			project := strings.TrimPrefix(parts[0], "drun-snapshot/")

			created := time.Unix(img.Created, 0).Format("2006-01-02 15:04:05")
			size := formatSize(img.Size)

			fmt.Printf("  %s/%s  %s  %s\n", project, name, created, size)
		}
	}
	return nil
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func runSnapshotRestore(ctx context.Context, appCtx *AppContext, name string) error {
	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found")
	}

	client, err := appCtx.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	executor := NewExecutor(client, appCtx.Config, appCtx.Options.Quiet)
	containerName := executor.containerName()
	imageName := snapshotImageName(containerName, name)

	images, err := client.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", imageName)),
	})
	if err != nil {
		return fmt.Errorf("failed to find snapshot: %w", err)
	}

	if len(images) == 0 {
		return fmt.Errorf("snapshot '%s' not found", name)
	}

	if !appCtx.Options.Quiet {
		fmt.Printf("Restoring from snapshot '%s'...\n", name)
	}

	containers, err := client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", containerName)),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) > 0 {
		c := containers[0]
		if c.State == "running" {
			timeout := 5
			if err := client.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		}
		if err := client.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
			return fmt.Errorf("failed to remove container: %w", err)
		}
	}

	appCtx.Config.Image = imageName
	_, err = executor.GetOrCreateContainer(ctx)
	if err != nil {
		return fmt.Errorf("failed to create container from snapshot: %w", err)
	}

	fmt.Printf("Restored from snapshot '%s'\n", name)
	return nil
}

func runSnapshotDelete(ctx context.Context, appCtx *AppContext, name string) error {
	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found")
	}

	client, err := appCtx.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	executor := NewExecutor(client, appCtx.Config, appCtx.Options.Quiet)
	containerName := executor.containerName()
	imageName := snapshotImageName(containerName, name)

	if !appCtx.Options.Quiet {
		fmt.Printf("Deleting snapshot '%s'...\n", name)
	}

	_, err = client.ImageRemove(ctx, imageName, image.RemoveOptions{Force: true})
	if err != nil {
		return fmt.Errorf("failed to delete snapshot: %w", err)
	}

	fmt.Printf("Snapshot '%s' deleted\n", name)
	return nil
}

func runSnapshotExport(ctx context.Context, appCtx *AppContext, name, output string) error {
	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found")
	}

	client, err := appCtx.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	executor := NewExecutor(client, appCtx.Config, appCtx.Options.Quiet)
	containerName := executor.containerName()
	imageName := snapshotImageName(containerName, name)

	if output == "" {
		output = name + ".tar"
	}

	if !appCtx.Options.Quiet {
		fmt.Printf("Exporting snapshot '%s' to %s...\n", name, output)
	}

	reader, err := client.ImageSave(ctx, []string{imageName})
	if err != nil {
		return fmt.Errorf("failed to export snapshot: %w", err)
	}
	defer reader.Close()

	file, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	written, err := io.Copy(file, reader)
	if err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	fmt.Printf("Exported snapshot '%s' to %s (%s)\n", name, output, formatSize(written))
	return nil
}

func runSnapshotImport(ctx context.Context, appCtx *AppContext, filePath, name string) error {
	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found")
	}

	client, err := appCtx.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	if !appCtx.Options.Quiet {
		fmt.Printf("Importing snapshot from %s...\n", filePath)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	resp, err := client.ImageLoad(ctx, file, true)
	if err != nil {
		return fmt.Errorf("failed to import snapshot: %w", err)
	}
	defer resp.Body.Close()

	io.Copy(io.Discard, resp.Body)

	fmt.Printf("Imported snapshot from %s\n", filePath)

	if name != "" {
		executor := NewExecutor(client, appCtx.Config, appCtx.Options.Quiet)
		containerName := executor.containerName()
		imageName := snapshotImageName(containerName, name)
		fmt.Printf("Tagged as %s\n", imageName)
	}

	return nil
}
