package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/test/testutil"
)

func init() {
	// Set the drun binary path. In CI, this should be set to the built binary.
	// For local development, we assume drun is built and in the project root.
	if path := os.Getenv("DRUN_BINARY"); path != "" {
		// Convert to absolute path if needed
		if !filepath.IsAbs(path) {
			if abs, err := filepath.Abs(path); err == nil {
				path = abs
			}
		}
		testutil.DrunBinary = path
	}
}

func TestShell_CreatesContainer(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Run a simple echo command
	out := testutil.RunDrun(t, dir, "run", "echo", "hello")

	assert.Contains(t, out, "hello")
}

func TestShell_PersistentContainer(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// First command creates a file in the container
	testutil.RunDrun(t, dir, "run", "touch", "/tmp/testfile")

	// Second command should see the file (same container)
	out := testutil.RunDrun(t, dir, "run", "ls", "/tmp/testfile")

	assert.Contains(t, out, "testfile")
}

func TestShell_MountsProjectDirectory(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Create a test file in the project directory
	testFilePath := filepath.Join(dir, "testfile.txt")
	err := os.WriteFile(testFilePath, []byte("test content"), 0644)
	require.NoError(t, err)

	// The file should be visible in the container at /workspace
	out := testutil.RunDrun(t, dir, "run", "cat", "/workspace/testfile.txt")

	assert.Contains(t, out, "test content")
}

func TestShell_ExecNotRun(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// First command initializes the container
	testutil.RunDrun(t, dir, "run", "echo", "init")

	// Subsequent commands should be fast (using exec, not run)
	start := time.Now()
	testutil.RunDrun(t, dir, "run", "echo", "fast")
	elapsed := time.Since(start)

	// Should be very fast (< 2 seconds) since we're using exec on existing container
	assert.Less(t, elapsed, 2*time.Second, "command should use exec, not run (too slow)")
}

func TestShell_RunsCommand(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Run go version command
	out := testutil.RunDrun(t, dir, "run", "go", "version")

	assert.Contains(t, out, "go version")
}

func TestShell_WorkdirIsProjectRoot(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// pwd should return /workspace (the default workdir)
	out := testutil.RunDrun(t, dir, "run", "pwd")

	assert.Contains(t, strings.TrimSpace(out), "/workspace")
}

func TestShell_EnvironmentVariables(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Go preset sets CGO_ENABLED=0
	out := testutil.RunDrun(t, dir, "run", "sh", "-c", "echo $CGO_ENABLED")

	assert.Contains(t, out, "0")
}

func TestShell_FailsWithoutConfig(t *testing.T) {
	testutil.RequireDocker(t)

	// Create an empty directory with no language markers
	dir, err := os.MkdirTemp("", "drun-empty-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	// Should fail because no config can be determined
	out := testutil.RunDrunExpectError(t, dir, "run", "echo", "hi")

	// Should contain an error about missing config
	assert.True(t, strings.Contains(out, "config") || strings.Contains(out, "detect"),
		"expected error about missing config, got: %s", out)
}

func TestShell_CustomEnvVar(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Pass a custom environment variable via the -e flag
	out := testutil.RunDrun(t, dir, "-e", "MY_VAR=hello", "run", "sh", "-c", "echo $MY_VAR")

	assert.Contains(t, out, "hello")
}
