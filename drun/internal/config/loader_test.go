package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/internal/testutil"
)

func TestLoadFromFile(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, ".drun.yaml", `
image: "alpine:latest"
workdir: "/app"
`)

	loader := NewLoader(tmpDir)
	cfg, err := loader.Load()

	require.NoError(t, err)
	assert.Equal(t, "alpine:latest", cfg.Image)
	assert.Equal(t, "/app", cfg.Workdir)
}

func TestLoadFromFileYml(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, ".drun.yml", `
image: "alpine:latest"
`)

	loader := NewLoader(tmpDir)
	cfg, err := loader.Load()

	require.NoError(t, err)
	assert.Equal(t, "alpine:latest", cfg.Image)
}

func TestLoadWithPreset(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "go.mod", "module test\n\ngo 1.22\n")

	loader := NewLoader(tmpDir)
	cfg, err := loader.Load()

	require.NoError(t, err)
	assert.Equal(t, "golang:1.22", cfg.Image)
	assert.Equal(t, "go", cfg.PresetName)
}

func TestLoadWithEnvSubstitution(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, ".drun.yaml", `
image: "golang:1.22"
env:
  - "TEST_VAR=${TEST_VALUE}"
  - "HOME_PATH=${HOME}/path"
`)
	testutil.SetEnv(t, "TEST_VALUE", "hello")

	loader := NewLoader(tmpDir)
	cfg, err := loader.Load()

	require.NoError(t, err)
	assert.Contains(t, cfg.Env, "TEST_VAR=hello")

	home, _ := os.UserHomeDir()
	assert.Contains(t, cfg.Env, "HOME_PATH="+home+"/path")
}

func TestLoadPriority(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	// Create go.mod for preset detection
	testutil.TempFileInDir(t, tmpDir, "go.mod", "module test")
	// Create config file that overrides preset image
	testutil.TempFileInDir(t, tmpDir, ".drun.yaml", `
image: "golang:1.21"
`)

	loader := NewLoader(tmpDir)
	cfg, err := loader.Load()

	require.NoError(t, err)
	// Project config should override preset
	assert.Equal(t, "golang:1.21", cfg.Image)
}

func TestLoadMissing(t *testing.T) {
	tmpDir := testutil.TempDir(t)

	loader := NewLoader(tmpDir)
	cfg, err := loader.Load()

	require.NoError(t, err)
	assert.NotNil(t, cfg)
	// Should have defaults
	assert.Equal(t, "/workspace", cfg.Workdir)
}

func TestLoadInvalidYAML(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, ".drun.yaml", `
image: [invalid yaml
`)

	loader := NewLoader(tmpDir)
	_, err := loader.Load()

	assert.Error(t, err)
}

func TestLoadWithConfigPath(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	configPath := testutil.TempFileInDir(t, tmpDir, "custom.yaml", `
image: "custom:latest"
`)

	loader := NewLoader(tmpDir, WithConfigPath(configPath))
	cfg, err := loader.Load()

	require.NoError(t, err)
	assert.Equal(t, "custom:latest", cfg.Image)
}

func TestLoadEnvSubstitutionInMounts(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, ".drun.yaml", `
image: "alpine:latest"
mounts:
  - source: "${HOME}/.cache"
    target: "/cache"
`)

	loader := NewLoader(tmpDir)
	cfg, err := loader.Load()

	require.NoError(t, err)
	home, _ := os.UserHomeDir()
	assert.Equal(t, home+"/.cache", cfg.Mounts[0].Source)
}

func TestLoadEnvSubstitutionInImage(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, ".drun.yaml", `
image: "golang:${GO_VERSION}"
`)
	testutil.SetEnv(t, "GO_VERSION", "1.22")

	loader := NewLoader(tmpDir)
	cfg, err := loader.Load()

	require.NoError(t, err)
	assert.Equal(t, "golang:1.22", cfg.Image)
}

func TestLoadUnexpandedEnvVars(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, ".drun.yaml", `
image: "alpine:latest"
env:
  - "UNDEFINED=${UNDEFINED_VAR}"
`)
	testutil.UnsetEnv(t, "UNDEFINED_VAR")

	loader := NewLoader(tmpDir)
	cfg, err := loader.Load()

	require.NoError(t, err)
	// Undefined vars remain as-is
	assert.Contains(t, cfg.Env, "UNDEFINED=${UNDEFINED_VAR}")
}

func TestLoadMergesPresetAndProject(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "go.mod", "module test")
	testutil.TempFileInDir(t, tmpDir, ".drun.yaml", `
ports:
  - "8080:8080"
env:
  - "DEBUG=true"
`)

	loader := NewLoader(tmpDir)
	cfg, err := loader.Load()

	require.NoError(t, err)
	// Should have preset image
	assert.Equal(t, "golang:1.22", cfg.Image)
	// Should have project ports
	assert.Contains(t, cfg.Ports, "8080:8080")
	// Should have merged env (preset + project)
	assert.Contains(t, cfg.Env, "DEBUG=true")
}

func TestLoadProjectDir(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, ".drun.yaml", `
image: "alpine:latest"
`)

	loader := NewLoader(tmpDir)
	cfg, err := loader.Load()

	require.NoError(t, err)
	assert.Equal(t, tmpDir, cfg.ProjectDir)
}
