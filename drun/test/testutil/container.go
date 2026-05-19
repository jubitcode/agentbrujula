// Package testutil provides integration test helpers for drun.
package testutil

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// DrunBinary is the path to the drun binary. Set this before running tests.
var DrunBinary = "drun"

// TempProject creates a temporary project directory with language-specific marker files.
// Returns the directory path and a cleanup function.
func TempProject(t *testing.T, lang string) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "drun-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Create language-specific marker files
	switch lang {
	case "go":
		writeFile(t, dir, "go.mod", "module test\n\ngo 1.22\n")
		writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	case "python":
		writeFile(t, dir, "requirements.txt", "")
		writeFile(t, dir, "main.py", "")
	case "node":
		writeFile(t, dir, "package.json", `{"name":"test","version":"1.0.0"}`)
	case "rust":
		writeFile(t, dir, "Cargo.toml", "[package]\nname = \"test\"\nversion = \"0.1.0\"\nedition = \"2021\"\n")
		writeFile(t, dir, "src/main.rs", "fn main() {}\n")
	case "odin":
		writeFile(t, dir, "main.odin", "package main\n")
	case "": // Empty project, no markers
	default:
		t.Fatalf("unknown language: %s", lang)
	}

	return dir, func() { os.RemoveAll(dir) }
}

// writeFile creates a file with the given content in the specified directory.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create directory for %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", name, err)
	}
}

// RunDrun executes drun command in the specified directory and returns the output.
// Fails the test if the command fails.
func RunDrun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(DrunBinary, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("drun %v failed: %v\nOutput: %s", args, err, out)
	}
	return string(out)
}

// RunDrunOutput executes drun command and returns both output and error.
// Does not fail the test on error.
func RunDrunOutput(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(DrunBinary, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// RunDrunExpectError runs drun expecting it to fail, returns the output.
func RunDrunExpectError(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(DrunBinary, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected drun %v to fail, but it succeeded. Output: %s", args, out)
	}
	return string(out)
}

// ContainerExists checks if a container with the given name prefix is running.
func ContainerExists(t *testing.T, namePrefix string) bool {
	t.Helper()
	cmd := exec.Command("docker", "ps", "-q", "--filter", "name="+namePrefix)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// ContainerExistsAll checks if a container exists (running or stopped).
func ContainerExistsAll(t *testing.T, namePrefix string) bool {
	t.Helper()
	cmd := exec.Command("docker", "ps", "-aq", "--filter", "name="+namePrefix)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// CleanupContainer removes a container by name prefix.
func CleanupContainer(t *testing.T, namePrefix string) {
	t.Helper()
	// Find containers matching the prefix
	cmd := exec.Command("docker", "ps", "-aq", "--filter", "name="+namePrefix)
	out, err := cmd.Output()
	if err != nil {
		return
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return
	}
	// Remove each container
	for _, id := range ids {
		exec.Command("docker", "rm", "-f", id).Run()
	}
}

// CleanupContainersWithLabel removes all containers with the drun.managed label.
func CleanupContainersWithLabel(t *testing.T) {
	t.Helper()
	cmd := exec.Command("docker", "ps", "-aq", "--filter", "label=drun.managed")
	out, err := cmd.Output()
	if err != nil {
		return
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return
	}
	for _, id := range ids {
		exec.Command("docker", "rm", "-f", id).Run()
	}
}

// WaitForPort waits for a TCP port to become available within the timeout.
func WaitForPort(t *testing.T, port int, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("localhost:%d", port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// WaitForContainer waits for a container to be running.
func WaitForContainer(t *testing.T, namePrefix string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ContainerExists(t, namePrefix) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// GetContainerID returns the container ID for a container with the given name prefix.
func GetContainerID(t *testing.T, namePrefix string) string {
	t.Helper()
	cmd := exec.Command("docker", "ps", "-q", "--filter", "name="+namePrefix)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ExecInContainer runs a command inside a container and returns the output.
func ExecInContainer(t *testing.T, containerID string, command ...string) (string, error) {
	t.Helper()
	args := append([]string{"exec", containerID}, command...)
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// InitGitRepo initializes a git repository in the given directory.
func InitGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
	// Configure git user for commits
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()
}

// GitCheckout creates and checks out a new branch.
func GitCheckout(t *testing.T, dir, branch string) {
	t.Helper()
	cmd := exec.Command("git", "checkout", "-B", branch)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout -B %s failed: %v\n%s", branch, err, out)
	}
}

// GitCurrentBranch returns the current git branch.
func GitCurrentBranch(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git branch --show-current failed: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// RequireDocker skips the test if Docker is not available.
func RequireDocker(t *testing.T) {
	t.Helper()
	cmd := exec.Command("docker", "info")
	if err := cmd.Run(); err != nil {
		t.Skip("Docker is not available, skipping integration test")
	}
}

// ContainerName returns the expected drun container name for a project directory.
// This matches the naming convention used by drun.
func ContainerName(projectDir string) string {
	base := filepath.Base(projectDir)
	return "drun-" + base
}

// LSPInitialize sends an LSP initialize request and returns the response.
// Returns the server name from the response, or an error.
func LSPInitialize(t *testing.T, port int, timeout time.Duration) (string, error) {
	t.Helper()

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), timeout)
	if err != nil {
		return "", fmt.Errorf("failed to connect to LSP server: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(timeout))

	// LSP initialize request
	request := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":1,"rootUri":"file:///workspace","capabilities":{}}}`
	message := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(request), request)

	_, err = conn.Write([]byte(message))
	if err != nil {
		return "", fmt.Errorf("failed to send LSP request: %w", err)
	}

	// Read response
	buf := make([]byte, 8192)
	n, err := conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("failed to read LSP response: %w", err)
	}

	response := string(buf[:n])
	// Check if we got a valid response with serverInfo
	if strings.Contains(response, `"serverInfo"`) {
		// Extract server name (simple check, not full JSON parsing)
		if strings.Contains(response, `"name":"gopls"`) || strings.Contains(response, `"name": "gopls"`) {
			return "gopls", nil
		}
		if strings.Contains(response, `"name"`) {
			return "unknown", nil
		}
	}

	// Check for any valid result
	if strings.Contains(response, `"result"`) {
		return "unknown", nil
	}

	return "", fmt.Errorf("invalid LSP response: %s", response)
}

// VerifyPathmapFile creates a file in the container and verifies it exists at the pathmap location.
func VerifyPathmapFile(t *testing.T, projectDir, containerPath string) bool {
	t.Helper()

	// Create file in container
	RunDrun(t, projectDir, "run", "sh", "-c", fmt.Sprintf("mkdir -p $(dirname %s) && touch %s", containerPath, containerPath))

	// Get host path via pathmap
	hostPath := strings.TrimSpace(RunDrun(t, projectDir, "pathmap", containerPath))

	// Verify file exists on host
	_, err := os.Stat(hostPath)
	return err == nil
}

// CreateHostFile creates a file on the host and returns its path.
func CreateHostFile(t *testing.T, projectDir, relativePath, content string) string {
	t.Helper()
	fullPath := filepath.Join(projectDir, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	return fullPath
}

// VerifyContainerSeesFile checks if the container can see a file at the given container path.
func VerifyContainerSeesFile(t *testing.T, projectDir, containerPath string) bool {
	t.Helper()
	_, err := RunDrunOutput(t, projectDir, "run", "test", "-f", containerPath)
	return err == nil
}
