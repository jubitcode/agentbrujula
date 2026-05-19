package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/spf13/cobra"

	cont "drun/internal/container"
)

func NewDebugCommand(appCtx *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Enter debug mode",
		Long: `Enter debug mode to interactively troubleshoot the container.
Debug mode provides commands to inspect the environment, view diffs,
retry failed commands, and more.`,
		Example: `  drun debug
  drun debug --last-error`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDebug(cmd.Context(), appCtx)
		},
	}

	return cmd
}

func runDebug(ctx context.Context, appCtx *AppContext) error {
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

	containerID := containers[0].ID

	fmt.Println("Debug mode - Container:", containerName)
	fmt.Println("Commands: help, env, top, diff, shell, exec <cmd>, exit")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	exec := cont.NewExecHandler(client.Client)

	for {
		fmt.Print("debug> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		input = strings.TrimSpace(input)
		parts := strings.Fields(input)
		if len(parts) == 0 {
			continue
		}

		cmd := parts[0]
		args := parts[1:]

		switch cmd {
		case "help", "h", "?":
			fmt.Println("Available commands:")
			fmt.Println("  help, h, ?    - Show this help")
			fmt.Println("  env           - Show environment variables")
			fmt.Println("  top           - Show running processes")
			fmt.Println("  diff          - Show filesystem changes")
			fmt.Println("  shell         - Open interactive shell")
			fmt.Println("  exec <cmd>    - Execute command in container")
			fmt.Println("  exit, quit, q - Exit debug mode")

		case "env":
			opts := &cont.ExecOptions{
				WorkingDir: appCtx.Config.Workdir,
				Stdout:     os.Stdout,
				Stderr:     os.Stderr,
			}
			exec.Exec(ctx, containerID, []string{"env"}, opts)

		case "top":
			top, err := client.ContainerTop(ctx, containerID, nil)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			fmt.Println(strings.Join(top.Titles, "\t"))
			for _, proc := range top.Processes {
				fmt.Println(strings.Join(proc, "\t"))
			}

		case "diff":
			changes, err := client.ContainerDiff(ctx, containerID)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			if len(changes) == 0 {
				fmt.Println("No filesystem changes")
				continue
			}
			for _, change := range changes {
				kind := "?"
				switch change.Kind {
				case 0:
					kind = "M"
				case 1:
					kind = "A"
				case 2:
					kind = "D"
				}
				fmt.Printf("%s %s\n", kind, change.Path)
			}

		case "shell":
			fmt.Println("Starting shell (type 'exit' to return to debug mode)...")
			executor.Run(ctx, containerID, []string{"/bin/sh"}, true)

		case "exec":
			if len(args) == 0 {
				fmt.Println("Usage: exec <command>")
				continue
			}
			opts := &cont.ExecOptions{
				WorkingDir: appCtx.Config.Workdir,
				Stdout:     os.Stdout,
				Stderr:     os.Stderr,
			}
			exec.Exec(ctx, containerID, append([]string{"/bin/sh", "-c"}, strings.Join(args, " ")), opts)

		case "exit", "quit", "q":
			fmt.Println("Exiting debug mode")
			return nil

		default:
			fmt.Printf("Unknown command: %s (type 'help' for commands)\n", cmd)
		}
		fmt.Println()
	}

	return nil
}
