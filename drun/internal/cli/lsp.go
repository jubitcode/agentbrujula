package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	cont "drun/internal/container"
)

// LSPServiceInfo represents the status of an LSP service.
type LSPServiceInfo struct {
	Server  string `json:"server"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Status  string `json:"status"`
	Command string `json:"command,omitempty"`
}

func NewLSPCommand(appCtx *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "Manage LSP services",
		Long:  `Start, stop, and manage Language Server Protocol (LSP) services for editor integration.`,
	}

	cmd.AddCommand(newLSPStartCommand(appCtx))
	cmd.AddCommand(newLSPStatusCommand(appCtx))
	cmd.AddCommand(newLSPStopCommand(appCtx))

	return cmd
}

func newLSPStartCommand(appCtx *AppContext) *cobra.Command {
	var detach bool

	cmd := &cobra.Command{
		Use:   "start [server]",
		Short: "Start an LSP service",
		Long: `Start an LSP server in the container. If no server is specified,
starts the default LSP server defined in the preset.`,
		Example: `  drun lsp start gopls
  drun lsp start pylsp
  drun lsp start --detach`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLSPStart(cmd.Context(), appCtx, args, detach)
		},
	}

	cmd.Flags().BoolVarP(&detach, "detach", "d", false, "Run in background")

	return cmd
}

func newLSPStatusCommand(appCtx *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show status of LSP services",
		Long:  `Show the status of running LSP services in the container.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLSPStatus(cmd.Context(), appCtx)
		},
	}
}

func newLSPStopCommand(appCtx *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "stop [server]",
		Short: "Stop an LSP service",
		Long:  `Stop a running LSP server in the container.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLSPStop(cmd.Context(), appCtx, args)
		},
	}
}

func runLSPStart(ctx context.Context, appCtx *AppContext, args []string, detach bool) error {
	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found in current directory")
	}

	if appCtx.Config.Dev == nil || appCtx.Config.Dev.LSP == nil {
		return fmt.Errorf("no LSP configuration found in preset or .drun.yaml")
	}

	lspConfig := appCtx.Config.Dev.LSP

	// Override with specific server if provided
	serverName := lspConfig.Name
	if len(args) > 0 {
		serverName = args[0]
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

	// Output service info
	info := LSPServiceInfo{
		Server:  serverName,
		Host:    "127.0.0.1",
		Port:    lspConfig.Port,
		Status:  "starting",
		Command: lspConfig.Command,
	}

	if detach {
		// Start LSP in background
		command := []string{"/bin/sh", "-c", lspConfig.Command + " &"}
		opts := &cont.ExecOptions{
			WorkingDir: appCtx.Config.Workdir,
			Env:        appCtx.Config.Env,
		}

		_, err := executor.exec.Exec(ctx, containerID, command, opts)
		if err != nil {
			return fmt.Errorf("failed to start LSP server: %w", err)
		}

		info.Status = "running"
		output, _ := json.MarshalIndent(info, "", "  ")
		fmt.Println(string(output))
		return nil
	}

	// Start LSP in foreground
	info.Status = "running"
	output, _ := json.MarshalIndent(info, "", "  ")
	fmt.Println(string(output))

	command := []string{"/bin/sh", "-c", lspConfig.Command}
	opts := &cont.ExecOptions{
		WorkingDir: appCtx.Config.Workdir,
		Env:        appCtx.Config.Env,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	}

	result, err := executor.exec.Exec(ctx, containerID, command, opts)
	if err != nil {
		return err
	}

	if result.ExitCode != 0 {
		return fmt.Errorf("LSP server exited with code %d", result.ExitCode)
	}

	return nil
}

func runLSPStatus(ctx context.Context, appCtx *AppContext) error {
	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found in current directory")
	}

	services := []LSPServiceInfo{}

	if appCtx.Config.Dev != nil {
		if appCtx.Config.Dev.LSP != nil {
			services = append(services, LSPServiceInfo{
				Server:  appCtx.Config.Dev.LSP.Name,
				Host:    "127.0.0.1",
				Port:    appCtx.Config.Dev.LSP.Port,
				Status:  "configured",
				Command: appCtx.Config.Dev.LSP.Command,
			})
		}
		if appCtx.Config.Dev.DAP != nil {
			services = append(services, LSPServiceInfo{
				Server:  appCtx.Config.Dev.DAP.Name,
				Host:    "127.0.0.1",
				Port:    appCtx.Config.Dev.DAP.Port,
				Status:  "configured",
				Command: appCtx.Config.Dev.DAP.Command,
			})
		}
	}

	if len(services) == 0 {
		fmt.Println("[]")
		return nil
	}

	output, _ := json.MarshalIndent(services, "", "  ")
	fmt.Println(string(output))
	return nil
}

func runLSPStop(ctx context.Context, appCtx *AppContext, args []string) error {
	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found in current directory")
	}

	if len(args) == 0 && (appCtx.Config.Dev == nil || appCtx.Config.Dev.LSP == nil) {
		return fmt.Errorf("no server specified and no default LSP configuration found")
	}

	serverName := ""
	if len(args) > 0 {
		serverName = args[0]
	} else if appCtx.Config.Dev != nil && appCtx.Config.Dev.LSP != nil {
		serverName = appCtx.Config.Dev.LSP.Name
	}

	client, err := appCtx.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	executor := NewExecutor(client, appCtx.Config, appCtx.Options.Quiet)

	containerID, err := executor.GetOrCreateContainer(ctx)
	if err != nil {
		return err
	}

	// Kill the LSP process
	command := []string{"/bin/sh", "-c", fmt.Sprintf("pkill -f '%s' || true", serverName)}
	opts := &cont.ExecOptions{
		WorkingDir: appCtx.Config.Workdir,
	}

	_, err = executor.exec.Exec(ctx, containerID, command, opts)
	if err != nil {
		return fmt.Errorf("failed to stop LSP server: %w", err)
	}

	info := LSPServiceInfo{
		Server: serverName,
		Status: "stopped",
	}
	output, _ := json.MarshalIndent(info, "", "  ")
	fmt.Println(string(output))

	return nil
}
