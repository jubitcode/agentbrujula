package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew(t *testing.T) {
	r := New()
	if r == nil {
		t.Fatal("New() returned nil")
	}

	// Should have no tools initially
	tools := r.AllTools()
	if len(tools) != 0 {
		t.Errorf("New registry has %d tools, want 0", len(tools))
	}
}

func TestRegistry_LoadManifests_SingleFile(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "test.yaml")

	manifest := `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: test-pod
spec:
  command: ["echo", "hello"]
  tools:
    - name: greet
      description: Greet the user
      inputSchema:
        type: object
        properties:
          name:
            type: string
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	r := New()
	if err := r.LoadManifests(manifestPath); err != nil {
		t.Fatalf("LoadManifests() error = %v", err)
	}

	tools := r.AllTools()
	if len(tools) != 1 {
		t.Errorf("Got %d tools, want 1", len(tools))
	}

	if tools[0].Name != "test-pod.greet" {
		t.Errorf("Tool name = %q, want %q", tools[0].Name, "test-pod.greet")
	}
}

func TestRegistry_LoadManifests_Directory(t *testing.T) {
	dir := t.TempDir()

	// Create multiple manifest files
	manifests := map[string]string{
		"pod1.yaml": `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: pod1
spec:
  command: ["echo", "1"]
  tools:
    - name: tool1
      description: Tool 1
`,
		"pod2.yml": `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: pod2
spec:
  command: ["echo", "2"]
  tools:
    - name: tool2
      description: Tool 2
    - name: tool3
      description: Tool 3
`,
	}

	for name, content := range manifests {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", name, err)
		}
	}

	r := New()
	if err := r.LoadManifests(dir); err != nil {
		t.Fatalf("LoadManifests() error = %v", err)
	}

	tools := r.AllTools()
	if len(tools) != 3 {
		t.Errorf("Got %d tools, want 3", len(tools))
	}
}

func TestRegistry_LoadManifests_NestedDirectory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	manifest := `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: nested-pod
spec:
  command: ["echo", "nested"]
  tools:
    - name: nested-tool
      description: Nested tool
`
	if err := os.WriteFile(filepath.Join(subdir, "nested.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	r := New()
	if err := r.LoadManifests(dir); err != nil {
		t.Fatalf("LoadManifests() error = %v", err)
	}

	tools := r.AllTools()
	if len(tools) != 1 {
		t.Errorf("Got %d tools, want 1", len(tools))
	}
}

func TestRegistry_LoadManifests_DuplicatePodName(t *testing.T) {
	dir := t.TempDir()

	manifest := `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: duplicate
spec:
  command: ["echo"]
  tools: []
`
	if err := os.WriteFile(filepath.Join(dir, "dup1.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("Failed to write manifest 1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dup2.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("Failed to write manifest 2: %v", err)
	}

	r := New()
	err := r.LoadManifests(dir)
	if err == nil {
		t.Error("LoadManifests() should fail with duplicate pod names")
	}
}

func TestRegistry_LoadManifests_InvalidManifest(t *testing.T) {
	dir := t.TempDir()

	// Invalid apiVersion
	manifest := `
apiVersion: invalid/v1
kind: ToolPod
metadata:
  name: invalid
spec:
  command: ["echo"]
  tools: []
`
	if err := os.WriteFile(filepath.Join(dir, "invalid.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	r := New()
	err := r.LoadManifests(dir)
	if err == nil {
		t.Error("LoadManifests() should fail with invalid manifest")
	}
}

func TestRegistry_LoadManifests_NonexistentPath(t *testing.T) {
	r := New()
	err := r.LoadManifests("/nonexistent/path")
	if err == nil {
		t.Error("LoadManifests() should fail with nonexistent path")
	}
}

func TestRegistry_LoadManifests_RelativeWorkdir(t *testing.T) {
	dir := t.TempDir()

	manifest := `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: workdir-pod
spec:
  command: ["python", "main.py"]
  workdir: ./src
  tools:
    - name: work
      description: Do work
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	r := New()
	if err := r.LoadManifests(filepath.Join(dir, "manifest.yaml")); err != nil {
		t.Fatalf("LoadManifests() error = %v", err)
	}

	spec := r.GetPodSpec("workdir-pod")
	if spec == nil {
		t.Fatal("GetPodSpec() returned nil")
	}

	// Workdir should be resolved to absolute path
	if !filepath.IsAbs(spec.Workdir) {
		t.Errorf("Workdir should be absolute, got %q", spec.Workdir)
	}
}

func TestRegistry_ResolveTool_FullyQualifiedName(t *testing.T) {
	dir := t.TempDir()

	manifest := `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: my-pod
spec:
  command: ["echo"]
  tools:
    - name: my-tool
      description: My tool
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	r := New()
	if err := r.LoadManifests(dir); err != nil {
		t.Fatalf("LoadManifests() error = %v", err)
	}

	podSpec, toolDef, err := r.ResolveTool("my-pod.my-tool")
	if err != nil {
		t.Fatalf("ResolveTool() error = %v", err)
	}
	if podSpec == nil {
		t.Error("ResolveTool() returned nil podSpec")
	}
	if toolDef == nil {
		t.Error("ResolveTool() returned nil toolDef")
	}
	if toolDef.Name != "my-tool" {
		t.Errorf("toolDef.Name = %q, want %q", toolDef.Name, "my-tool")
	}
}

func TestRegistry_ResolveTool_ShortName(t *testing.T) {
	dir := t.TempDir()

	manifest := `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: my-pod
spec:
  command: ["echo"]
  tools:
    - name: unique-tool
      description: Unique tool
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	r := New()
	if err := r.LoadManifests(dir); err != nil {
		t.Fatalf("LoadManifests() error = %v", err)
	}

	// Should resolve by short name when unambiguous
	podSpec, toolDef, err := r.ResolveTool("unique-tool")
	if err != nil {
		t.Fatalf("ResolveTool() error = %v", err)
	}
	if toolDef.Name != "unique-tool" {
		t.Errorf("toolDef.Name = %q, want %q", toolDef.Name, "unique-tool")
	}
	if podSpec.Name != "my-pod" {
		t.Errorf("podSpec.Name = %q, want %q", podSpec.Name, "my-pod")
	}
}

func TestRegistry_ResolveTool_AmbiguousShortName(t *testing.T) {
	dir := t.TempDir()

	manifests := map[string]string{
		"pod1.yaml": `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: pod1
spec:
  command: ["echo", "1"]
  tools:
    - name: shared
      description: Shared tool 1
`,
		"pod2.yaml": `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: pod2
spec:
  command: ["echo", "2"]
  tools:
    - name: shared
      description: Shared tool 2
`,
	}

	for name, content := range manifests {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", name, err)
		}
	}

	r := New()
	if err := r.LoadManifests(dir); err != nil {
		t.Fatalf("LoadManifests() error = %v", err)
	}

	// Short name should be ambiguous
	_, _, err := r.ResolveTool("shared")
	if err == nil {
		t.Error("ResolveTool() should fail for ambiguous short name")
	}

	// FQN should still work
	_, toolDef, err := r.ResolveTool("pod1.shared")
	if err != nil {
		t.Fatalf("ResolveTool() error = %v", err)
	}
	if toolDef.Description != "Shared tool 1" {
		t.Errorf("Got wrong tool: %q", toolDef.Description)
	}
}

func TestRegistry_ResolveTool_NotFound(t *testing.T) {
	r := New()
	_, _, err := r.ResolveTool("nonexistent")
	if err == nil {
		t.Error("ResolveTool() should fail for nonexistent tool")
	}
}

func TestRegistry_GetPodSpec(t *testing.T) {
	dir := t.TempDir()

	manifest := `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: my-pod
spec:
  command: ["echo"]
  callTimeout: 60s
  tools: []
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	r := New()
	if err := r.LoadManifests(dir); err != nil {
		t.Fatalf("LoadManifests() error = %v", err)
	}

	spec := r.GetPodSpec("my-pod")
	if spec == nil {
		t.Fatal("GetPodSpec() returned nil")
	}
	if spec.Name != "my-pod" {
		t.Errorf("spec.Name = %q, want %q", spec.Name, "my-pod")
	}

	// Nonexistent pod
	spec = r.GetPodSpec("nonexistent")
	if spec != nil {
		t.Error("GetPodSpec() should return nil for nonexistent pod")
	}
}

func TestRegistry_PodsToPreWarm(t *testing.T) {
	dir := t.TempDir()

	manifests := map[string]string{
		"warm.yaml": `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: warm-pod
spec:
  command: ["echo"]
  preWarm: true
  tools: []
`,
		"cold.yaml": `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: cold-pod
spec:
  command: ["echo"]
  preWarm: false
  tools: []
`,
		"default.yaml": `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: default-pod
spec:
  command: ["echo"]
  tools: []
`,
	}

	for name, content := range manifests {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", name, err)
		}
	}

	r := New()
	if err := r.LoadManifests(dir); err != nil {
		t.Fatalf("LoadManifests() error = %v", err)
	}

	pods := r.PodsToPreWarm()
	if len(pods) != 1 {
		t.Errorf("Got %d pods to prewarm, want 1", len(pods))
	}
	if len(pods) > 0 && pods[0].Name != "warm-pod" {
		t.Errorf("pods[0].Name = %q, want %q", pods[0].Name, "warm-pod")
	}
}

func TestRegistry_Validate(t *testing.T) {
	dir := t.TempDir()

	// All valid manifests
	manifest := `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: valid-pod
spec:
  command: ["echo"]
  tools:
    - name: valid-tool
      description: Valid tool
`
	if err := os.WriteFile(filepath.Join(dir, "valid.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	r := New()
	if err := r.LoadManifests(dir); err != nil {
		t.Fatalf("LoadManifests() error = %v", err)
	}

	errs := r.Validate()
	if len(errs) != 0 {
		t.Errorf("Validate() returned %d errors, want 0: %v", len(errs), errs)
	}
}

func TestRegistry_AllTools_DefaultInputSchema(t *testing.T) {
	dir := t.TempDir()

	// Tool without inputSchema
	manifest := `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: my-pod
spec:
  command: ["echo"]
  tools:
    - name: no-schema
      description: Tool without schema
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	r := New()
	if err := r.LoadManifests(dir); err != nil {
		t.Fatalf("LoadManifests() error = %v", err)
	}

	tools := r.AllTools()
	if len(tools) != 1 {
		t.Fatalf("Got %d tools, want 1", len(tools))
	}

	// Should have default schema
	expected := `{"type":"object"}`
	if string(tools[0].InputSchema) != expected {
		t.Errorf("InputSchema = %q, want %q", string(tools[0].InputSchema), expected)
	}
}

func TestRegistry_IgnoresNonYAMLFiles(t *testing.T) {
	dir := t.TempDir()

	// Write non-YAML files
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Readme"), 0644); err != nil {
		t.Fatalf("Failed to write readme: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to write json: %v", err)
	}

	// Write one valid YAML
	manifest := `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: my-pod
spec:
  command: ["echo"]
  tools: []
`
	if err := os.WriteFile(filepath.Join(dir, "pod.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("Failed to write manifest: %v", err)
	}

	r := New()
	if err := r.LoadManifests(dir); err != nil {
		t.Fatalf("LoadManifests() error = %v", err)
	}

	// Should only have one pod from the yaml file
	spec := r.GetPodSpec("my-pod")
	if spec == nil {
		t.Error("Should have loaded my-pod from yaml file")
	}
}
