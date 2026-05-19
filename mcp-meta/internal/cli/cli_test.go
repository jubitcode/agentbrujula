package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	cli := New()
	if cli == nil {
		t.Fatal("New() returned nil")
	}
	if cli.commands == nil {
		t.Fatal("commands map is nil")
	}

	// Check that all expected commands are registered
	expectedCommands := []string{"init", "build", "dev", "publish", "auth", "version"}
	for _, name := range expectedCommands {
		if _, ok := cli.commands[name]; !ok {
			t.Errorf("Command %q not registered", name)
		}
	}
}

func TestCLI_Register(t *testing.T) {
	cli := &CLI{commands: make(map[string]Command)}

	// Create a mock command
	mockCmd := &mockCommand{name: "test", desc: "Test command"}
	cli.Register(mockCmd)

	if cmd, ok := cli.commands["test"]; !ok {
		t.Error("Command not registered")
	} else if cmd.Name() != "test" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "test")
	}
}

func TestCLI_Run(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		handled  bool
		wantErr  bool
		setupCmd func(*CLI) // Optional: add mock command
	}{
		{
			name:    "empty args",
			args:    []string{},
			handled: false,
			wantErr: false,
		},
		{
			name:    "flag arg passes through",
			args:    []string{"--manifests", "/path"},
			handled: false,
			wantErr: false,
		},
		{
			name:    "short flag passes through",
			args:    []string{"-m", "/path"},
			handled: false,
			wantErr: false,
		},
		{
			name:    "unknown command passes through",
			args:    []string{"unknown"},
			handled: false,
			wantErr: false,
		},
		{
			name:    "version command handled",
			args:    []string{"version"},
			handled: true,
			wantErr: false,
		},
		{
			name:    "command with error",
			args:    []string{"failing"},
			handled: true,
			wantErr: true,
			setupCmd: func(c *CLI) {
				c.Register(&mockCommand{name: "failing", runErr: os.ErrNotExist})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cli := New()
			if tt.setupCmd != nil {
				tt.setupCmd(cli)
			}

			handled, err := cli.Run(tt.args)
			if handled != tt.handled {
				t.Errorf("handled = %v, want %v", handled, tt.handled)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCLI_IsSubcommand(t *testing.T) {
	cli := New()

	tests := []struct {
		arg  string
		want bool
	}{
		{"init", true},
		{"build", true},
		{"dev", true},
		{"publish", true},
		{"auth", true},
		{"version", true},
		{"unknown", false},
		{"--help", false},
		{"-m", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			if got := cli.IsSubcommand(tt.arg); got != tt.want {
				t.Errorf("IsSubcommand(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}

func TestCLI_PrintHelp(t *testing.T) {
	cli := New()

	// Capture stdout (we can't easily capture, so just ensure no panic)
	// In a real scenario, you'd redirect os.Stdout
	cli.PrintHelp()
	// If we get here without panic, that's good
}

// Helper function tests
func TestEnsureDir(t *testing.T) {
	dir := t.TempDir()
	newDir := filepath.Join(dir, "subdir", "nested")

	err := ensureDir(newDir)
	if err != nil {
		t.Fatalf("ensureDir() error = %v", err)
	}

	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		t.Error("Directory was not created")
	}

	// Should be idempotent
	err = ensureDir(newDir)
	if err != nil {
		t.Errorf("Second ensureDir() error = %v", err)
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := "hello world"

	err := writeFile(path, content)
	if err != nil {
		t.Fatalf("writeFile() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != content {
		t.Errorf("File content = %q, want %q", string(data), content)
	}
}

// mockCommand is a test helper
type mockCommand struct {
	name   string
	desc   string
	usage  string
	runErr error
}

func (m *mockCommand) Name() string        { return m.name }
func (m *mockCommand) Description() string { return m.desc }
func (m *mockCommand) Usage() string       { return m.usage }
func (m *mockCommand) Run(args []string) error {
	return m.runErr
}

// Command tests

func TestBuildCommand(t *testing.T) {
	cmd := &BuildCommand{}

	if cmd.Name() != "build" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "build")
	}
	if cmd.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if !strings.Contains(cmd.Usage(), "build") {
		t.Error("Usage() should mention 'build'")
	}
}

func TestBuildCommand_Help(t *testing.T) {
	cmd := &BuildCommand{}
	err := cmd.Run([]string{"--help"})
	if err != nil {
		t.Errorf("Run(--help) error = %v", err)
	}
}

func TestDevCommand(t *testing.T) {
	cmd := &DevCommand{}

	if cmd.Name() != "dev" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "dev")
	}
	if cmd.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if !strings.Contains(cmd.Usage(), "dev") {
		t.Error("Usage() should mention 'dev'")
	}
}

func TestDevCommand_Help(t *testing.T) {
	cmd := &DevCommand{}
	err := cmd.Run([]string{"--help"})
	if err != nil {
		t.Errorf("Run(--help) error = %v", err)
	}
}

func TestDevCommand_Run(t *testing.T) {
	cmd := &DevCommand{}
	// Just ensure it doesn't crash - it's a stub
	err := cmd.Run([]string{})
	if err != nil {
		t.Errorf("Run() error = %v", err)
	}
}

func TestPublishCommand(t *testing.T) {
	cmd := &PublishCommand{}

	if cmd.Name() != "publish" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "publish")
	}
	if cmd.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if !strings.Contains(cmd.Usage(), "publish") {
		t.Error("Usage() should mention 'publish'")
	}
}

func TestPublishCommand_Help(t *testing.T) {
	cmd := &PublishCommand{}
	err := cmd.Run([]string{"--help"})
	if err != nil {
		t.Errorf("Run(--help) error = %v", err)
	}
}

func TestPublishCommand_Run(t *testing.T) {
	cmd := &PublishCommand{}
	// Just ensure it doesn't crash - it's a stub
	err := cmd.Run([]string{})
	if err != nil {
		t.Errorf("Run() error = %v", err)
	}
}

func TestPublishCommand_DryRun(t *testing.T) {
	cmd := &PublishCommand{}
	err := cmd.Run([]string{"--dry-run"})
	if err != nil {
		t.Errorf("Run(--dry-run) error = %v", err)
	}
}

func TestAuthCommand(t *testing.T) {
	cmd := &AuthCommand{}

	if cmd.Name() != "auth" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "auth")
	}
	if cmd.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if !strings.Contains(cmd.Usage(), "auth") {
		t.Error("Usage() should mention 'auth'")
	}
}

func TestAuthCommand_NoArgs(t *testing.T) {
	cmd := &AuthCommand{}
	err := cmd.Run([]string{})
	if err != nil {
		t.Errorf("Run() error = %v", err)
	}
}

func TestAuthCommand_Login(t *testing.T) {
	cmd := &AuthCommand{}
	err := cmd.Run([]string{"login"})
	if err != nil {
		t.Errorf("Run(login) error = %v", err)
	}
}

func TestAuthCommand_Logout(t *testing.T) {
	cmd := &AuthCommand{}
	err := cmd.Run([]string{"logout"})
	if err != nil {
		t.Errorf("Run(logout) error = %v", err)
	}
}

func TestAuthCommand_Status(t *testing.T) {
	cmd := &AuthCommand{}
	err := cmd.Run([]string{"status"})
	if err != nil {
		t.Errorf("Run(status) error = %v", err)
	}
}

func TestAuthCommand_Help(t *testing.T) {
	cmd := &AuthCommand{}
	for _, arg := range []string{"-h", "--help", "help"} {
		err := cmd.Run([]string{arg})
		if err != nil {
			t.Errorf("Run(%s) error = %v", arg, err)
		}
	}
}

func TestAuthCommand_Unknown(t *testing.T) {
	cmd := &AuthCommand{}
	err := cmd.Run([]string{"unknown"})
	if err == nil {
		t.Error("Run(unknown) should return error")
	}
}

func TestVersionCommand(t *testing.T) {
	cmd := &VersionCommand{}

	if cmd.Name() != "version" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "version")
	}
	if cmd.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if !strings.Contains(cmd.Usage(), "version") {
		t.Error("Usage() should mention 'version'")
	}
}

func TestVersionCommand_NoArgs(t *testing.T) {
	cmd := &VersionCommand{}
	err := cmd.Run([]string{})
	if err != nil {
		t.Errorf("Run() error = %v", err)
	}
}

func TestVersionCommand_Bump(t *testing.T) {
	cmd := &VersionCommand{}

	// Valid bump types
	for _, bumpType := range []string{"patch", "minor", "major"} {
		err := cmd.Run([]string{"bump", bumpType})
		if err != nil {
			t.Errorf("Run(bump %s) error = %v", bumpType, err)
		}
	}

	// Missing bump type
	err := cmd.Run([]string{"bump"})
	if err == nil {
		t.Error("Run(bump) without type should error")
	}

	// Invalid bump type
	err = cmd.Run([]string{"bump", "invalid"})
	if err == nil {
		t.Error("Run(bump invalid) should error")
	}
}

func TestVersionCommand_Set(t *testing.T) {
	cmd := &VersionCommand{}

	err := cmd.Run([]string{"set", "2.0.0"})
	if err != nil {
		t.Errorf("Run(set 2.0.0) error = %v", err)
	}

	// Missing version
	err = cmd.Run([]string{"set"})
	if err == nil {
		t.Error("Run(set) without version should error")
	}
}

func TestVersionCommand_Help(t *testing.T) {
	cmd := &VersionCommand{}
	for _, arg := range []string{"-h", "--help", "help"} {
		err := cmd.Run([]string{arg})
		if err != nil {
			t.Errorf("Run(%s) error = %v", arg, err)
		}
	}
}

func TestVersionCommand_Unknown(t *testing.T) {
	cmd := &VersionCommand{}
	err := cmd.Run([]string{"unknown"})
	if err == nil {
		t.Error("Run(unknown) should return error")
	}
}
