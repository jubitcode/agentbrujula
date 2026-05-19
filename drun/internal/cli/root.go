package cli

import (
	"context"
	"fmt"
	"os"
	goruntime "runtime"
	"strings"

	"github.com/spf13/cobra"

	"drun/internal/config"
	"drun/internal/runtime"
	"drun/internal/runtime/docker"
	"drun/internal/runtime/firecracker"
	"drun/internal/runtime/sandbox"
	"drun/internal/runtime/vz"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

type GlobalOptions struct {
	ConfigPath  string
	Verbose     bool
	Quiet       bool
	Image       string
	Network     []string
	HostNetwork bool
	Ports       []string
	Env         []string
	Sandbox     bool
	Rebuild     bool
	NoCache     bool
	Dev         bool   // Enable dev mode with LSP/DAP port exposure
	Runtime     string // Runtime backend: docker, sandbox, vz (macOS), firecracker (Linux)
}

func NewGlobalOptions() *GlobalOptions {
	return &GlobalOptions{
		Network: make([]string, 0),
		Ports:   make([]string, 0),
		Env:     make([]string, 0),
	}
}

type AppContext struct {
	Options  *GlobalOptions
	Config   *config.Config
	Runtime  *runtime.Runtime
	Detector *runtime.Detector
}

func NewAppContext() *AppContext {
	return &AppContext{
		Options: NewGlobalOptions(),
	}
}

func (c *AppContext) WithOptions(opts *GlobalOptions) *AppContext {
	c.Options = opts
	return c
}

type BuildInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

func GetVersion() string {
	return version
}

func GetBuildInfo() *BuildInfo {
	return &BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
	}
}

func NewRootCommand() *cobra.Command {
	opts := NewGlobalOptions()
	ctx := NewAppContext()

	cmd := &cobra.Command{
		Use:   "drun",
		Short: "Run code in Docker containers with minimal friction",
		Long: `drun is a CLI tool for running code in Docker containers with minimal friction.
It maintains persistent containers, handles environment setup, and provides
excellent developer experience.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, buildDate),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			ctx.Options = opts
			return initializeContext(ctx)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := cmd.PersistentFlags()
	pf.StringVar(&opts.ConfigPath, "config", "", "Config file path")
	pf.BoolVarP(&opts.Verbose, "verbose", "v", false, "Verbose output")
	pf.BoolVarP(&opts.Quiet, "quiet", "q", false, "Quiet output")
	pf.StringVar(&opts.Image, "image", "", "Override image")
	pf.StringArrayVar(&opts.Network, "network", nil, "Join network (repeatable)")
	pf.BoolVar(&opts.HostNetwork, "host-network", false, "Use host network")
	pf.StringArrayVarP(&opts.Ports, "port", "p", nil, "Expose port (repeatable)")
	pf.StringArrayVarP(&opts.Env, "env", "e", nil, "Set env var (repeatable)")
	pf.BoolVar(&opts.Sandbox, "sandbox", false, "Enable sandbox mode")
	pf.BoolVar(&opts.Rebuild, "rebuild", false, "Rebuild image")
	pf.BoolVar(&opts.NoCache, "no-cache", false, "Disable all caching")
	pf.BoolVar(&opts.Dev, "dev", false, "Enable dev mode (expose LSP/DAP ports)")
	pf.StringVar(&opts.Runtime, "runtime", "", "Runtime backend: docker, sandbox, vz (macOS), firecracker (Linux)")

	cmd.AddCommand(NewRunCommand(ctx))
	cmd.AddCommand(NewBuildCommand(ctx))
	cmd.AddCommand(NewPSCommand(ctx))
	cmd.AddCommand(NewScriptsCommand(ctx))
	cmd.AddCommand(NewSnapshotCommand(ctx))
	cmd.AddCommand(NewDebugCommand(ctx))
	cmd.AddCommand(NewCleanupCommand(ctx))
	cmd.AddCommand(NewInitCommand(ctx))
	cmd.AddCommand(NewServicesCommand(ctx))
	cmd.AddCommand(NewEnvCommand(ctx))
	cmd.AddCommand(NewDiffCommand(ctx))
	cmd.AddCommand(NewStopCommand(ctx))
	cmd.AddCommand(NewStatusCommand(ctx))
	cmd.AddCommand(NewPruneCommand(ctx))
	cmd.AddCommand(NewRemoteCommand(ctx))
	cmd.AddCommand(NewLSPCommand(ctx))
	cmd.AddCommand(NewPathmapCommand(ctx))

	addScriptCommands(cmd, ctx)

	return cmd
}

func addScriptCommands(rootCmd *cobra.Command, appCtx *AppContext) {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	loader := config.NewLoader(cwd)
	cfg, err := loader.Load()
	if err != nil || cfg == nil {
		return
	}

	existingCmds := make(map[string]bool)
	for _, cmd := range rootCmd.Commands() {
		existingCmds[cmd.Name()] = true
		for _, alias := range cmd.Aliases {
			existingCmds[alias] = true
		}
	}

	for name, script := range cfg.Scripts {
		if existingCmds[name] {
			continue
		}

		scriptName := name
		scriptDef := script

		description := scriptDef.Description
		if description == "" {
			if len(scriptDef.Composite) > 0 {
				description = fmt.Sprintf("Run composite script [%s]", strings.Join(scriptDef.Composite, ", "))
			} else {
				description = fmt.Sprintf("Run: %s", scriptDef.Command)
			}
		}

		opts := &ScriptsOptions{}
		cmd := &cobra.Command{
			Use:   scriptName + " [args...]",
			Short: description,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runScript(cmd.Context(), appCtx, opts, scriptName, args)
			},
		}
		cmd.Flags().BoolVar(&opts.Setup, "setup", false, "Run setup before script")

		rootCmd.AddCommand(cmd)
	}
}

func initializeContext(appCtx *AppContext) error {
	detector := runtime.NewDetector(nil)
	rt, err := detector.Detect(context.Background())
	if err != nil {
		appCtx.Runtime = nil
	} else {
		appCtx.Runtime = rt
	}
	appCtx.Detector = detector

	if appCtx.Options.ConfigPath != "" {
		loader := config.NewLoader(appCtx.Options.ConfigPath)
		cfg, err := loader.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		appCtx.Config = cfg
	} else {
		cwd, err := os.Getwd()
		if err == nil {
			loader := config.NewLoader(cwd)
			cfg, _ := loader.Load()
			appCtx.Config = cfg
		}
	}

	return nil
}

func (c *AppContext) NewClient(ctx context.Context) (*runtime.Client, error) {
	if c.Runtime == nil {
		return nil, fmt.Errorf("no container runtime available")
	}
	return runtime.NewClient(ctx, &runtime.ClientOptions{Runtime: c.Runtime})
}

// NewBackend creates a RuntimeBackend based on the --runtime flag or auto-detects the best available.
// Priority order without explicit flag:
// - Linux: Firecracker > Docker > Sandbox
// - macOS: VZ > Docker > Sandbox
// - Other: Docker > Sandbox
func (c *AppContext) NewBackend(ctx context.Context) (runtime.RuntimeBackend, error) {
	registry := runtime.NewRegistry()

	// Register available backends

	// Docker backend (if available)
	dockerBackend, err := docker.NewDockerBackend(ctx, "")
	if err == nil && dockerBackend.Available() {
		registry.Register(runtime.BackendDocker, dockerBackend)
	}

	// Sandbox backend (always available)
	sandboxBackend := sandbox.NewSandboxBackend("")
	registry.Register(runtime.BackendSandbox, sandboxBackend)

	// Platform-specific backends
	switch goruntime.GOOS {
	case "darwin":
		// VZ backend (Virtualization.framework) for macOS
		if vzBackend, err := vz.NewVZBackend(); err == nil && vzBackend.Available() {
			registry.Register(runtime.BackendVZ, vzBackend)
		}
	case "linux":
		// Firecracker backend for Linux
		if fcBackend, err := firecracker.NewFirecrackerBackend(""); err == nil && fcBackend.Available() {
			registry.Register(runtime.BackendFirecracker, fcBackend)
		}
	}

	// If user specified a runtime, use it
	if c.Options.Runtime != "" {
		backendType := runtime.BackendType(c.Options.Runtime)
		backend, err := registry.GetOrError(backendType)
		if err != nil {
			// Clean up unused backends
			registry.Close()
			return nil, fmt.Errorf("runtime %q not available: %w", c.Options.Runtime, err)
		}

		// Close other backends
		if dockerBackend != nil && backendType != runtime.BackendDocker {
			dockerBackend.Close()
		}

		return backend, nil
	}

	// Auto-select best available backend
	backend, err := registry.AutoSelect(ctx)
	if err != nil {
		registry.Close()
		return nil, fmt.Errorf("no runtime backend available: %w", err)
	}

	return backend, nil
}

// ListAvailableBackends returns all available runtime backends for the current platform.
func (c *AppContext) ListAvailableBackends(ctx context.Context) []runtime.BackendType {
	var available []runtime.BackendType

	// Check Docker
	dockerBackend, err := docker.NewDockerBackend(ctx, "")
	if err == nil {
		if dockerBackend.Available() {
			available = append(available, runtime.BackendDocker)
		}
		dockerBackend.Close()
	}

	// Sandbox is always available
	available = append(available, runtime.BackendSandbox)

	// Platform-specific
	switch goruntime.GOOS {
	case "darwin":
		// VZ backend (Virtualization.framework)
		if vzBackend, err := vz.NewVZBackend(); err == nil {
			if vzBackend.Available() {
				available = append(available, runtime.BackendVZ)
			}
		}
	case "linux":
		// Firecracker backend
		if fcBackend, err := firecracker.NewFirecrackerBackend(""); err == nil {
			if fcBackend.Available() {
				available = append(available, runtime.BackendFirecracker)
			}
		}
	}

	return available
}

func Execute() error {
	return NewRootCommand().Execute()
}
