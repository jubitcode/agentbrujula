package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"drun/test/testutil"
)

func TestPreset_DetectsGo(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Go preset should use golang image and have go available
	out := testutil.RunDrun(t, dir, "run", "go", "version")

	assert.Contains(t, out, "go version")
}

func TestPreset_DetectsPython(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "python")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Python preset should have python available
	out := testutil.RunDrun(t, dir, "run", "python", "--version")

	assert.Contains(t, out, "Python")
}

func TestPreset_DetectsNode(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "node")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Node preset should have node available
	out := testutil.RunDrun(t, dir, "run", "node", "--version")

	assert.Regexp(t, `v\d+\.\d+`, out)
}

func TestPreset_DetectsRust(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "rust")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Rust preset should have rustc available
	out := testutil.RunDrun(t, dir, "run", "rustc", "--version")

	assert.Contains(t, out, "rustc")
}

func TestPreset_ExplicitOverridesDetection(t *testing.T) {
	testutil.RequireDocker(t)

	// Create a Go project but provide a .drun.yaml that specifies Python
	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Add a .drun.yaml that explicitly requests Python
	drunConfig := `
image: "python:3.12"
workdir: "/workspace"
mounts:
  - source: "."
    target: "/workspace"
`
	err := os.WriteFile(filepath.Join(dir, ".drun.yaml"), []byte(drunConfig), 0644)
	if err != nil {
		t.Fatalf("failed to write .drun.yaml: %v", err)
	}

	// Should use Python despite having go.mod
	out := testutil.RunDrun(t, dir, "run", "python", "--version")

	assert.Contains(t, out, "Python")
}

func TestPreset_FailsForUnknownProject(t *testing.T) {
	testutil.RequireDocker(t)

	// Create an empty directory with no language markers
	dir, err := os.MkdirTemp("", "drun-unknown-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// Should fail because no project type can be detected
	out := testutil.RunDrunExpectError(t, dir, "run", "echo", "hi")

	// Should contain an error about config or detection
	assert.True(t, len(out) > 0, "expected error output")
}

func TestPreset_GoEnvironmentDefaults(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Go preset should set GOPROXY environment variable
	out := testutil.RunDrun(t, dir, "run", "sh", "-c", "echo $GOPROXY")

	assert.Contains(t, out, "proxy.golang.org")
}

func TestPreset_NodeEnvironmentDefaults(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "node")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Node preset should set NODE_ENV environment variable
	out := testutil.RunDrun(t, dir, "run", "sh", "-c", "echo $NODE_ENV")

	assert.Contains(t, out, "development")
}

func TestPreset_UsesCorrectWorkdir(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// All presets should use /workspace as the default workdir
	out := testutil.RunDrun(t, dir, "run", "pwd")

	assert.Contains(t, out, "/workspace")
}

func TestPreset_MountsGoBuildCache(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Go preset should mount the build cache directory
	// The directory should exist (it's created by mount)
	out := testutil.RunDrun(t, dir, "run", "ls", "-la", "/root/.cache/go-build")

	// If the directory exists (even if empty), the mount worked
	// We just check it doesn't error out
	assert.NotContains(t, out, "No such file")
}
