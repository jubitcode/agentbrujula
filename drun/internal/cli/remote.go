package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"drun/internal/remote"
)

func NewRemoteCommand(appCtx *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Manage remote Docker hosts",
		Long:  `Add, list, and manage remote Docker hosts for running containers remotely.`,
	}

	cmd.AddCommand(newRemoteAddCommand(appCtx))
	cmd.AddCommand(newRemoteListCommand(appCtx))
	cmd.AddCommand(newRemoteRemoveCommand(appCtx))
	cmd.AddCommand(newRemoteTestCommand(appCtx))
	cmd.AddCommand(newRemoteSyncCommand(appCtx))
	cmd.AddCommand(newRemoteRunCommand(appCtx))

	return cmd
}

func getRemoteStore() *remote.RemoteStore {
	home, _ := os.UserHomeDir()
	return remote.NewRemoteStore(filepath.Join(home, ".drun", "remotes"))
}

func newRemoteAddCommand(appCtx *AppContext) *cobra.Command {
	var user string
	var port int
	var keyPath string
	var dockerHost string

	cmd := &cobra.Command{
		Use:   "add <name> <host>",
		Short: "Add a remote host",
		Args:  cobra.ExactArgs(2),
		Example: `  drun remote add myserver example.com
  drun remote add dev dev.example.com --user ubuntu
  drun remote add prod 192.168.1.100 --port 2222 --key ~/.ssh/prod_key`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			host := args[1]

			if user == "" {
				user = os.Getenv("USER")
			}

			config := &remote.RemoteConfig{
				Name:       name,
				Host:       host,
				User:       user,
				Port:       port,
				KeyPath:    keyPath,
				DockerHost: dockerHost,
			}

			store := getRemoteStore()
			if err := store.Add(config); err != nil {
				return err
			}

			manager := remote.NewRemoteManager()
			if err := manager.TestConnection(config); err != nil {
				fmt.Printf("Warning: connection test failed: %v\n", err)
				fmt.Println("Remote saved but may not be reachable.")
			} else {
				fmt.Printf("Remote '%s' added and connection verified.\n", name)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&user, "user", "u", "", "SSH user (default: current user)")
	cmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port")
	cmd.Flags().StringVarP(&keyPath, "key", "k", "", "SSH private key path")
	cmd.Flags().StringVar(&dockerHost, "docker-host", "", "Custom DOCKER_HOST on remote")

	return cmd
}

func newRemoteListCommand(appCtx *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List remote hosts",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := getRemoteStore()
			configs, err := store.List()
			if err != nil {
				return err
			}

			if len(configs) == 0 {
				fmt.Println("No remotes configured")
				fmt.Println("Use 'drun remote add <name> <host>' to add one")
				return nil
			}

			fmt.Println("Configured remotes:")
			for _, c := range configs {
				fmt.Printf("  %s  %s@%s", c.Name, c.User, c.Host)
				if c.Port != 0 && c.Port != 22 {
					fmt.Printf(":%d", c.Port)
				}
				fmt.Println()
			}

			return nil
		},
	}
}

func newRemoteRemoveCommand(appCtx *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Remove a remote host",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := getRemoteStore()
			if err := store.Remove(args[0]); err != nil {
				return err
			}
			fmt.Printf("Remote '%s' removed\n", args[0])
			return nil
		},
	}
}

func newRemoteTestCommand(appCtx *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "test <name>",
		Short: "Test connection to a remote host",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := getRemoteStore()
			config, err := store.Get(args[0])
			if err != nil {
				return err
			}

			manager := remote.NewRemoteManager()

			fmt.Printf("Testing connection to %s...\n", config.SSHTarget())
			if err := manager.TestConnection(config); err != nil {
				return fmt.Errorf("connection failed: %w", err)
			}
			fmt.Println("SSH connection: OK")

			fmt.Println("Testing Docker...")
			output, err := manager.RunRemoteCommand(config, "docker version --format '{{.Server.Version}}'")
			if err != nil {
				return fmt.Errorf("Docker test failed: %w", err)
			}
			fmt.Printf("Docker version: %s\n", output)

			fmt.Println("All tests passed!")
			return nil
		},
	}
}

func newRemoteSyncCommand(appCtx *AppContext) *cobra.Command {
	var remoteName string
	var remotePath string
	var delete bool
	var dryRun bool
	var exclude []string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync current directory to remote host",
		Long: `Synchronize the current project directory to a remote host using rsync.
The files will be synced to the specified path on the remote.`,
		Example: `  drun remote sync --remote myserver --path /home/user/project
  drun remote sync -r myserver -p /tmp/project --delete
  drun remote sync -r myserver -p /opt/app --exclude node_modules --exclude .git`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if remoteName == "" {
				return fmt.Errorf("remote name is required (use --remote or -r)")
			}

			store := getRemoteStore()
			config, err := store.Get(remoteName)
			if err != nil {
				return err
			}

			if remotePath == "" {
				cwd, _ := os.Getwd()
				projectName := filepath.Base(cwd)
				remotePath = filepath.Join("/tmp/drun", projectName)
			}

			manager := remote.NewRemoteManager()
			_, err = manager.RunRemoteCommand(config, fmt.Sprintf("mkdir -p %s", remotePath))
			if err != nil {
				return fmt.Errorf("failed to create remote directory: %w", err)
			}

			cwd, _ := os.Getwd()
			fmt.Printf("Syncing %s to %s:%s\n", cwd, config.SSHTarget(), remotePath)

			syncer := remote.NewSyncer(config)
			opts := &remote.SyncOptions{
				Delete: delete,
				DryRun: dryRun,
				Exclude: append(exclude,
					".git", ".drun", "node_modules", "__pycache__", "*.pyc", ".DS_Store"),
			}

			if err := syncer.Sync(cwd, remotePath, opts); err != nil {
				return fmt.Errorf("sync failed: %w", err)
			}

			if !dryRun {
				fmt.Println("Sync complete!")
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&remoteName, "remote", "r", "", "Remote name (required)")
	cmd.Flags().StringVarP(&remotePath, "path", "P", "", "Remote path (default: /tmp/drun/<project>)")
	cmd.Flags().BoolVar(&delete, "delete", false, "Delete files on remote that don't exist locally")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be synced without syncing")
	cmd.Flags().StringArrayVarP(&exclude, "exclude", "x", nil, "Exclude pattern (repeatable)")

	return cmd
}

func newRemoteRunCommand(appCtx *AppContext) *cobra.Command {
	var remoteName string
	var remotePath string
	var sync bool

	cmd := &cobra.Command{
		Use:   "run [flags] [command...]",
		Short: "Run a command on a remote Docker host",
		Long: `Run a drun command on a remote Docker host.
Optionally sync files before running.`,
		Example: `  drun remote run -r myserver -p /tmp/project --sync go test ./...
  drun remote run -r myserver -p /opt/app docker ps`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if remoteName == "" {
				return fmt.Errorf("remote name is required (use --remote or -r)")
			}

			if len(args) == 0 {
				return fmt.Errorf("command is required")
			}

			store := getRemoteStore()
			config, err := store.Get(remoteName)
			if err != nil {
				return err
			}

			manager := remote.NewRemoteManager()

			if sync {
				if remotePath == "" {
					cwd, _ := os.Getwd()
					projectName := filepath.Base(cwd)
					remotePath = filepath.Join("/tmp/drun", projectName)
				}

				cwd, _ := os.Getwd()
				fmt.Printf("Syncing to %s:%s...\n", config.SSHTarget(), remotePath)

				syncer := remote.NewSyncer(config)
				opts := &remote.SyncOptions{
					Exclude: []string{".git", ".drun", "node_modules", "__pycache__", "*.pyc", ".DS_Store"},
				}

				if err := syncer.Sync(cwd, remotePath, opts); err != nil {
					return fmt.Errorf("sync failed: %w", err)
				}
			}

			remoteCmd := fmt.Sprintf("cd %s && %s", remotePath, joinArgs(args))

			fmt.Printf("Running on %s: %s\n", remoteName, joinArgs(args))
			output, err := manager.RunRemoteCommand(config, remoteCmd)
			if err != nil {
				return err
			}

			if output != "" {
				fmt.Println(output)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&remoteName, "remote", "r", "", "Remote name (required)")
	cmd.Flags().StringVarP(&remotePath, "path", "P", "", "Remote working directory")
	cmd.Flags().BoolVar(&sync, "sync", false, "Sync files before running")

	return cmd
}

func joinArgs(args []string) string {
	result := ""
	for i, arg := range args {
		if i > 0 {
			result += " "
		}
		if containsSpace(arg) {
			result += fmt.Sprintf("'%s'", arg)
		} else {
			result += arg
		}
	}
	return result
}

func containsSpace(s string) bool {
	for _, c := range s {
		if c == ' ' {
			return true
		}
	}
	return false
}
