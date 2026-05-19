package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"mcp-meta/internal/build"
	"mcp-meta/internal/mcp"
	"mcp-meta/internal/registry"
)

// BuildCommand builds a container image for the tool.
type BuildCommand struct{}

func (c *BuildCommand) Name() string        { return "build" }
func (c *BuildCommand) Description() string { return "Build container image for the tool" }

func (c *BuildCommand) Usage() string {
	return `mcp-meta build - Build container image

USAGE:
    mcp-meta build [options]

OPTIONS:
    --manifest <FILE>    Manifest file (default: ./mcp-meta.yaml)
    --tag <TAG>          Image tag (default: from manifest version)
    --push               Push image to registry after build
    -h, --help           Print help
`
}

func (c *BuildCommand) Run(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)

	var manifestPath string
	var tag string
	var push bool
	var outputDockerfile string
	var showHelp bool

	fs.StringVar(&manifestPath, "manifest", "./mcp-meta.yaml", "Manifest file")
	fs.StringVar(&tag, "tag", "", "Image tag")
	fs.BoolVar(&push, "push", false, "Push image to registry")
	fs.StringVar(&outputDockerfile, "output", "", "Write Dockerfile to file (default: print to stdout)")
	fs.BoolVar(&showHelp, "help", false, "Print help")
	fs.BoolVar(&showHelp, "h", false, "Print help")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if showHelp {
		fmt.Print(c.Usage())
		return nil
	}

	// Load manifest
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}

	manifest, err := registry.ParseManifest(data)
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Generate Dockerfile
	generator := build.NewDockerfileGenerator()
	dockerfile, err := generator.Generate(manifest)
	if err != nil {
		return fmt.Errorf("failed to generate Dockerfile: %w", err)
	}

	// Output Dockerfile
	if outputDockerfile != "" {
		if err := os.WriteFile(outputDockerfile, []byte(dockerfile), 0644); err != nil {
			return fmt.Errorf("failed to write Dockerfile: %w", err)
		}
		fmt.Printf("Dockerfile written to %s\n", outputDockerfile)
	} else {
		fmt.Println("Generated Dockerfile:")
		fmt.Println("---")
		fmt.Print(dockerfile)
		fmt.Println("---")
	}

	// Determine image tag
	if tag == "" {
		tag = manifest.VersionedSlug()
		// Replace / with - for docker tag compatibility
		tag = filepath.Base(tag)
		if manifest.Metadata.Namespace != "" {
			tag = manifest.Metadata.Namespace + "/" + manifest.Metadata.Name
		}
		if manifest.Metadata.Version != "" {
			tag = tag + ":" + manifest.Metadata.Version
		} else {
			tag = tag + ":latest"
		}
	}

	fmt.Printf("\nImage tag: %s\n", tag)
	fmt.Println("\nTo build the image, run:")
	fmt.Printf("  docker build -t %s .\n", tag)

	if push {
		fmt.Println("\nTo push the image after building:")
		fmt.Printf("  docker push %s\n", tag)
	}

	return nil
}

// DevCommand runs the tool in development mode.
type DevCommand struct{}

func (c *DevCommand) Name() string        { return "dev" }
func (c *DevCommand) Description() string { return "Run tool in development mode" }

func (c *DevCommand) Usage() string {
	return `mcp-meta dev - Run tool in development mode

USAGE:
    mcp-meta dev [options]

OPTIONS:
    --manifest <FILE>    Manifest file (default: ./mcp-meta.yaml)
    --port <PORT>        Port for SSE transport (default: 3000)
    -h, --help           Print help

DESCRIPTION:
    Runs the tool locally with hot-reload support. The tool is started
    as a subprocess and can be tested interactively.
`
}

func (c *DevCommand) Run(args []string) error {
	fs := flag.NewFlagSet("dev", flag.ContinueOnError)

	var manifest string
	var port int
	var showHelp bool

	fs.StringVar(&manifest, "manifest", "./mcp-meta.yaml", "Manifest file")
	fs.IntVar(&port, "port", 3000, "Port for SSE transport")
	fs.BoolVar(&showHelp, "help", false, "Print help")
	fs.BoolVar(&showHelp, "h", false, "Print help")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if showHelp {
		fmt.Print(c.Usage())
		return nil
	}

	// TODO: Implement dev server
	fmt.Println("Starting development server...")
	fmt.Println("  Manifest:", manifest)
	fmt.Println("  Port:", port)
	fmt.Println()
	fmt.Println("Dev command not yet fully implemented.")
	fmt.Println("Use 'mcp-meta --manifests . --transport sse --port", port, "' for now.")

	return nil
}

// PublishCommand publishes the tool to the marketplace.
type PublishCommand struct{}

func (c *PublishCommand) Name() string        { return "publish" }
func (c *PublishCommand) Description() string { return "Publish tool to marketplace" }

func (c *PublishCommand) Usage() string {
	return `mcp-meta publish - Publish tool to marketplace

USAGE:
    mcp-meta publish [options]

OPTIONS:
    --manifest <FILE>    Manifest file (default: ./mcp-meta.yaml)
    --dry-run            Validate without publishing
    --env <ENV>          Target environment: staging, production (default: production)
    -h, --help           Print help

DESCRIPTION:
    Validates the manifest, builds the container image, and publishes
    the tool to the mcp-tools.io marketplace.
`
}

func (c *PublishCommand) Run(args []string) error {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)

	var manifest string
	var dryRun bool
	var env string
	var showHelp bool

	fs.StringVar(&manifest, "manifest", "./mcp-meta.yaml", "Manifest file")
	fs.BoolVar(&dryRun, "dry-run", false, "Validate without publishing")
	fs.StringVar(&env, "env", "production", "Target environment")
	fs.BoolVar(&showHelp, "help", false, "Print help")
	fs.BoolVar(&showHelp, "h", false, "Print help")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if showHelp {
		fmt.Print(c.Usage())
		return nil
	}

	// TODO: Implement publish logic
	fmt.Println("Publishing tool to marketplace...")
	fmt.Println("  Manifest:", manifest)
	fmt.Println("  Environment:", env)
	if dryRun {
		fmt.Println("  Mode: dry-run")
	}
	fmt.Println()
	fmt.Println("Publish command not yet implemented.")
	fmt.Println("Marketplace API integration coming soon.")

	return nil
}

// AuthCommand handles authentication.
type AuthCommand struct{}

func (c *AuthCommand) Name() string        { return "auth" }
func (c *AuthCommand) Description() string { return "Authenticate with marketplace" }

func (c *AuthCommand) Usage() string {
	return `mcp-meta auth - Authenticate with marketplace

USAGE:
    mcp-meta auth <subcommand>

SUBCOMMANDS:
    login      Log in to the marketplace
    logout     Log out
    status     Show authentication status

EXAMPLES:
    mcp-meta auth login
    mcp-meta auth status
`
}

func (c *AuthCommand) Run(args []string) error {
	if len(args) == 0 {
		fmt.Print(c.Usage())
		return nil
	}

	subcommand := args[0]

	switch subcommand {
	case "login":
		fmt.Println("Opening browser for authentication...")
		fmt.Println()
		fmt.Println("Auth login not yet implemented.")
		fmt.Println("OAuth integration with mcp-tools.io coming soon.")

	case "logout":
		fmt.Println("Logging out...")
		fmt.Println("Auth logout not yet implemented.")

	case "status":
		fmt.Println("Authentication status:")
		fmt.Println("  Not logged in")
		fmt.Println()
		fmt.Println("Run 'mcp-meta auth login' to authenticate.")

	case "-h", "--help", "help":
		fmt.Print(c.Usage())

	default:
		return fmt.Errorf("unknown auth subcommand: %s\n\n%s", subcommand, c.Usage())
	}

	return nil
}

// VersionCommand manages tool versioning.
type VersionCommand struct{}

func (c *VersionCommand) Name() string        { return "version" }
func (c *VersionCommand) Description() string { return "Show or manage version" }

func (c *VersionCommand) Usage() string {
	return `mcp-meta version - Show or manage version

USAGE:
    mcp-meta version [subcommand]

SUBCOMMANDS:
    (none)     Show mcp-meta version
    bump       Bump manifest version (patch, minor, major)
    set        Set specific version

EXAMPLES:
    mcp-meta version
    mcp-meta version bump patch
    mcp-meta version bump minor
    mcp-meta version set 2.0.0
`
}

func (c *VersionCommand) Run(args []string) error {
	if len(args) == 0 {
		fmt.Printf("mcp-meta %s\n", mcp.Version)
		return nil
	}

	subcommand := args[0]

	switch subcommand {
	case "bump":
		if len(args) < 2 {
			return fmt.Errorf("bump type required: patch, minor, or major")
		}
		bumpType := args[1]
		switch bumpType {
		case "patch", "minor", "major":
			fmt.Printf("Bumping %s version...\n", bumpType)
			fmt.Println("Version bump not yet implemented.")
		default:
			return fmt.Errorf("invalid bump type: %s (must be patch, minor, or major)", bumpType)
		}

	case "set":
		if len(args) < 2 {
			return fmt.Errorf("version required")
		}
		version := args[1]
		fmt.Printf("Setting version to %s...\n", version)
		fmt.Println("Version set not yet implemented.")

	case "-h", "--help", "help":
		fmt.Print(c.Usage())

	default:
		return fmt.Errorf("unknown version subcommand: %s\n\n%s", subcommand, c.Usage())
	}

	return nil
}
