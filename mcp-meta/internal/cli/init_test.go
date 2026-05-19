package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCommand(t *testing.T) {
	cmd := &InitCommand{}

	if cmd.Name() != "init" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "init")
	}
	if cmd.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if !strings.Contains(cmd.Usage(), "init") {
		t.Error("Usage() should mention 'init'")
	}
}

func TestInitCommand_Help(t *testing.T) {
	cmd := &InitCommand{}

	for _, arg := range []string{"-h", "--help"} {
		err := cmd.Run([]string{arg})
		if err != nil {
			t.Errorf("Run(%s) error = %v", arg, err)
		}
	}
}

func TestInitCommand_MissingName(t *testing.T) {
	cmd := &InitCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Run() without name should error")
	}
}

func TestInitCommand_Python(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "my-python-tool")

	cmd := &InitCommand{}
	err := cmd.Run([]string{"my-python-tool", "--language", "python", "--dir", projectDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Check files exist
	files := []string{
		"mcp-meta.yaml",
		"main.py",
		"pyproject.toml",
	}

	for _, f := range files {
		path := filepath.Join(projectDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("File %s not created", f)
		}
	}

	// Check manifest content
	manifest, err := os.ReadFile(filepath.Join(projectDir, "mcp-meta.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(mcp-meta.yaml) error = %v", err)
	}

	manifestStr := string(manifest)
	checks := []string{
		"apiVersion: mcp-meta/v1",
		"kind: ToolPod",
		"name: my-python-tool",
		"python",
	}

	for _, check := range checks {
		if !strings.Contains(manifestStr, check) {
			t.Errorf("Manifest missing %q", check)
		}
	}

	// Check main.py
	mainPy, err := os.ReadFile(filepath.Join(projectDir, "main.py"))
	if err != nil {
		t.Fatalf("ReadFile(main.py) error = %v", err)
	}

	if !strings.Contains(string(mainPy), "json") {
		t.Error("main.py should contain json handling")
	}
}

func TestInitCommand_Go(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "my-go-tool")

	cmd := &InitCommand{}
	err := cmd.Run([]string{"my-go-tool", "--language", "go", "--dir", projectDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Check files exist
	files := []string{
		"mcp-meta.yaml",
		"main.go",
		"go.mod",
	}

	for _, f := range files {
		path := filepath.Join(projectDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("File %s not created", f)
		}
	}

	// Check manifest content
	manifest, err := os.ReadFile(filepath.Join(projectDir, "mcp-meta.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(mcp-meta.yaml) error = %v", err)
	}

	if !strings.Contains(string(manifest), "go") {
		t.Error("Manifest should mention go")
	}

	// Check main.go
	mainGo, err := os.ReadFile(filepath.Join(projectDir, "main.go"))
	if err != nil {
		t.Fatalf("ReadFile(main.go) error = %v", err)
	}

	goChecks := []string{
		"package main",
		"func main()",
		"json",
	}

	for _, check := range goChecks {
		if !strings.Contains(string(mainGo), check) {
			t.Errorf("main.go missing %q", check)
		}
	}

	// Check go.mod
	goMod, err := os.ReadFile(filepath.Join(projectDir, "go.mod"))
	if err != nil {
		t.Fatalf("ReadFile(go.mod) error = %v", err)
	}

	if !strings.Contains(string(goMod), "module") {
		t.Error("go.mod should have module declaration")
	}
}

func TestInitCommand_Node(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "my-node-tool")

	cmd := &InitCommand{}
	err := cmd.Run([]string{"my-node-tool", "--language", "node", "--dir", projectDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Check files exist
	files := []string{
		"mcp-meta.yaml",
		"index.js",
		"package.json",
	}

	for _, f := range files {
		path := filepath.Join(projectDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("File %s not created", f)
		}
	}

	// Check package.json
	pkgJSON, err := os.ReadFile(filepath.Join(projectDir, "package.json"))
	if err != nil {
		t.Fatalf("ReadFile(package.json) error = %v", err)
	}

	pkgChecks := []string{
		"my-node-tool",
		"\"main\"",
		"index.js",
	}

	for _, check := range pkgChecks {
		if !strings.Contains(string(pkgJSON), check) {
			t.Errorf("package.json missing %q", check)
		}
	}

	// Check index.js
	indexJS, err := os.ReadFile(filepath.Join(projectDir, "index.js"))
	if err != nil {
		t.Fatalf("ReadFile(index.js) error = %v", err)
	}

	if !strings.Contains(string(indexJS), "readline") {
		t.Error("index.js should contain readline")
	}
}

func TestInitCommand_DefaultLanguage(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "default-tool")

	cmd := &InitCommand{}
	// No --language flag, should default to python
	err := cmd.Run([]string{"default-tool", "--dir", projectDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Should have created python files
	if _, err := os.Stat(filepath.Join(projectDir, "main.py")); os.IsNotExist(err) {
		t.Error("Default language should be python")
	}
}

func TestInitCommand_InvalidLanguage(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "invalid-tool")

	cmd := &InitCommand{}
	err := cmd.Run([]string{"invalid-tool", "--language", "rust", "--dir", projectDir})
	if err == nil {
		t.Error("Run() with invalid language should error")
	}
	if !strings.Contains(err.Error(), "invalid language") {
		t.Errorf("Error should mention invalid language: %v", err)
	}
}

func TestInitCommand_ExistingDirectory(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "existing")

	// Create the directory first
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	cmd := &InitCommand{}
	err := cmd.Run([]string{"existing", "--dir", projectDir})
	if err == nil {
		t.Error("Run() in existing directory should error")
	}
}

func TestInitCommand_MarketplaceFields(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "marketplace-tool")

	cmd := &InitCommand{}
	err := cmd.Run([]string{"marketplace-tool", "--dir", projectDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	manifest, err := os.ReadFile(filepath.Join(projectDir, "mcp-meta.yaml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// Check for marketplace-ready fields
	marketplaceChecks := []string{
		"namespace:",
		"version:",
		"description:",
		"marketplace:",
		"pricing:",
		"model:",
	}

	for _, check := range marketplaceChecks {
		if !strings.Contains(string(manifest), check) {
			t.Errorf("Manifest missing marketplace field %q", check)
		}
	}
}

func TestInitCommand_ToolDefinition(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "tool-def-test")

	cmd := &InitCommand{}
	err := cmd.Run([]string{"tool-def-test", "--dir", projectDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	manifest, err := os.ReadFile(filepath.Join(projectDir, "mcp-meta.yaml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// Check for tool definition
	toolChecks := []string{
		"tools:",
		"- name:",
		"description:",
		"inputSchema:",
	}

	for _, check := range toolChecks {
		if !strings.Contains(string(manifest), check) {
			t.Errorf("Manifest missing tool definition field %q", check)
		}
	}
}

func TestInitCommand_NameValidation(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"valid-name", false},
		{"my-tool", false},
		{"tool123", false},
		{"UPPERCASE", true},
		{"with spaces", true},
		{"with_underscore", true},
		{"with.dot", true},
		{"with/slash", true},
		{"", true},
	}

	cmd := &InitCommand{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			projectDir := filepath.Join(baseDir, "project")
			err := cmd.Run([]string{tt.name, "--dir", projectDir})
			if (err != nil) != tt.wantErr {
				t.Errorf("Run(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestInitCommand_PythonHandlesPrompt(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "python-prompt")

	cmd := &InitCommand{}
	err := cmd.Run([]string{"python-prompt", "--language", "python", "--dir", projectDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	mainPy, err := os.ReadFile(filepath.Join(projectDir, "main.py"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// Python code should handle JSON-RPC
	pythonChecks := []string{
		"sys.stdin",
		"sys.stdout",
		"jsonrpc",
		"method",
	}

	for _, check := range pythonChecks {
		if !strings.Contains(string(mainPy), check) {
			t.Errorf("main.py missing %q", check)
		}
	}
}

func TestInitCommand_GoHandlesPrompt(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "go-prompt")

	cmd := &InitCommand{}
	err := cmd.Run([]string{"go-prompt", "--language", "go", "--dir", projectDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	mainGo, err := os.ReadFile(filepath.Join(projectDir, "main.go"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// Go code should handle JSON-RPC
	goChecks := []string{
		"bufio.NewScanner",
		"json.Unmarshal",
		"json.Marshal",
	}

	for _, check := range goChecks {
		if !strings.Contains(string(mainGo), check) {
			t.Errorf("main.go missing %q", check)
		}
	}
}

func TestInitCommand_NodeHandlesPrompt(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "node-prompt")

	cmd := &InitCommand{}
	err := cmd.Run([]string{"node-prompt", "--language", "node", "--dir", projectDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	indexJS, err := os.ReadFile(filepath.Join(projectDir, "index.js"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// Node code should handle JSON-RPC
	nodeChecks := []string{
		"readline",
		"JSON.parse",
		"JSON.stringify",
	}

	for _, check := range nodeChecks {
		if !strings.Contains(string(indexJS), check) {
			t.Errorf("index.js missing %q", check)
		}
	}
}

func TestInitCommand_NamespaceOption(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "ns-tool")

	cmd := &InitCommand{}
	err := cmd.Run([]string{"ns-tool", "--namespace", "myorg", "--dir", projectDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	manifest, err := os.ReadFile(filepath.Join(projectDir, "mcp-meta.yaml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if !strings.Contains(string(manifest), "namespace: myorg") {
		t.Error("Manifest should have namespace: myorg")
	}
}
