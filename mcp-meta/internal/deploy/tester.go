package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"mcp-meta/internal/registry"
	"mcp-meta/internal/supervisor"
)

// Tester runs smoke tests against a freshly built pod.
type Tester struct {
	logger *slog.Logger
}

// NewTester creates a smoke test runner.
func NewTester(logger *slog.Logger) *Tester {
	if logger == nil {
		logger = slog.Default()
	}
	return &Tester{logger: logger}
}

// TestResult holds smoke test outcomes per tool.
type TestResult struct {
	PingOK    bool              `json:"ping"`
	ToolTests map[string]string `json:"tools"` // tool name → "pass" or error message
}

// Test spawns the pod as a subprocess, sends ping + each tool call, validates responses.
func (t *Tester) Test(ctx context.Context, sourceDir string, spec *registry.PodSpec) (*TestResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result := &TestResult{
		ToolTests: make(map[string]string),
	}

	// Spawn the pod as subprocess (for smoke testing, not production)
	sup := supervisor.New(t.logger)
	defer sup.Shutdown(context.Background())

	// Create a test spec with the source dir as workdir
	testSpec := *spec
	if testSpec.Workdir == "" {
		testSpec.Workdir = sourceDir
	}

	// Use venv python if available (for smoke testing with installed deps)
	if spec.Language == "python" {
		venvPython := filepath.Join(sourceDir, ".venv", "bin", "python3")
		if _, err := os.Stat(venvPython); err == nil && len(testSpec.Command) >= 2 {
			testSpec.Command = append([]string{venvPython}, testSpec.Command[1:]...)
			t.logger.Info("smoke test using venv python", "python", venvPython)
		}
	}
	if testSpec.CallTimeout.Duration == 0 {
		testSpec.CallTimeout.Duration = 20 * time.Second
	}
	if testSpec.ShutdownTimeout.Duration == 0 {
		testSpec.ShutdownTimeout.Duration = 5 * time.Second
	}

	pool, err := sup.EnsurePool(ctx, "smoke-test-"+spec.Name, &testSpec)
	if err != nil {
		return result, fmt.Errorf("spawn pod: %w", err)
	}

	pod, release, err := pool.AcquireWithTracking()
	if err != nil {
		return result, fmt.Errorf("acquire pod: %w", err)
	}
	defer release()

	// 1. Ping test
	t.logger.Info("smoke test: ping", "agent", spec.Name)
	pingResult, err := pod.Call(ctx, "ping", nil)
	if err != nil {
		return result, fmt.Errorf("ping failed: %w", err)
	}

	var pingResp map[string]any
	if err := json.Unmarshal(pingResult, &pingResp); err == nil {
		if status, ok := pingResp["status"].(string); ok && status == "ok" {
			result.PingOK = true
		}
	}
	if !result.PingOK {
		return result, fmt.Errorf("ping did not return status:ok, got: %s", string(pingResult))
	}

	// 2. Test each tool with empty/minimal args
	for _, tool := range spec.Tools {
		if tool.Name == "ping" || tool.Name == "shutdown" {
			continue
		}

		t.logger.Info("smoke test: tool call", "agent", spec.Name, "tool", tool.Name)

		// Send minimal args — an empty object
		args := json.RawMessage(`{}`)
		toolResult, err := pod.Call(ctx, tool.Name, args)

		if err != nil {
			result.ToolTests[tool.Name] = fmt.Sprintf("error: %v", err)
			// Don't fail the whole test — some tools may need real args
			// Just check that the pod didn't crash
			if pod.Status().State == supervisor.PodStateCrashed {
				return result, fmt.Errorf("pod crashed during tool %s: %v", tool.Name, err)
			}
			continue
		}

		// Verify we got valid JSON back (not an empty response)
		if len(toolResult) == 0 {
			result.ToolTests[tool.Name] = "error: empty response"
			continue
		}

		// Check if the response is a JSON-RPC error
		var respObj map[string]any
		if err := json.Unmarshal(toolResult, &respObj); err == nil {
			if _, hasError := respObj["error"]; hasError {
				// Tool returned an error response — that's OK, it means the pod
				// handled the request properly, just the args were wrong
				result.ToolTests[tool.Name] = "pass"
				continue
			}
		}

		result.ToolTests[tool.Name] = "pass"
	}

	return result, nil
}
