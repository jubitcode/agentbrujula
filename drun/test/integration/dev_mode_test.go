package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/test/testutil"
)

func TestDevMode_Flag(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Run with --dev flag
	out := testutil.RunDrun(t, dir, "--dev", "run", "echo", "hello")

	assert.Contains(t, out, "hello")
}

func TestLSP_Status(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Initialize container
	testutil.RunDrun(t, dir, "run", "echo", "init")

	// Get LSP status
	out := testutil.RunDrun(t, dir, "lsp", "status")

	// Should return JSON with gopls configured (from Go preset)
	var services []map[string]interface{}
	err := json.Unmarshal([]byte(out), &services)
	require.NoError(t, err, "LSP status should return valid JSON")

	assert.NotEmpty(t, services, "should have configured services")

	// Check for gopls
	found := false
	for _, svc := range services {
		if svc["server"] == "gopls" {
			found = true
			assert.Equal(t, float64(6389), svc["port"])
			break
		}
	}
	assert.True(t, found, "should have gopls configured")
}

func TestPathmap_ContainerToHost(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Get path mapping from container to host
	out := testutil.RunDrun(t, dir, "pathmap", "/workspace/main.go")

	// Should contain the project directory
	out = strings.TrimSpace(out)
	assert.Contains(t, out, "main.go", "should contain the file name")
}

func TestPathmap_HostToContainer(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Map a host path to container path
	hostPath := dir + "/main.go"
	out := testutil.RunDrun(t, dir, "pathmap", "--reverse", hostPath)

	out = strings.TrimSpace(out)
	assert.Equal(t, "/workspace/main.go", out, "should map to /workspace/main.go")
}

func TestPathmap_JSONOutput(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Get path mapping with JSON output
	out := testutil.RunDrun(t, dir, "pathmap", "--json", "/workspace/main.go")

	var result map[string]string
	err := json.Unmarshal([]byte(out), &result)
	require.NoError(t, err, "should return valid JSON")

	assert.Equal(t, "/workspace/main.go", result["container"])
	assert.Contains(t, result["host"], "main.go")
}

func TestDevMode_PresetHasDevConfig(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Check LSP status to verify dev config is loaded from preset
	out := testutil.RunDrun(t, dir, "lsp", "status")

	// Should have gopls from Go preset
	assert.Contains(t, out, "gopls")
	assert.Contains(t, out, "6389") // gopls port
}

func TestLSP_StopNonRunning(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Initialize container
	testutil.RunDrun(t, dir, "run", "echo", "init")

	// Stop LSP should succeed even if not running
	out := testutil.RunDrun(t, dir, "lsp", "stop")

	// Should return JSON with stopped status
	var result map[string]interface{}
	err := json.Unmarshal([]byte(out), &result)
	require.NoError(t, err, "should return valid JSON")

	assert.Equal(t, "stopped", result["status"])
}

// TestLSP_ActualConnection tests actual LSP TCP communication with gopls.
// This test installs gopls, starts it, and verifies we can send LSP requests.
func TestLSP_ActualConnection(t *testing.T) {
	testutil.RequireDocker(t)

	if testing.Short() {
		t.Skip("skipping LSP connection test in short mode")
	}

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Create container with --dev to expose LSP port
	testutil.RunDrun(t, dir, "--dev", "run", "echo", "init")

	// Install gopls (compatible version for Go 1.22)
	testutil.RunDrun(t, dir, "run", "go", "install", "golang.org/x/tools/gopls@v0.15.0")

	// Start gopls in background
	testutil.RunDrun(t, dir, "run", "sh", "-c", "nohup /go/bin/gopls serve -listen=:6389 > /tmp/gopls.log 2>&1 &")

	// Give gopls time to initialize
	time.Sleep(2 * time.Second)

	// Wait for gopls to start listening
	if !testutil.WaitForPort(t, 6389, 15*time.Second) {
		// Get gopls log for debugging
		log, _ := testutil.RunDrunOutput(t, dir, "run", "cat", "/tmp/gopls.log")
		t.Fatalf("gopls did not start listening on port 6389. Log: %s", log)
	}

	// Give gopls a moment to be ready for connections
	time.Sleep(500 * time.Millisecond)

	// Send LSP initialize request with retries
	var serverName string
	var err error
	for i := 0; i < 3; i++ {
		serverName, err = testutil.LSPInitialize(t, 6389, 5*time.Second)
		if err == nil {
			break
		}
		t.Logf("LSP connection attempt %d failed: %v", i+1, err)
		time.Sleep(time.Second)
	}
	require.NoError(t, err, "should be able to communicate with gopls")
	assert.Equal(t, "gopls", serverName, "should receive response from gopls")
}

// TestDevMode_PortsExposed verifies that --dev flag exposes LSP/DAP ports.
func TestDevMode_PortsExposed(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Create container with --dev flag
	testutil.RunDrun(t, dir, "--dev", "run", "echo", "init")

	// Check that ports are in container config
	containerID := testutil.GetContainerID(t, "drun-")
	require.NotEmpty(t, containerID, "container should exist")

	// Inspect container for port bindings
	out, err := exec.Command("docker", "inspect", containerID,
		"--format", "{{json .HostConfig.PortBindings}}").Output()
	require.NoError(t, err, "should be able to inspect container")

	portBindings := string(out)
	assert.Contains(t, portBindings, "6389", "should expose LSP port 6389")
	assert.Contains(t, portBindings, "38697", "should expose DAP port 38697")
}

// TestPathmap_WithContainerFile verifies pathmap with files created in container.
func TestPathmap_WithContainerFile(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Initialize container
	testutil.RunDrun(t, dir, "run", "echo", "init")

	// Create nested directory and file in container
	testutil.RunDrun(t, dir, "run", "sh", "-c",
		"mkdir -p /workspace/src/handlers && echo 'package handlers' > /workspace/src/handlers/api.go")

	// Get pathmap for the container file
	out := testutil.RunDrun(t, dir, "pathmap", "/workspace/src/handlers/api.go")
	hostPath := strings.TrimSpace(out)

	// Verify the file exists on host at the mapped path
	_, err := os.Stat(hostPath)
	require.NoError(t, err, "file should exist on host at mapped path: %s", hostPath)

	// Verify the content matches
	content, err := os.ReadFile(hostPath)
	require.NoError(t, err, "should be able to read file")
	assert.Contains(t, string(content), "package handlers")
}

// TestPathmap_Bidirectional verifies paths work in both directions.
func TestPathmap_Bidirectional(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Initialize container
	testutil.RunDrun(t, dir, "run", "echo", "init")

	// Test 1: Create file on host, verify container sees it
	hostFilePath := testutil.CreateHostFile(t, dir, "pkg/utils/helper.go", "package utils\n")
	assert.True(t, testutil.VerifyContainerSeesFile(t, dir, "/workspace/pkg/utils/helper.go"),
		"container should see file created on host")

	// Verify reverse pathmap gives correct container path
	out := testutil.RunDrun(t, dir, "pathmap", "--reverse", hostFilePath)
	assert.Equal(t, "/workspace/pkg/utils/helper.go", strings.TrimSpace(out))

	// Test 2: Create file in container, verify host sees it
	testutil.RunDrun(t, dir, "run", "sh", "-c",
		"mkdir -p /workspace/internal/api && echo 'package api' > /workspace/internal/api/server.go")

	// Verify pathmap and host file exists
	out = testutil.RunDrun(t, dir, "pathmap", "/workspace/internal/api/server.go")
	hostPath := strings.TrimSpace(out)

	content, err := os.ReadFile(hostPath)
	require.NoError(t, err, "should be able to read file created in container")
	assert.Contains(t, string(content), "package api")
}

// TestPathmap_NestedDirectories tests pathmap with deeply nested paths.
func TestPathmap_NestedDirectories(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Initialize container
	testutil.RunDrun(t, dir, "run", "echo", "init")

	// Test various nested paths
	testCases := []struct {
		containerPath string
		relativePath  string
	}{
		{"/workspace/cmd/server/main.go", "cmd/server/main.go"},
		{"/workspace/internal/pkg/db/postgres/connection.go", "internal/pkg/db/postgres/connection.go"},
		{"/workspace/api/v1/handlers/user.go", "api/v1/handlers/user.go"},
	}

	for _, tc := range testCases {
		t.Run(tc.relativePath, func(t *testing.T) {
			// Create file on host
			testutil.CreateHostFile(t, dir, tc.relativePath, "package test\n")

			// Verify container to host mapping
			out := testutil.RunDrun(t, dir, "pathmap", tc.containerPath)
			hostPath := strings.TrimSpace(out)
			assert.Contains(t, hostPath, tc.relativePath)

			// Verify file exists at mapped path
			_, err := os.Stat(hostPath)
			assert.NoError(t, err, "file should exist at mapped path")

			// Verify reverse mapping
			out = testutil.RunDrun(t, dir, "pathmap", "--reverse", hostPath)
			assert.Equal(t, tc.containerPath, strings.TrimSpace(out))
		})
	}
}
