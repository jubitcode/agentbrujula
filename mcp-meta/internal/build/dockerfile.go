// Package build provides container image building functionality.
package build

import (
	"fmt"
	"path/filepath"
	"strings"

	"mcp-meta/internal/registry"
)

// DockerfileGenerator generates Dockerfiles for tool projects.
type DockerfileGenerator struct{}

// NewDockerfileGenerator creates a new generator.
func NewDockerfileGenerator() *DockerfileGenerator {
	return &DockerfileGenerator{}
}

// Generate creates a Dockerfile for the given manifest.
func (g *DockerfileGenerator) Generate(manifest *registry.Manifest) (string, error) {
	// If a custom Dockerfile is specified, don't generate
	if manifest.Spec.Build != nil && manifest.Spec.Build.Dockerfile != "" {
		return "", fmt.Errorf("custom Dockerfile specified, not generating")
	}

	// If a pre-built image is specified, don't generate
	if manifest.Spec.Image != "" {
		return "", fmt.Errorf("pre-built image specified, not generating")
	}

	// Determine runtime
	runtime := detectRuntime(manifest)
	if runtime == "" {
		return "", fmt.Errorf("cannot determine runtime from manifest")
	}

	// Generate based on runtime
	switch {
	case strings.HasPrefix(runtime, "python"):
		return g.generatePythonDockerfile(manifest, runtime)
	case strings.HasPrefix(runtime, "go"):
		return g.generateGoDockerfile(manifest, runtime)
	case strings.HasPrefix(runtime, "node"):
		return g.generateNodeDockerfile(manifest, runtime)
	default:
		return "", fmt.Errorf("unsupported runtime: %s", runtime)
	}
}

// detectRuntime determines the runtime from manifest configuration.
func detectRuntime(m *registry.Manifest) string {
	// First check build.runtime
	if m.Spec.Build != nil && m.Spec.Build.Runtime != "" {
		return m.Spec.Build.Runtime
	}

	// Then check spec.runtime (legacy)
	if m.Spec.Runtime != "" {
		return m.Spec.Runtime
	}

	// Try to detect from command
	if len(m.Spec.Command) > 0 {
		cmd := filepath.Base(m.Spec.Command[0])
		switch cmd {
		case "python", "python3", "uv":
			return "python:3.12"
		case "go":
			return "go:1.22"
		case "node", "npm", "npx":
			return "node:20"
		}
	}

	// Check labels
	if lang, ok := m.Metadata.Labels["language"]; ok {
		switch lang {
		case "python":
			return "python:3.12"
		case "go", "golang":
			return "go:1.22"
		case "node", "javascript", "typescript":
			return "node:20"
		}
	}

	return ""
}

func (g *DockerfileGenerator) generatePythonDockerfile(m *registry.Manifest, runtime string) (string, error) {
	// Extract Python version from runtime (e.g., "python:3.12" -> "3.12")
	pythonVersion := "3.12"
	if parts := strings.Split(runtime, ":"); len(parts) == 2 {
		pythonVersion = parts[1]
	}

	// Check if using uv (recommended)
	useUV := false
	if len(m.Spec.Command) > 0 && m.Spec.Command[0] == "uv" {
		useUV = true
	}

	var dockerfile strings.Builder

	dockerfile.WriteString(fmt.Sprintf(`# Auto-generated Dockerfile for %s
# Runtime: Python %s

`, m.Metadata.Name, pythonVersion))

	if useUV {
		dockerfile.WriteString(fmt.Sprintf(`FROM ghcr.io/astral-sh/uv:python%s-bookworm-slim

# Set working directory
WORKDIR /app

# Copy dependency files first for caching
COPY pyproject.toml ./
COPY uv.lock* ./

# Install dependencies
RUN uv sync --frozen --no-dev

# Copy application code
COPY . .

`, pythonVersion))
	} else {
		dockerfile.WriteString(fmt.Sprintf(`FROM python:%s-slim

# Set working directory
WORKDIR /app

# Install dependencies
COPY requirements.txt* pyproject.toml* ./
RUN pip install --no-cache-dir -r requirements.txt 2>/dev/null || \
    pip install --no-cache-dir . 2>/dev/null || \
    echo "No dependencies to install"

# Copy application code
COPY . .

`, pythonVersion))
	}

	// Add resource limits if specified
	if m.Spec.Resources != nil {
		dockerfile.WriteString("# Resource limits configured in manifest\n")
		if m.Spec.Resources.Memory != "" {
			dockerfile.WriteString(fmt.Sprintf("# Memory: %s\n", m.Spec.Resources.Memory))
		}
		if m.Spec.Resources.CPU != "" {
			dockerfile.WriteString(fmt.Sprintf("# CPU: %s\n", m.Spec.Resources.CPU))
		}
		dockerfile.WriteString("\n")
	}

	// Set entrypoint
	cmd := formatCommand(m.Spec.Command)
	dockerfile.WriteString(fmt.Sprintf(`# Entrypoint
CMD %s
`, cmd))

	return dockerfile.String(), nil
}

func (g *DockerfileGenerator) generateGoDockerfile(m *registry.Manifest, runtime string) (string, error) {
	// Extract Go version from runtime (e.g., "go:1.22" -> "1.22")
	goVersion := "1.22"
	if parts := strings.Split(runtime, ":"); len(parts) == 2 {
		goVersion = parts[1]
	}

	var dockerfile strings.Builder

	dockerfile.WriteString(fmt.Sprintf(`# Auto-generated Dockerfile for %s
# Runtime: Go %s

# Build stage
FROM golang:%s-alpine AS builder

WORKDIR /app

# Copy dependency files first for caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/server .

# Runtime stage
FROM alpine:latest

# Add ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/server .

`, m.Metadata.Name, goVersion, goVersion))

	// Add resource limits comment
	if m.Spec.Resources != nil {
		dockerfile.WriteString("# Resource limits configured in manifest\n")
		if m.Spec.Resources.Memory != "" {
			dockerfile.WriteString(fmt.Sprintf("# Memory: %s\n", m.Spec.Resources.Memory))
		}
		if m.Spec.Resources.CPU != "" {
			dockerfile.WriteString(fmt.Sprintf("# CPU: %s\n", m.Spec.Resources.CPU))
		}
		dockerfile.WriteString("\n")
	}

	dockerfile.WriteString(`# Entrypoint
CMD ["./server"]
`)

	return dockerfile.String(), nil
}

func (g *DockerfileGenerator) generateNodeDockerfile(m *registry.Manifest, runtime string) (string, error) {
	// Extract Node version from runtime (e.g., "node:20" -> "20")
	nodeVersion := "20"
	if parts := strings.Split(runtime, ":"); len(parts) == 2 {
		nodeVersion = parts[1]
	}

	var dockerfile strings.Builder

	dockerfile.WriteString(fmt.Sprintf(`# Auto-generated Dockerfile for %s
# Runtime: Node.js %s

FROM node:%s-alpine

WORKDIR /app

# Copy dependency files first for caching
COPY package*.json ./

# Install dependencies
RUN npm ci --only=production

# Copy application code
COPY . .

`, m.Metadata.Name, nodeVersion, nodeVersion))

	// Add resource limits comment
	if m.Spec.Resources != nil {
		dockerfile.WriteString("# Resource limits configured in manifest\n")
		if m.Spec.Resources.Memory != "" {
			dockerfile.WriteString(fmt.Sprintf("# Memory: %s\n", m.Spec.Resources.Memory))
		}
		if m.Spec.Resources.CPU != "" {
			dockerfile.WriteString(fmt.Sprintf("# CPU: %s\n", m.Spec.Resources.CPU))
		}
		dockerfile.WriteString("\n")
	}

	// Set entrypoint
	cmd := formatCommand(m.Spec.Command)
	dockerfile.WriteString(fmt.Sprintf(`# Entrypoint
CMD %s
`, cmd))

	return dockerfile.String(), nil
}

// formatCommand formats a command slice as a JSON array string.
func formatCommand(cmd []string) string {
	if len(cmd) == 0 {
		return `["node", "index.js"]`
	}

	// Format as JSON array
	parts := make([]string, len(cmd))
	for i, part := range cmd {
		parts[i] = fmt.Sprintf(`"%s"`, part)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// RuntimeInfo provides information about supported runtimes.
type RuntimeInfo struct {
	Name          string
	BaseImage     string
	DefaultFiles  []string
	BuildCommand  string
	StartCommand  []string
}

// SupportedRuntimes returns information about all supported runtimes.
func SupportedRuntimes() []RuntimeInfo {
	return []RuntimeInfo{
		{
			Name:         "python",
			BaseImage:    "python:3.12-slim",
			DefaultFiles: []string{"main.py", "pyproject.toml"},
			StartCommand: []string{"python", "main.py"},
		},
		{
			Name:          "go",
			BaseImage:     "golang:1.22-alpine",
			DefaultFiles:  []string{"main.go", "go.mod"},
			BuildCommand:  "go build -o server .",
			StartCommand:  []string{"./server"},
		},
		{
			Name:         "node",
			BaseImage:    "node:20-alpine",
			DefaultFiles: []string{"index.js", "package.json"},
			StartCommand: []string{"node", "index.js"},
		},
	}
}
