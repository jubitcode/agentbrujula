package deploy

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mcp-meta/internal/registry"
)

// Builder generates container images from agent source code + manifest.
type Builder struct {
	logger *slog.Logger
}

// NewBuilder creates a builder.
func NewBuilder(logger *slog.Logger) *Builder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Builder{logger: logger}
}

// BuildResult holds the outcome of a build step.
type BuildResult struct {
	ImageTag   string
	Dockerfile string // generated Dockerfile content
	BuildLog   string
}

// Build generates a Dockerfile from the manifest language field, then runs docker build.
func (b *Builder) Build(sourceDir string, spec *registry.PodSpec, imageTag string) (*BuildResult, error) {
	dockerfile := generateDockerfile(spec)

	// Write Dockerfile to source dir
	dockerfilePath := filepath.Join(sourceDir, "Dockerfile.mcp-meta")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return nil, fmt.Errorf("write dockerfile: %w", err)
	}

	b.logger.Info("building container image",
		"image", imageTag,
		"language", spec.Language,
		"source", sourceDir,
	)

	// Run docker build
	cmd := exec.Command("docker", "build",
		"-t", imageTag,
		"-f", dockerfilePath,
		sourceDir,
	)

	output, err := cmd.CombinedOutput()
	buildLog := string(output)

	if err != nil {
		return &BuildResult{Dockerfile: dockerfile, BuildLog: buildLog},
			fmt.Errorf("docker build failed: %w\n%s", err, buildLog)
	}

	b.logger.Info("docker build complete", "image", imageTag)

	// For Python: also create a local venv for subprocess execution
	if spec.Language == "python" {
		b.logger.Info("creating local venv", "source", sourceDir)
		venvLog, venvErr := b.createPythonVenv(sourceDir)
		buildLog += "\n--- venv ---\n" + venvLog
		if venvErr != nil {
			b.logger.Warn("venv creation failed (will use system python)", "error", venvErr)
		}
	}

	return &BuildResult{
		ImageTag:   imageTag,
		Dockerfile: dockerfile,
		BuildLog:   buildLog,
	}, nil
}

func (b *Builder) createPythonVenv(sourceDir string) (string, error) {
	// Create venv
	cmd := exec.Command("python3", "-m", "venv", filepath.Join(sourceDir, ".venv"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("create venv: %w", err)
	}

	// Install requirements if present
	reqPath := filepath.Join(sourceDir, "requirements.txt")
	if _, err := os.Stat(reqPath); err == nil {
		pip := filepath.Join(sourceDir, ".venv", "bin", "pip")
		cmd = exec.Command(pip, "install", "--no-cache-dir", "-r", reqPath)
		pipOutput, err := cmd.CombinedOutput()
		output = append(output, pipOutput...)
		if err != nil {
			return string(output), fmt.Errorf("pip install: %w", err)
		}
	}

	return string(output), nil
}

func generateDockerfile(spec *registry.PodSpec) string {
	switch spec.Language {
	case "python":
		return generatePythonDockerfile(spec)
	case "go":
		return generateGoDockerfile(spec)
	case "node":
		return generateNodeDockerfile(spec)
	case "rust":
		return generateRustDockerfile(spec)
	default:
		// custom — user must provide their own Dockerfile
		return ""
	}
}

func generatePythonDockerfile(spec *registry.PodSpec) string {
	var sb strings.Builder
	sb.WriteString("FROM python:3.13-slim\n")
	sb.WriteString("WORKDIR /app\n")
	sb.WriteString("COPY requirements.txt* ./\n")
	sb.WriteString("RUN pip install --no-cache-dir -r requirements.txt 2>/dev/null || true\n")
	sb.WriteString("COPY pyproject.toml* ./\n")
	sb.WriteString("RUN pip install --no-cache-dir . 2>/dev/null || true\n")
	sb.WriteString("COPY . .\n")

	// Set environment variables from manifest
	for k, v := range spec.Env {
		sb.WriteString(fmt.Sprintf("ENV %s=%s\n", k, v))
	}

	// Entrypoint: sleep infinity (drun manages execution via docker exec)
	sb.WriteString("CMD [\"sleep\", \"infinity\"]\n")
	return sb.String()
}

func generateGoDockerfile(spec *registry.PodSpec) string {
	var sb strings.Builder
	sb.WriteString("FROM golang:1.25 AS builder\n")
	sb.WriteString("WORKDIR /app\n")
	sb.WriteString("COPY go.mod go.sum* ./\n")
	sb.WriteString("RUN go mod download 2>/dev/null || true\n")
	sb.WriteString("COPY . .\n")
	sb.WriteString("RUN CGO_ENABLED=0 go build -o agent .\n")
	sb.WriteString("\n")
	sb.WriteString("FROM alpine:latest\n")
	sb.WriteString("WORKDIR /app\n")
	sb.WriteString("COPY --from=builder /app/agent .\n")

	for k, v := range spec.Env {
		sb.WriteString(fmt.Sprintf("ENV %s=%s\n", k, v))
	}

	sb.WriteString("CMD [\"sleep\", \"infinity\"]\n")
	return sb.String()
}

func generateNodeDockerfile(spec *registry.PodSpec) string {
	var sb strings.Builder
	sb.WriteString("FROM node:22-slim\n")
	sb.WriteString("WORKDIR /app\n")
	sb.WriteString("COPY package*.json ./\n")
	sb.WriteString("RUN npm install --production 2>/dev/null || true\n")
	sb.WriteString("COPY . .\n")

	for k, v := range spec.Env {
		sb.WriteString(fmt.Sprintf("ENV %s=%s\n", k, v))
	}

	sb.WriteString("CMD [\"sleep\", \"infinity\"]\n")
	return sb.String()
}

func generateRustDockerfile(spec *registry.PodSpec) string {
	var sb strings.Builder
	sb.WriteString("FROM rust:1.85-slim AS builder\n")
	sb.WriteString("WORKDIR /app\n")
	sb.WriteString("COPY Cargo.toml Cargo.lock* ./\n")
	sb.WriteString("COPY src/ src/\n")
	sb.WriteString("RUN cargo build --release\n")
	sb.WriteString("\n")
	sb.WriteString("FROM debian:bookworm-slim\n")
	sb.WriteString("WORKDIR /app\n")
	sb.WriteString("COPY --from=builder /app/target/release/agent .\n")

	for k, v := range spec.Env {
		sb.WriteString(fmt.Sprintf("ENV %s=%s\n", k, v))
	}

	sb.WriteString("CMD [\"sleep\", \"infinity\"]\n")
	return sb.String()
}
