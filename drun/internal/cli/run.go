package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"drun/internal/tty"
)

type RunOptions struct {
	Shell       bool
	Interactive bool
	Detach      bool
	WorkDir     string
	User        string
	Setup       bool
}

func NewRunCommand(appCtx *AppContext) *cobra.Command {
	opts := &RunOptions{}

	cmd := &cobra.Command{
		Use:   "run [flags] [command] [args...]",
		Short: "Run a command in a container",
		Long: `Run a command in a drun-managed container. If no command is provided,
an interactive shell is started.`,
		Example: `  drun run go test ./...
  drun run --shell
  drun run npm install
  drun run python --version`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRun(cmd.Context(), appCtx, opts, args)
		},
	}

	cmd.Flags().BoolVar(&opts.Shell, "shell", false, "Start an interactive shell")
	cmd.Flags().BoolVarP(&opts.Interactive, "interactive", "i", false, "Keep STDIN open")
	cmd.Flags().BoolVarP(&opts.Detach, "detach", "d", false, "Run in background")
	cmd.Flags().StringVarP(&opts.WorkDir, "workdir", "w", "", "Working directory")
	cmd.Flags().StringVarP(&opts.User, "user", "u", "", "User to run as")
	cmd.Flags().BoolVar(&opts.Setup, "setup", false, "Run setup before command")

	// Stop parsing flags after first non-flag argument, so command args like
	// "--version" or "-la" are passed to the command, not parsed by drun
	cmd.Flags().SetInterspersed(false)

	return cmd
}

func runRun(ctx context.Context, appCtx *AppContext, opts *RunOptions, args []string) error {
	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found in current directory")
	}

	// Merge global environment variables from -e flag into config
	if len(appCtx.Options.Env) > 0 {
		appCtx.Config.Env = append(appCtx.Config.Env, appCtx.Options.Env...)
	}

	// Merge dev ports when --dev flag is enabled
	if appCtx.Options.Dev && appCtx.Config.Dev != nil {
		if appCtx.Config.Dev.LSP != nil && appCtx.Config.Dev.LSP.Port > 0 {
			port := fmt.Sprintf("%d", appCtx.Config.Dev.LSP.Port)
			appCtx.Config.Ports = append(appCtx.Config.Ports, port)
		}
		if appCtx.Config.Dev.DAP != nil && appCtx.Config.Dev.DAP.Port > 0 {
			port := fmt.Sprintf("%d", appCtx.Config.Dev.DAP.Port)
			appCtx.Config.Ports = append(appCtx.Config.Ports, port)
		}
	}

	client, err := appCtx.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-sigCh
		cancel()
	}()

	executor := NewExecutor(client, appCtx.Config, appCtx.Options.Quiet)

	if err := executor.EnsureImage(ctx); err != nil {
		return err
	}

	containerID, err := executor.GetOrCreateContainer(ctx)
	if err != nil {
		return err
	}

	if opts.Setup {
		if err := executor.RunSetup(ctx, containerID); err != nil {
			return fmt.Errorf("setup failed: %w", err)
		}
	}

	var command []string
	if opts.Shell || len(args) == 0 {
		command = []string{"/bin/sh"}
		opts.Interactive = true
	} else {
		command = args
	}

	interactive := opts.Interactive || opts.Shell
	if !interactive {
		interactive = tty.IsTTY()
	}

	if !appCtx.Options.Quiet && !interactive {
		fmt.Printf("Running: %v\n", command)
	}

	exitCode, err := executor.Run(ctx, containerID, command, interactive)
	if err != nil {
		return err
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}

	return nil
}
