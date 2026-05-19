package config

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	cfg := New()

	if cfg.ManifestsPath != "./manifests" {
		t.Errorf("ManifestsPath = %q, want %q", cfg.ManifestsPath, "./manifests")
	}
	if cfg.Transport != TransportStdio {
		t.Errorf("Transport = %q, want %q", cfg.Transport, TransportStdio)
	}
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want %d", cfg.Port, 3000)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", cfg.Host, "127.0.0.1")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.LogJSON {
		t.Error("LogJSON should be false by default")
	}
	if cfg.ValidateOnly {
		t.Error("ValidateOnly should be false by default")
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    func(*Config) bool
		wantErr bool
	}{
		{
			name: "empty args uses defaults",
			args: []string{},
			want: func(c *Config) bool {
				return c.ManifestsPath == "./manifests" &&
					c.Transport == TransportStdio &&
					c.Port == 3000
			},
		},
		{
			name: "manifests long flag",
			args: []string{"--manifests", "/custom/path"},
			want: func(c *Config) bool {
				return c.ManifestsPath == "/custom/path"
			},
		},
		{
			name: "manifests short flag",
			args: []string{"-m", "/short/path"},
			want: func(c *Config) bool {
				return c.ManifestsPath == "/short/path"
			},
		},
		{
			name: "transport sse long",
			args: []string{"--transport", "sse"},
			want: func(c *Config) bool {
				return c.Transport == TransportSSE
			},
		},
		{
			name: "transport sse short",
			args: []string{"-t", "sse"},
			want: func(c *Config) bool {
				return c.Transport == TransportSSE
			},
		},
		{
			name: "port long",
			args: []string{"--port", "8080"},
			want: func(c *Config) bool {
				return c.Port == 8080
			},
		},
		{
			name: "port short",
			args: []string{"-p", "9000"},
			want: func(c *Config) bool {
				return c.Port == 9000
			},
		},
		{
			name: "host",
			args: []string{"--host", "0.0.0.0"},
			want: func(c *Config) bool {
				return c.Host == "0.0.0.0"
			},
		},
		{
			name: "log level long",
			args: []string{"--log-level", "debug"},
			want: func(c *Config) bool {
				return c.LogLevel == "debug"
			},
		},
		{
			name: "log level short",
			args: []string{"-l", "error"},
			want: func(c *Config) bool {
				return c.LogLevel == "error"
			},
		},
		{
			name: "log json long",
			args: []string{"--log-json"},
			want: func(c *Config) bool {
				return c.LogJSON == true
			},
		},
		{
			name: "log json short",
			args: []string{"-j"},
			want: func(c *Config) bool {
				return c.LogJSON == true
			},
		},
		{
			name: "validate",
			args: []string{"--validate"},
			want: func(c *Config) bool {
				return c.ValidateOnly == true
			},
		},
		{
			name: "version long",
			args: []string{"--version"},
			want: func(c *Config) bool {
				return c.ShowVersion == true
			},
		},
		{
			name: "version short",
			args: []string{"-v"},
			want: func(c *Config) bool {
				return c.ShowVersion == true
			},
		},
		{
			name: "help long",
			args: []string{"--help"},
			want: func(c *Config) bool {
				return c.ShowHelp == true
			},
		},
		{
			name: "help short",
			args: []string{"-h"},
			want: func(c *Config) bool {
				return c.ShowHelp == true
			},
		},
		{
			name: "multiple flags",
			args: []string{"-m", "/path", "-t", "sse", "-p", "8080", "-l", "debug", "-j"},
			want: func(c *Config) bool {
				return c.ManifestsPath == "/path" &&
					c.Transport == TransportSSE &&
					c.Port == 8080 &&
					c.LogLevel == "debug" &&
					c.LogJSON == true
			},
		},
		{
			name:    "invalid flag",
			args:    []string{"--invalid"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil && !tt.want(got) {
				t.Errorf("Parse() returned unexpected config: %+v", got)
			}
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid default config",
			config: &Config{
				ManifestsPath: "./manifests",
				Transport:     TransportStdio,
				Port:          3000,
				LogLevel:      "info",
			},
			wantErr: false,
		},
		{
			name: "valid sse config",
			config: &Config{
				ManifestsPath: "./manifests",
				Transport:     TransportSSE,
				Port:          8080,
				LogLevel:      "info",
			},
			wantErr: false,
		},
		{
			name: "empty manifests path",
			config: &Config{
				ManifestsPath: "",
				Transport:     TransportStdio,
				LogLevel:      "info",
			},
			wantErr: true,
			errMsg:  "manifests path is required",
		},
		{
			name: "invalid transport",
			config: &Config{
				ManifestsPath: "./manifests",
				Transport:     "invalid",
				LogLevel:      "info",
			},
			wantErr: true,
			errMsg:  "invalid transport",
		},
		{
			name: "port too low for sse",
			config: &Config{
				ManifestsPath: "./manifests",
				Transport:     TransportSSE,
				Port:          0,
				LogLevel:      "info",
			},
			wantErr: true,
			errMsg:  "invalid port",
		},
		{
			name: "port too high for sse",
			config: &Config{
				ManifestsPath: "./manifests",
				Transport:     TransportSSE,
				Port:          70000,
				LogLevel:      "info",
			},
			wantErr: true,
			errMsg:  "invalid port",
		},
		{
			name: "invalid port for stdio is ok",
			config: &Config{
				ManifestsPath: "./manifests",
				Transport:     TransportStdio,
				Port:          0, // Invalid but not checked for stdio
				LogLevel:      "info",
			},
			wantErr: false,
		},
		{
			name: "invalid log level",
			config: &Config{
				ManifestsPath: "./manifests",
				Transport:     TransportStdio,
				LogLevel:      "invalid",
			},
			wantErr: true,
			errMsg:  "invalid log level",
		},
		{
			name: "log level debug",
			config: &Config{
				ManifestsPath: "./manifests",
				Transport:     TransportStdio,
				LogLevel:      "debug",
			},
			wantErr: false,
		},
		{
			name: "log level warn",
			config: &Config{
				ManifestsPath: "./manifests",
				Transport:     TransportStdio,
				LogLevel:      "warn",
			},
			wantErr: false,
		},
		{
			name: "log level error",
			config: &Config{
				ManifestsPath: "./manifests",
				Transport:     TransportStdio,
				LogLevel:      "error",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.errMsg != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestUsage(t *testing.T) {
	usage := Usage()

	// Check for important sections
	checks := []string{
		"mcp-meta",
		"USAGE:",
		"OPTIONS:",
		"--manifests",
		"--transport",
		"--port",
		"--host",
		"--log-level",
		"--log-json",
		"--validate",
		"--version",
		"--help",
		"EXAMPLES:",
		"stdio",
		"sse",
	}

	for _, check := range checks {
		if !strings.Contains(usage, check) {
			t.Errorf("Usage() missing %q", check)
		}
	}
}

func TestTransportConstants(t *testing.T) {
	if TransportStdio != "stdio" {
		t.Errorf("TransportStdio = %q, want %q", TransportStdio, "stdio")
	}
	if TransportSSE != "sse" {
		t.Errorf("TransportSSE = %q, want %q", TransportSSE, "sse")
	}
}
