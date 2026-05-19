// Package cli provides the command-line interface for mcp-meta.
package cli

import (
	"fmt"
	"os"
	"strings"
)

// Command represents a CLI subcommand.
type Command interface {
	Name() string
	Description() string
	Usage() string
	Run(args []string) error
}

// CLI is the main command-line interface.
type CLI struct {
	commands map[string]Command
}

// New creates a new CLI instance.
func New() *CLI {
	cli := &CLI{
		commands: make(map[string]Command),
	}

	// Register commands
	cli.Register(&InitCommand{})
	cli.Register(&BuildCommand{})
	cli.Register(&DevCommand{})
	cli.Register(&PublishCommand{})
	cli.Register(&AuthCommand{})
	cli.Register(&VersionCommand{})

	return cli
}

// Register adds a command to the CLI.
func (c *CLI) Register(cmd Command) {
	c.commands[cmd.Name()] = cmd
}

// Run executes the CLI with the given arguments.
// Returns true if a subcommand was handled, false if the default behavior should run.
func (c *CLI) Run(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}

	// Check if first arg is a subcommand
	cmdName := args[0]

	// Skip if it looks like a flag (backward compatibility)
	if strings.HasPrefix(cmdName, "-") {
		return false, nil
	}

	cmd, ok := c.commands[cmdName]
	if !ok {
		// Not a known subcommand - might be a path, let default handler check
		return false, nil
	}

	// Run the subcommand
	if err := cmd.Run(args[1:]); err != nil {
		return true, err
	}
	return true, nil
}

// PrintHelp prints the help message for all commands.
func (c *CLI) PrintHelp() {
	fmt.Println("mcp-meta - Daemonless MCP tool orchestrator and marketplace CLI")
	fmt.Println()
	fmt.Println("USAGE:")
	fmt.Println("    mcp-meta <command> [options]")
	fmt.Println("    mcp-meta [server options]     # Run as MCP server (default)")
	fmt.Println()
	fmt.Println("COMMANDS:")

	// Print command list with descriptions
	for _, name := range []string{"init", "build", "dev", "publish", "auth", "version"} {
		if cmd, ok := c.commands[name]; ok {
			fmt.Printf("    %-12s %s\n", name, cmd.Description())
		}
	}

	fmt.Println()
	fmt.Println("SERVER OPTIONS:")
	fmt.Println("    -m, --manifests <DIR>      Directory containing tool manifests")
	fmt.Println("    -t, --transport <TYPE>     Transport type: stdio, sse")
	fmt.Println("    -p, --port <PORT>          Port for SSE transport")
	fmt.Println("    --validate                 Validate manifests and exit")
	fmt.Println()
	fmt.Println("Run 'mcp-meta <command> --help' for more information on a command.")
}

// IsSubcommand returns true if the given argument is a subcommand.
func (c *CLI) IsSubcommand(arg string) bool {
	_, ok := c.commands[arg]
	return ok
}

// Helper function for creating directories
func ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// Helper function for writing files
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
