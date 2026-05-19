package build

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/internal/config"
)

func TestDockerfileBuilderBasic(t *testing.T) {
	builder := NewDockerfileBuilder()
	builder.From("golang:1.22")

	result := builder.String()
	assert.Contains(t, result, "FROM golang:1.22")
}

func TestDockerfileBuilderWithWorkdir(t *testing.T) {
	builder := NewDockerfileBuilder()
	builder.From("golang:1.22").Workdir("/workspace")

	result := builder.String()
	assert.Contains(t, result, "FROM golang:1.22")
	assert.Contains(t, result, "WORKDIR /workspace")
}

func TestDockerfileBuilderWithRun(t *testing.T) {
	builder := NewDockerfileBuilder()
	builder.From("ubuntu:22.04").
		Run("apt-get update").
		Run("apt-get install -y git")

	result := builder.String()
	assert.Contains(t, result, "RUN apt-get update")
	assert.Contains(t, result, "RUN apt-get install -y git")
}

func TestDockerfileBuilderWithEnv(t *testing.T) {
	builder := NewDockerfileBuilder()
	builder.From("golang:1.22").
		Env("GOPATH", "/go").
		Env("PATH", "/go/bin:$PATH")

	result := builder.String()
	assert.Contains(t, result, `ENV GOPATH="/go"`)
	assert.Contains(t, result, `ENV PATH="/go/bin:$PATH"`)
}

func TestDockerfileBuilderWithLabel(t *testing.T) {
	builder := NewDockerfileBuilder()
	builder.From("golang:1.22").
		Label("drun.version", "1.0.0").
		Label("drun.project", "myproject")

	result := builder.String()
	assert.Contains(t, result, `LABEL drun.version="1.0.0"`)
	assert.Contains(t, result, `LABEL drun.project="myproject"`)
}

func TestDockerfileBuilderWithCopy(t *testing.T) {
	builder := NewDockerfileBuilder()
	builder.From("golang:1.22").
		Copy(".", "/app").
		Copy("go.mod", "/app/go.mod")

	result := builder.String()
	assert.Contains(t, result, "COPY . /app")
	assert.Contains(t, result, "COPY go.mod /app/go.mod")
}

func TestDockerfileBuilderWithUser(t *testing.T) {
	builder := NewDockerfileBuilder()
	builder.From("golang:1.22").
		User("1000:1000")

	result := builder.String()
	assert.Contains(t, result, "USER 1000:1000")
}

func TestDockerfileBuilderWithEntrypoint(t *testing.T) {
	builder := NewDockerfileBuilder()
	builder.From("golang:1.22").
		Entrypoint("/tini", "--")

	result := builder.String()
	assert.Contains(t, result, `ENTRYPOINT ["/tini", "--"]`)
}

func TestDockerfileBuilderWithCmd(t *testing.T) {
	builder := NewDockerfileBuilder()
	builder.From("golang:1.22").
		Cmd("/bin/bash")

	result := builder.String()
	assert.Contains(t, result, `CMD ["/bin/bash"]`)
}

func TestDockerfileBuilderChaining(t *testing.T) {
	builder := NewDockerfileBuilder()
	result := builder.
		From("golang:1.22").
		Workdir("/workspace").
		Env("CGO_ENABLED", "0").
		Run("go mod download").
		Copy(".", ".").
		Cmd("go", "run", ".").
		String()

	lines := strings.Split(result, "\n")
	require.GreaterOrEqual(t, len(lines), 5)
	assert.True(t, strings.HasPrefix(lines[0], "FROM"))
}

func TestDockerfileBuilderComment(t *testing.T) {
	builder := NewDockerfileBuilder()
	builder.From("golang:1.22").
		Comment("Install dependencies").
		Run("apt-get update")

	result := builder.String()
	assert.Contains(t, result, "# Install dependencies")
}

func TestDockerfileBuilderBlankLine(t *testing.T) {
	builder := NewDockerfileBuilder()
	builder.From("golang:1.22").
		BlankLine().
		Workdir("/app")

	result := builder.String()
	assert.Contains(t, result, "\n\n")
}

func TestGenerateDockerfileBasic(t *testing.T) {
	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
	}

	dockerfile, err := GenerateDockerfile(cfg)
	require.NoError(t, err)

	assert.Contains(t, dockerfile, "FROM golang:1.22")
	assert.Contains(t, dockerfile, "WORKDIR /workspace")
}

func TestGenerateDockerfileWithPackages(t *testing.T) {
	cfg := &config.Config{
		Image:   "ubuntu:22.04",
		Workdir: "/workspace",
		Setup: &config.SetupConfig{
			Packages: []string{"git", "curl", "make"},
		},
	}

	dockerfile, err := GenerateDockerfile(cfg)
	require.NoError(t, err)

	assert.Contains(t, dockerfile, "FROM ubuntu:22.04")
	assert.Contains(t, dockerfile, "apt-get update")
	assert.Contains(t, dockerfile, "apt-get install")
	assert.Contains(t, dockerfile, "git curl make")
}

func TestGenerateDockerfileWithRun(t *testing.T) {
	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
		Setup: &config.SetupConfig{
			Run: []string{
				"go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest",
				"go install github.com/cosmtrek/air@latest",
			},
		},
	}

	dockerfile, err := GenerateDockerfile(cfg)
	require.NoError(t, err)

	assert.Contains(t, dockerfile, "golangci-lint")
	assert.Contains(t, dockerfile, "air")
}

func TestGenerateDockerfileWithTini(t *testing.T) {
	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
		Process: &config.ProcessConfig{
			Init: true,
		},
	}

	dockerfile, err := GenerateDockerfile(cfg)
	require.NoError(t, err)

	// Should install tini for apt-based images
	assert.Contains(t, dockerfile, "tini")
	assert.Contains(t, dockerfile, `ENTRYPOINT ["/tini", "--"]`)
}

func TestGenerateDockerfileWithTiniAlpine(t *testing.T) {
	cfg := &config.Config{
		Image:   "alpine:3.19",
		Workdir: "/workspace",
		Process: &config.ProcessConfig{
			Init: true,
		},
	}

	dockerfile, err := GenerateDockerfile(cfg)
	require.NoError(t, err)

	// Should install tini-static for alpine
	assert.Contains(t, dockerfile, "tini-static")
	assert.Contains(t, dockerfile, `ENTRYPOINT ["/sbin/tini", "--"]`)
}

func TestGenerateDockerfileFromInline(t *testing.T) {
	inlineDockerfile := `FROM golang:1.22
RUN apt-get update && apt-get install -y protobuf-compiler
WORKDIR /workspace`

	cfg := &config.Config{
		DockerfileInline: inlineDockerfile,
	}

	dockerfile, err := GenerateDockerfile(cfg)
	require.NoError(t, err)

	assert.Equal(t, inlineDockerfile, dockerfile)
}

func TestGenerateDockerfileWithEnv(t *testing.T) {
	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
		Env: []string{
			"CGO_ENABLED=0",
			"GOPATH=/go",
		},
	}

	dockerfile, err := GenerateDockerfile(cfg)
	require.NoError(t, err)

	assert.Contains(t, dockerfile, `ENV CGO_ENABLED="0"`)
	assert.Contains(t, dockerfile, `ENV GOPATH="/go"`)
}

func TestGenerateDockerfileUnknownPackageManager(t *testing.T) {
	cfg := &config.Config{
		Image:   "scratch",
		Workdir: "/",
		Setup: &config.SetupConfig{
			Packages: []string{"git"},
		},
	}

	_, err := GenerateDockerfile(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot install packages")
}

func TestGenerateDockerfileWithLabels(t *testing.T) {
	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
	}

	dockerfile, err := GenerateDockerfile(cfg)
	require.NoError(t, err)

	// Should include drun labels
	assert.Contains(t, dockerfile, "LABEL")
	assert.Contains(t, dockerfile, "drun.managed")
}

func TestGenerateDockerfileWithSteps(t *testing.T) {
	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
		Setup: &config.SetupConfig{
			Steps: []config.SetupStep{
				{Name: "tools", Run: "go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest"},
				{Name: "codegen", Run: "go generate ./...", DependsOn: []string{"tools"}},
			},
		},
	}

	dockerfile, err := GenerateDockerfile(cfg)
	require.NoError(t, err)

	// Steps should be included in order (tools before codegen)
	toolsIdx := strings.Index(dockerfile, "sqlc@latest")
	codegenIdx := strings.Index(dockerfile, "go generate")
	assert.Less(t, toolsIdx, codegenIdx)
}

func TestDockerfileBuilderRunCombined(t *testing.T) {
	builder := NewDockerfileBuilder()
	builder.From("ubuntu:22.04").
		RunCombined([]string{
			"apt-get update",
			"apt-get install -y git",
			"rm -rf /var/lib/apt/lists/*",
		})

	result := builder.String()
	// Should combine commands with && for layer optimization
	assert.Contains(t, result, "apt-get update &&")
	assert.Contains(t, result, "apt-get install -y git")
	assert.Contains(t, result, "rm -rf /var/lib/apt/lists/*")
	// Should only have one RUN instruction
	assert.Equal(t, 1, strings.Count(result, "RUN "))
}

func TestDockerfileBuilderWithShell(t *testing.T) {
	builder := NewDockerfileBuilder()
	builder.From("ubuntu:22.04").
		Shell("/bin/bash", "-c")

	result := builder.String()
	assert.Contains(t, result, `SHELL ["/bin/bash", "-c"]`)
}

func TestDockerfileBuilderWithArg(t *testing.T) {
	builder := NewDockerfileBuilder()
	builder.From("golang:1.22").
		Arg("VERSION", "1.0.0").
		Arg("BUILD_DATE", "")

	result := builder.String()
	assert.Contains(t, result, `ARG VERSION="1.0.0"`)
	assert.Contains(t, result, "ARG BUILD_DATE")
}
