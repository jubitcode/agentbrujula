package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"drun/internal/discovery"
)

type PSOptions struct {
	All   bool
	Quiet bool
}

func NewPSCommand(appCtx *AppContext) *cobra.Command {
	opts := &PSOptions{}

	cmd := &cobra.Command{
		Use:   "ps",
		Short: "List containers and services",
		Long: `List all containers, networks, and services managed by drun
or visible in docker-compose projects.`,
		Example: `  drun ps
  drun ps --all
  drun ps --quiet`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPS(cmd.Context(), appCtx, opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.All, "all", "a", false, "Show all containers")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "Only show container IDs")

	return cmd
}

func runPS(ctx context.Context, appCtx *AppContext, opts *PSOptions) error {
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

	display := discovery.NewDisplay()
	if opts.Quiet {
		display.Options = display.Options.WithQuiet(true)
	}

	fmt.Print(display.FormatDiscoveryResult(result))
	return nil
}
