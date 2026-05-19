package build

import (
	"strings"
	"testing"
	"time"

	"mcp-meta/internal/registry"
)

func TestNewDockerfileGenerator(t *testing.T) {
	g := NewDockerfileGenerator()
	if g == nil {
		t.Fatal("NewDockerfileGenerator() returned nil")
	}
}

func TestDockerfileGenerator_Generate_Python(t *testing.T) {
	g := NewDockerfileGenerator()

	manifest := &registry.Manifest{
		Metadata: registry.Metadata{Name: "python-tool"},
		Spec: registry.PodSpec{
			Command: []string{"python", "main.py"},
			Build:   &registry.BuildSpec{Runtime: "python:3.12"},
		},
	}

	dockerfile, err := g.Generate(manifest)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Check expected content
	checks := []string{
		"FROM python:3.12-slim",
		"WORKDIR /app",
		"COPY",
		"CMD [\"python\", \"main.py\"]",
	}

	for _, check := range checks {
		if !strings.Contains(dockerfile, check) {
			t.Errorf("Dockerfile missing %q", check)
		}
	}
}

func TestDockerfileGenerator_Generate_PythonWithUV(t *testing.T) {
	g := NewDockerfileGenerator()

	manifest := &registry.Manifest{
		Metadata: registry.Metadata{Name: "uv-tool"},
		Spec: registry.PodSpec{
			Command: []string{"uv", "run", "main.py"},
			Build:   &registry.BuildSpec{Runtime: "python:3.11"},
		},
	}

	dockerfile, err := g.Generate(manifest)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Should use UV-specific base image
	if !strings.Contains(dockerfile, "ghcr.io/astral-sh/uv") {
		t.Error("Dockerfile should use UV base image")
	}
	if !strings.Contains(dockerfile, "uv sync") {
		t.Error("Dockerfile should use uv sync")
	}
}

func TestDockerfileGenerator_Generate_Go(t *testing.T) {
	g := NewDockerfileGenerator()

	manifest := &registry.Manifest{
		Metadata: registry.Metadata{Name: "go-tool"},
		Spec: registry.PodSpec{
			Command: []string{"./server"},
			Build:   &registry.BuildSpec{Runtime: "go:1.22"},
		},
	}

	dockerfile, err := g.Generate(manifest)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Check for multi-stage build
	checks := []string{
		"FROM golang:1.22-alpine AS builder",
		"go mod download",
		"CGO_ENABLED=0",
		"FROM alpine:latest",
		"COPY --from=builder",
		"CMD [\"./server\"]",
	}

	for _, check := range checks {
		if !strings.Contains(dockerfile, check) {
			t.Errorf("Dockerfile missing %q", check)
		}
	}
}

func TestDockerfileGenerator_Generate_Node(t *testing.T) {
	g := NewDockerfileGenerator()

	manifest := &registry.Manifest{
		Metadata: registry.Metadata{Name: "node-tool"},
		Spec: registry.PodSpec{
			Command: []string{"node", "index.js"},
			Build:   &registry.BuildSpec{Runtime: "node:20"},
		},
	}

	dockerfile, err := g.Generate(manifest)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	checks := []string{
		"FROM node:20-alpine",
		"WORKDIR /app",
		"COPY package*.json",
		"npm ci",
		"CMD [\"node\", \"index.js\"]",
	}

	for _, check := range checks {
		if !strings.Contains(dockerfile, check) {
			t.Errorf("Dockerfile missing %q", check)
		}
	}
}

func TestDockerfileGenerator_Generate_WithResources(t *testing.T) {
	g := NewDockerfileGenerator()

	manifest := &registry.Manifest{
		Metadata: registry.Metadata{Name: "resource-tool"},
		Spec: registry.PodSpec{
			Command: []string{"python", "main.py"},
			Build:   &registry.BuildSpec{Runtime: "python:3.12"},
			Resources: &registry.ResourceSpec{
				CPU:    "0.5",
				Memory: "256Mi",
			},
		},
	}

	dockerfile, err := g.Generate(manifest)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Resource limits should be documented as comments
	if !strings.Contains(dockerfile, "# Memory: 256Mi") {
		t.Error("Dockerfile should document memory limit")
	}
	if !strings.Contains(dockerfile, "# CPU: 0.5") {
		t.Error("Dockerfile should document CPU limit")
	}
}

func TestDockerfileGenerator_Generate_CustomDockerfile(t *testing.T) {
	g := NewDockerfileGenerator()

	manifest := &registry.Manifest{
		Metadata: registry.Metadata{Name: "custom"},
		Spec: registry.PodSpec{
			Command: []string{"./app"},
			Build:   &registry.BuildSpec{Dockerfile: "./Dockerfile.custom"},
		},
	}

	_, err := g.Generate(manifest)
	if err == nil {
		t.Error("Generate() should fail when custom Dockerfile is specified")
	}
	if !strings.Contains(err.Error(), "custom Dockerfile") {
		t.Errorf("Error should mention custom Dockerfile: %v", err)
	}
}

func TestDockerfileGenerator_Generate_PrebuiltImage(t *testing.T) {
	g := NewDockerfileGenerator()

	manifest := &registry.Manifest{
		Metadata: registry.Metadata{Name: "prebuilt"},
		Spec: registry.PodSpec{
			Image: "ghcr.io/acme/tool:v1",
		},
	}

	_, err := g.Generate(manifest)
	if err == nil {
		t.Error("Generate() should fail when pre-built image is specified")
	}
	if !strings.Contains(err.Error(), "pre-built image") {
		t.Errorf("Error should mention pre-built image: %v", err)
	}
}

func TestDockerfileGenerator_Generate_UnsupportedRuntime(t *testing.T) {
	g := NewDockerfileGenerator()

	manifest := &registry.Manifest{
		Metadata: registry.Metadata{Name: "rust-tool"},
		Spec: registry.PodSpec{
			Command: []string{"./app"},
			Build:   &registry.BuildSpec{Runtime: "rust:1.70"},
		},
	}

	_, err := g.Generate(manifest)
	if err == nil {
		t.Error("Generate() should fail for unsupported runtime")
	}
	if !strings.Contains(err.Error(), "unsupported runtime") {
		t.Errorf("Error should mention unsupported runtime: %v", err)
	}
}

func TestDockerfileGenerator_Generate_NoRuntime(t *testing.T) {
	g := NewDockerfileGenerator()

	manifest := &registry.Manifest{
		Metadata: registry.Metadata{Name: "no-runtime"},
		Spec: registry.PodSpec{
			Command: []string{"./unknown"},
		},
	}

	_, err := g.Generate(manifest)
	if err == nil {
		t.Error("Generate() should fail when runtime cannot be determined")
	}
	if !strings.Contains(err.Error(), "cannot determine runtime") {
		t.Errorf("Error should mention cannot determine runtime: %v", err)
	}
}

func TestDetectRuntime(t *testing.T) {
	tests := []struct {
		name     string
		manifest *registry.Manifest
		want     string
	}{
		{
			name: "from build.runtime",
			manifest: &registry.Manifest{
				Spec: registry.PodSpec{
					Build: &registry.BuildSpec{Runtime: "python:3.11"},
				},
			},
			want: "python:3.11",
		},
		{
			name: "from spec.runtime (legacy)",
			manifest: &registry.Manifest{
				Spec: registry.PodSpec{
					Runtime: "go:1.21",
				},
			},
			want: "go:1.21",
		},
		{
			name: "from python command",
			manifest: &registry.Manifest{
				Spec: registry.PodSpec{
					Command: []string{"python", "main.py"},
				},
			},
			want: "python:3.12",
		},
		{
			name: "from python3 command",
			manifest: &registry.Manifest{
				Spec: registry.PodSpec{
					Command: []string{"python3", "main.py"},
				},
			},
			want: "python:3.12",
		},
		{
			name: "from uv command",
			manifest: &registry.Manifest{
				Spec: registry.PodSpec{
					Command: []string{"uv", "run", "main.py"},
				},
			},
			want: "python:3.12",
		},
		{
			name: "from go command",
			manifest: &registry.Manifest{
				Spec: registry.PodSpec{
					Command: []string{"go", "run", "."},
				},
			},
			want: "go:1.22",
		},
		{
			name: "from node command",
			manifest: &registry.Manifest{
				Spec: registry.PodSpec{
					Command: []string{"node", "index.js"},
				},
			},
			want: "node:20",
		},
		{
			name: "from npm command",
			manifest: &registry.Manifest{
				Spec: registry.PodSpec{
					Command: []string{"npm", "start"},
				},
			},
			want: "node:20",
		},
		{
			name: "from npx command",
			manifest: &registry.Manifest{
				Spec: registry.PodSpec{
					Command: []string{"npx", "ts-node", "index.ts"},
				},
			},
			want: "node:20",
		},
		{
			name: "from language label - python",
			manifest: &registry.Manifest{
				Metadata: registry.Metadata{
					Labels: map[string]string{"language": "python"},
				},
			},
			want: "python:3.12",
		},
		{
			name: "from language label - go",
			manifest: &registry.Manifest{
				Metadata: registry.Metadata{
					Labels: map[string]string{"language": "go"},
				},
			},
			want: "go:1.22",
		},
		{
			name: "from language label - golang",
			manifest: &registry.Manifest{
				Metadata: registry.Metadata{
					Labels: map[string]string{"language": "golang"},
				},
			},
			want: "go:1.22",
		},
		{
			name: "from language label - node",
			manifest: &registry.Manifest{
				Metadata: registry.Metadata{
					Labels: map[string]string{"language": "node"},
				},
			},
			want: "node:20",
		},
		{
			name: "from language label - javascript",
			manifest: &registry.Manifest{
				Metadata: registry.Metadata{
					Labels: map[string]string{"language": "javascript"},
				},
			},
			want: "node:20",
		},
		{
			name: "from language label - typescript",
			manifest: &registry.Manifest{
				Metadata: registry.Metadata{
					Labels: map[string]string{"language": "typescript"},
				},
			},
			want: "node:20",
		},
		{
			name: "unknown command",
			manifest: &registry.Manifest{
				Spec: registry.PodSpec{
					Command: []string{"./custom-binary"},
				},
			},
			want: "",
		},
		{
			name: "empty manifest",
			manifest: &registry.Manifest{},
			want:     "",
		},
		{
			name: "build.runtime takes precedence",
			manifest: &registry.Manifest{
				Spec: registry.PodSpec{
					Build:   &registry.BuildSpec{Runtime: "python:3.10"},
					Runtime: "go:1.21", // Should be ignored
					Command: []string{"node", "index.js"},
				},
			},
			want: "python:3.10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectRuntime(tt.manifest)
			if got != tt.want {
				t.Errorf("detectRuntime() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatCommand(t *testing.T) {
	tests := []struct {
		name string
		cmd  []string
		want string
	}{
		{
			name: "empty",
			cmd:  nil,
			want: `["node", "index.js"]`,
		},
		{
			name: "single",
			cmd:  []string{"./server"},
			want: `["./server"]`,
		},
		{
			name: "multiple",
			cmd:  []string{"python", "-m", "main"},
			want: `["python", "-m", "main"]`,
		},
		{
			name: "with spaces",
			cmd:  []string{"echo", "hello world"},
			want: `["echo", "hello world"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("formatCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSupportedRuntimes(t *testing.T) {
	runtimes := SupportedRuntimes()

	if len(runtimes) != 3 {
		t.Errorf("SupportedRuntimes() returned %d runtimes, want 3", len(runtimes))
	}

	// Check all expected runtimes are present
	names := make(map[string]bool)
	for _, r := range runtimes {
		names[r.Name] = true

		// Verify each runtime has required fields
		if r.BaseImage == "" {
			t.Errorf("Runtime %q missing BaseImage", r.Name)
		}
		if len(r.DefaultFiles) == 0 {
			t.Errorf("Runtime %q missing DefaultFiles", r.Name)
		}
		if len(r.StartCommand) == 0 {
			t.Errorf("Runtime %q missing StartCommand", r.Name)
		}
	}

	expected := []string{"python", "go", "node"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("Missing runtime: %s", name)
		}
	}
}

func TestDockerfileGenerator_Generate_VersionExtraction(t *testing.T) {
	tests := []struct {
		name    string
		runtime string
		want    string // substring that should be in dockerfile
	}{
		{"python version", "python:3.11", "python:3.11-slim"},
		{"go version", "go:1.21", "golang:1.21-alpine"},
		{"node version", "node:18", "node:18-alpine"},
	}

	g := NewDockerfileGenerator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := &registry.Manifest{
				Metadata: registry.Metadata{Name: "test"},
				Spec: registry.PodSpec{
					Command: []string{"echo"},
					Build:   &registry.BuildSpec{Runtime: tt.runtime},
				},
			}

			dockerfile, err := g.Generate(manifest)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			if !strings.Contains(dockerfile, tt.want) {
				t.Errorf("Dockerfile should contain %q", tt.want)
			}
		})
	}
}

func TestDockerfileGenerator_Generate_DefaultVersion(t *testing.T) {
	tests := []struct {
		name    string
		runtime string
		want    string
	}{
		{"python without version", "python", "python:3.12-slim"},
		{"go without version", "go", "golang:1.22-alpine"},
		{"node without version", "node", "node:20-alpine"},
	}

	g := NewDockerfileGenerator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := &registry.Manifest{
				Metadata: registry.Metadata{Name: "test"},
				Spec: registry.PodSpec{
					Command: []string{"echo"},
					Build:   &registry.BuildSpec{Runtime: tt.runtime},
				},
			}

			dockerfile, err := g.Generate(manifest)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			if !strings.Contains(dockerfile, tt.want) {
				t.Errorf("Dockerfile should contain %q, got:\n%s", tt.want, dockerfile)
			}
		})
	}
}

func TestDockerfileGenerator_Generate_HeaderComment(t *testing.T) {
	g := NewDockerfileGenerator()

	manifest := &registry.Manifest{
		Metadata: registry.Metadata{Name: "my-cool-tool"},
		Spec: registry.PodSpec{
			Command: []string{"python", "main.py"},
			Build:   &registry.BuildSpec{Runtime: "python:3.12"},
		},
	}

	dockerfile, err := g.Generate(manifest)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if !strings.Contains(dockerfile, "# Auto-generated Dockerfile for my-cool-tool") {
		t.Error("Dockerfile should have header comment with tool name")
	}
}

func TestDockerfileGenerator_Generate_ResourcesTimeout(t *testing.T) {
	g := NewDockerfileGenerator()

	manifest := &registry.Manifest{
		Metadata: registry.Metadata{Name: "timeout-tool"},
		Spec: registry.PodSpec{
			Command: []string{"python", "main.py"},
			Build:   &registry.BuildSpec{Runtime: "python:3.12"},
			Resources: &registry.ResourceSpec{
				Timeout: registry.Duration{Duration: 5 * time.Minute},
			},
		},
	}

	// Should not error with timeout only
	_, err := g.Generate(manifest)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
}
