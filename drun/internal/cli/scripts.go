package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

type ScriptsOptions struct {
	List  bool
	Setup bool
}

func NewScriptsCommand(appCtx *AppContext) *cobra.Command {
	opts := &ScriptsOptions{}

	cmd := &cobra.Command{
		Use:   "scripts [name] [args...]",
		Short: "List or run project scripts",
		Long: `List available scripts or run a named script from .drun.yaml.
Scripts can be simple commands or composite scripts that run multiple commands.`,
		Example: `  drun scripts           # List available scripts
  drun scripts test      # Run the 'test' script
  drun scripts dev       # Run the 'dev' script`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScripts(cmd.Context(), appCtx, opts, args)
		},
	}

	cmd.Flags().BoolVarP(&opts.List, "list", "l", false, "List available scripts")
	cmd.Flags().BoolVar(&opts.Setup, "setup", false, "Run setup before script")

	return cmd
}

func runScripts(ctx context.Context, appCtx *AppContext, opts *ScriptsOptions, args []string) error {
	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found")
	}

	if opts.List || len(args) == 0 {
		return listScripts(appCtx)
	}

	scriptName := args[0]
	scriptArgs := args[1:]

	return runScript(ctx, appCtx, opts, scriptName, scriptArgs)
}

func listScripts(appCtx *AppContext) error {
	if len(appCtx.Config.Scripts) == 0 {
		fmt.Println("No scripts defined in .drun.yaml")
		return nil
	}

	fmt.Println("Available scripts:")

	names := make([]string, 0, len(appCtx.Config.Scripts))
	for name := range appCtx.Config.Scripts {
		names = append(names, name)
	}
	sort.Strings(names)

	maxLen := 0
	for _, name := range names {
		if len(name) > maxLen {
			maxLen = len(name)
		}
	}

	for _, name := range names {
		script := appCtx.Config.Scripts[name]
		padding := strings.Repeat(" ", maxLen-len(name))
		if script.Description != "" {
			fmt.Printf("  %s%s  %s\n", name, padding, script.Description)
		} else if len(script.Composite) > 0 {
			fmt.Printf("  %s%s  [%s]\n", name, padding, strings.Join(script.Composite, ", "))
		} else {
			fmt.Printf("  %s%s  %s\n", name, padding, script.Command)
		}
	}

	return nil
}

func runScript(ctx context.Context, appCtx *AppContext, opts *ScriptsOptions, name string, args []string) error {
	script, ok := appCtx.Config.Scripts[name]
	if !ok {
		return fmt.Errorf("script not found: %s", name)
	}
	_ = script

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

	exitCode, err := executor.RunScript(ctx, containerID, name, args)
	if err != nil {
		return err
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}

	return nil
}
