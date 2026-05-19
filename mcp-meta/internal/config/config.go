package config

import (
	"flag"
	"fmt"
)

type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportSSE   Transport = "sse"
	TransportAPI   Transport = "api" // HTTP REST API mode
)

type Config struct {
	ManifestsPath string
	Transport     Transport
	Port          int
	Host          string
	LogLevel      string
	LogJSON       bool
	ValidateOnly  bool
	ShowVersion   bool
	ShowHelp      bool
}

func New() *Config {
	return &Config{
		ManifestsPath: "./manifests",
		Transport:     TransportStdio,
		Port:          3000,
		Host:          "127.0.0.1",
		LogLevel:      "info",
	}
}

func Parse(args []string) (*Config, error) {
	cfg := New()

	fs := flag.NewFlagSet("mcp-meta", flag.ContinueOnError)

	fs.StringVar(&cfg.ManifestsPath, "manifests", cfg.ManifestsPath, "Directory containing tool manifests")
	fs.StringVar(&cfg.ManifestsPath, "m", cfg.ManifestsPath, "Directory containing tool manifests (shorthand)")

	var transport string
	fs.StringVar(&transport, "transport", string(cfg.Transport), "Transport type: stdio, sse")
	fs.StringVar(&transport, "t", string(cfg.Transport), "Transport type (shorthand)")

	fs.IntVar(&cfg.Port, "port", cfg.Port, "Port for SSE transport")
	fs.IntVar(&cfg.Port, "p", cfg.Port, "Port for SSE transport (shorthand)")

	fs.StringVar(&cfg.Host, "host", cfg.Host, "Host for SSE transport")

	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "Log level: debug, info, warn, error")
	fs.StringVar(&cfg.LogLevel, "l", cfg.LogLevel, "Log level (shorthand)")

	fs.BoolVar(&cfg.LogJSON, "log-json", cfg.LogJSON, "Output logs as JSON")
	fs.BoolVar(&cfg.LogJSON, "j", cfg.LogJSON, "Output logs as JSON (shorthand)")

	fs.BoolVar(&cfg.ValidateOnly, "validate", cfg.ValidateOnly, "Validate manifests and exit")

	fs.BoolVar(&cfg.ShowVersion, "version", cfg.ShowVersion, "Show version")
	fs.BoolVar(&cfg.ShowVersion, "v", cfg.ShowVersion, "Show version (shorthand)")

	fs.BoolVar(&cfg.ShowHelp, "help", cfg.ShowHelp, "Show help")
	fs.BoolVar(&cfg.ShowHelp, "h", cfg.ShowHelp, "Show help (shorthand)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	cfg.Transport = Transport(transport)

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.ManifestsPath == "" {
		return fmt.Errorf("manifests path is required")
	}

	switch c.Transport {
	case TransportStdio, TransportSSE, TransportAPI:
		// Valid
	default:
		return fmt.Errorf("invalid transport: %s (must be stdio, sse, or api)", c.Transport)
	}

	if c.Transport == TransportSSE || c.Transport == TransportAPI {
		if c.Port < 1 || c.Port > 65535 {
			return fmt.Errorf("invalid port: %d (must be 1-65535)", c.Port)
		}
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
		// Valid
	default:
		return fmt.Errorf("invalid log level: %s (must be debug, info, warn, or error)", c.LogLevel)
	}

	return nil
}

func Usage() string {
	return `mcp-meta - Daemonless MCP tool orchestrator

USAGE:
    mcp-meta [OPTIONS]

OPTIONS:
    -m, --manifests <DIR>      Directory containing tool manifests (default: ./manifests)
    -t, --transport <TYPE>     Transport type: stdio, sse, api (default: stdio)
    -p, --port <PORT>          Port for SSE/API transport (default: 3000)
    --host <HOST>              Host for SSE/API transport (default: 127.0.0.1)
    -l, --log-level <LEVEL>    Log level: debug, info, warn, error (default: info)
    -j, --log-json             Output logs as JSON
    --validate                 Validate manifests and exit
    -v, --version              Print version
    -h, --help                 Print help

EXAMPLES:
    # stdio mode (for Claude Desktop)
    mcp-meta --manifests /path/to/manifests

    # SSE mode (for web MCP clients)
    mcp-meta --manifests /path/to/manifests --transport sse --port 8080

    # API mode (REST API + MCP over HTTP)
    mcp-meta --manifests /path/to/manifests --transport api --port 8080

    # Debug mode with JSON logs
    mcp-meta --manifests /path/to/manifests --log-level debug --log-json

    # Validate manifests only
    mcp-meta --manifests /path/to/manifests --validate
`
}
