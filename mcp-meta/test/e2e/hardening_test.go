package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcp-meta/internal/api"
	"mcp-meta/internal/jsonrpc"
	"mcp-meta/internal/mcp"
	"mcp-meta/internal/registry"
	"mcp-meta/internal/router"
	"mcp-meta/internal/supervisor"
)

// testServer holds a running MCP-Meta server for e2e tests.
type testServer struct {
	URL    string
	server *api.Server
	sup    supervisor.Supervisor
	cancel context.CancelFunc
}

// startServer boots a real MCP-Meta server with the given manifest YAML.
// The server listens on a random available port.
func startServer(t *testing.T, manifestYAML string) *testServer {
	t.Helper()

	// Write manifest to temp dir
	manifestDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(manifestDir, "testpod.yaml"), []byte(manifestYAML), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Load manifests
	reg := registry.New()
	if err := reg.LoadManifests(manifestDir); err != nil {
		t.Fatalf("load manifests: %v", err)
	}
	if errs := reg.Validate(); len(errs) > 0 {
		t.Fatalf("manifest validation: %v", errs)
	}

	// Create supervisor and router
	sup := supervisor.New(logger)
	rtr := router.New(reg, sup, logger)

	// Wire inter-pod handler
	sup.SetInterPodHandlerWithContract(func(ctx context.Context, sourcePod string, targetTool string, args json.RawMessage, contract *jsonrpc.ContractExpectation) (json.RawMessage, error) {
		return rtr.InternalCallWithContract(ctx, sourcePod, targetTool, args, contract)
	})

	mcpHandler := mcp.NewHandler(reg, rtr, sup, logger)

	// Find a free port
	port := freePort(t)

	apiServer := api.NewServer(reg, rtr, mcpHandler, logger, api.Config{
		Host: "127.0.0.1",
		Port: port,
	})

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		if err := apiServer.Start(ctx); err != nil && err != context.Canceled {
			logger.Error("server error", "error", err)
		}
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Wait for server to be ready
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	ts := &testServer{
		URL:    baseURL,
		server: apiServer,
		sup:    sup,
		cancel: cancel,
	}

	t.Cleanup(func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		apiServer.Shutdown(shutdownCtx)
		sup.Shutdown(shutdownCtx)
	})

	return ts
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// httpDo is a helper that sends an HTTP request and returns the status code and body.
func httpDo(t *testing.T, method, url string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody
}

// Cwd-relative path to testpod source.
func testpodCommand() string {
	// test/e2e/ is 2 levels deep from repo root
	abs, _ := filepath.Abs("../../test/testpod/main.go")
	return abs
}

// --- Manifest templates ---

const baseManifest = `apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: testpod
  description: "E2E test pod"
spec:
  command: ["go", "run", "%s"]
  callTimeout: 30s
  shutdownTimeout: 5s
  tools:
    - name: ping
      description: "Health check"
    - name: echo
      description: "Echo input"
    - name: very_slow
      description: "500ms sleep"
    - name: crash
      description: "Exit 1"
`

func manifest() string {
	return fmt.Sprintf(baseManifest, testpodCommand())
}

func manifestWithCircuitBreaker() string {
	return fmt.Sprintf(`apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: testpod
  description: "E2E test pod with circuit breaker"
spec:
  command: ["go", "run", "%s"]
  callTimeout: 30s
  shutdownTimeout: 5s
  circuitBreaker:
    failureThreshold: 2
    resetTimeout: 500ms
    successThreshold: 1
  tools:
    - name: ping
      description: "Health check"
    - name: echo
      description: "Echo input"
    - name: very_slow
      description: "500ms sleep"
      timeout: 50ms
`, testpodCommand())
}

func manifestWithRetry() string {
	return fmt.Sprintf(`apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: testpod
  description: "E2E test pod with retry"
spec:
  command: ["go", "run", "%s"]
  callTimeout: 30s
  shutdownTimeout: 5s
  retry:
    maxRetries: 2
    baseDelay: 10ms
    maxDelay: 50ms
  circuitBreaker:
    failureThreshold: 100
    resetTimeout: 30s
    successThreshold: 1
  tools:
    - name: very_slow
      description: "500ms sleep"
      timeout: 50ms
    - name: echo
      description: "Echo input"
`, testpodCommand())
}

func manifestWithSizeLimit() string {
	return fmt.Sprintf(`apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: testpod
  description: "E2E test pod with size limit"
spec:
  command: ["go", "run", "%s"]
  callTimeout: 30s
  shutdownTimeout: 5s
  maxResponseSize: 200
  tools:
    - name: echo
      description: "Echo input"
`, testpodCommand())
}

// ==================== Tests ====================

func TestE2E_HealthAndReady(t *testing.T) {
	ts := startServer(t, manifest())

	// Health
	code, body := httpDo(t, "GET", ts.URL+"/health", nil)
	if code != 200 {
		t.Fatalf("GET /health = %d, want 200", code)
	}
	var health map[string]string
	json.Unmarshal(body, &health)
	if health["status"] != "ok" {
		t.Errorf("health status = %q, want ok", health["status"])
	}

	// Ready
	code, body = httpDo(t, "GET", ts.URL+"/ready", nil)
	if code != 200 {
		t.Fatalf("GET /ready = %d, want 200; body: %s", code, body)
	}
}

func TestE2E_ListTools(t *testing.T) {
	ts := startServer(t, manifest())

	code, body := httpDo(t, "GET", ts.URL+"/v1/tools", nil)
	if code != 200 {
		t.Fatalf("GET /v1/tools = %d; body: %s", code, body)
	}

	var resp map[string]any
	json.Unmarshal(body, &resp)
	tools, _ := resp["tools"].([]any)
	if len(tools) == 0 {
		t.Fatal("expected at least 1 tool, got 0")
	}
	t.Logf("tools: %d", len(tools))
}

func TestE2E_ExecuteTool_Ping(t *testing.T) {
	ts := startServer(t, manifest())

	code, body := httpDo(t, "POST", ts.URL+"/v1/execute", map[string]any{
		"tool":      "testpod.ping",
		"arguments": map[string]any{},
	})
	if code != 200 {
		t.Fatalf("POST /v1/execute = %d; body: %s", code, body)
	}

	var resp map[string]any
	json.Unmarshal(body, &resp)
	if resp["error"] != nil && resp["error"] != "" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}

	// result should contain ping response
	result, _ := resp["result"].(map[string]any)
	if result["status"] != "ok" {
		t.Errorf("ping result = %v, want status=ok", result)
	}
	t.Logf("ping result: %v", result)
}

func TestE2E_ExecuteTool_Echo(t *testing.T) {
	ts := startServer(t, manifest())

	code, body := httpDo(t, "POST", ts.URL+"/v1/execute", map[string]any{
		"tool": "testpod.echo",
		"arguments": map[string]any{
			"message": "hello e2e",
			"number":  42,
		},
	})
	if code != 200 {
		t.Fatalf("POST /v1/execute = %d; body: %s", code, body)
	}

	var resp map[string]any
	json.Unmarshal(body, &resp)
	if resp["error"] != nil && resp["error"] != "" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}

	result, _ := resp["result"].(map[string]any)
	if result["message"] != "hello e2e" {
		t.Errorf("echo message = %v, want 'hello e2e'", result["message"])
	}
	if result["handled_by"] == nil {
		t.Error("expected handled_by field in echo response")
	}
	t.Logf("echo result: %v", result)
}

func TestE2E_ExecuteTool_NotFound(t *testing.T) {
	ts := startServer(t, manifest())

	code, body := httpDo(t, "POST", ts.URL+"/v1/execute", map[string]any{
		"tool": "nonexistent.tool",
	})
	if code != 200 {
		t.Fatalf("expected 200 with error in body, got %d; body: %s", code, body)
	}

	var resp map[string]any
	json.Unmarshal(body, &resp)
	if resp["error"] == nil || resp["error"] == "" {
		t.Fatal("expected error for nonexistent tool")
	}
	t.Logf("not found error: %v", resp["error"])
}

func TestE2E_MCP_ToolsCall(t *testing.T) {
	ts := startServer(t, manifest())

	// MCP JSON-RPC tools/call
	code, body := httpDo(t, "POST", ts.URL+"/v1/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "echo",
			"arguments": map[string]any{"message": "mcp e2e"},
		},
	})
	if code != 200 {
		t.Fatalf("POST /v1/mcp = %d; body: %s", code, body)
	}

	var resp map[string]any
	json.Unmarshal(body, &resp)
	if resp["error"] != nil {
		t.Fatalf("MCP error: %v", resp["error"])
	}
	t.Logf("MCP result: %s", body)
}

func TestE2E_MCP_ToolsList(t *testing.T) {
	ts := startServer(t, manifest())

	code, body := httpDo(t, "POST", ts.URL+"/v1/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	if code != 200 {
		t.Fatalf("POST /v1/mcp tools/list = %d; body: %s", code, body)
	}

	var resp map[string]any
	json.Unmarshal(body, &resp)
	if resp["error"] != nil {
		t.Fatalf("MCP error: %v", resp["error"])
	}

	result, _ := resp["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) == 0 {
		t.Fatal("expected tools in MCP tools/list response")
	}
	t.Logf("MCP tools: %d", len(tools))
}

func TestE2E_CorrelationID_Provided(t *testing.T) {
	ts := startServer(t, manifest())

	reqBody, _ := json.Marshal(map[string]any{
		"tool":      "testpod.ping",
		"arguments": map[string]any{},
	})
	req, _ := http.NewRequest("POST", ts.URL+"/v1/execute", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "e2e-corr-id-999")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Header echoed back
	if got := resp.Header.Get("X-Request-ID"); got != "e2e-corr-id-999" {
		t.Errorf("X-Request-ID header = %q, want 'e2e-corr-id-999'", got)
	}

	// requestId in body
	var execResp map[string]any
	json.Unmarshal(body, &execResp)
	if execResp["requestId"] != "e2e-corr-id-999" {
		t.Errorf("requestId = %v, want 'e2e-corr-id-999'", execResp["requestId"])
	}
}

func TestE2E_CorrelationID_AutoGenerated(t *testing.T) {
	ts := startServer(t, manifest())

	code, _ := httpDo(t, "POST", ts.URL+"/v1/execute", map[string]any{
		"tool":      "testpod.ping",
		"arguments": map[string]any{},
	})
	if code != 200 {
		t.Fatalf("execute = %d", code)
	}

	// Make a raw request to check headers
	reqBody, _ := json.Marshal(map[string]any{
		"tool":      "testpod.ping",
		"arguments": map[string]any{},
	})
	req, _ := http.NewRequest("POST", ts.URL+"/v1/execute", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	// No X-Request-ID header

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	got := resp.Header.Get("X-Request-ID")
	if got == "" {
		t.Fatal("expected auto-generated X-Request-ID header")
	}
	if len(got) != 32 {
		t.Errorf("expected 32-char hex ID, got %d chars: %q", len(got), got)
	}
	t.Logf("auto-generated ID: %s", got)
}

func TestE2E_Metrics_Endpoint(t *testing.T) {
	ts := startServer(t, manifest())

	// Call a tool first so metrics have data
	httpDo(t, "POST", ts.URL+"/v1/execute", map[string]any{
		"tool":      "testpod.ping",
		"arguments": map[string]any{},
	})

	code, body := httpDo(t, "GET", ts.URL+"/metrics", nil)
	if code != 200 {
		t.Fatalf("GET /metrics = %d", code)
	}

	bodyStr := string(body)
	if !strings.Contains(bodyStr, "mcp_tool_calls_total") {
		t.Error("expected mcp_tool_calls_total in metrics output")
	}
	if !strings.Contains(bodyStr, "mcp_tool_call_duration_seconds") {
		t.Error("expected mcp_tool_call_duration_seconds in metrics output")
	}
	if !strings.Contains(bodyStr, "mcp_pod_spawns_total") {
		t.Error("expected mcp_pod_spawns_total in metrics output")
	}
}

func TestE2E_CircuitBreaker_TripsAndRecovers(t *testing.T) {
	ts := startServer(t, manifestWithCircuitBreaker())

	// Trip the breaker: 2 timeouts on very_slow (50ms timeout, 500ms sleep)
	for i := range 2 {
		code, body := httpDo(t, "POST", ts.URL+"/v1/execute", map[string]any{
			"tool":      "testpod.very_slow",
			"arguments": map[string]any{},
		})
		if code != 200 {
			t.Fatalf("attempt %d: unexpected status %d", i, code)
		}
		var resp map[string]any
		json.Unmarshal(body, &resp)
		if resp["error"] == nil || resp["error"] == "" {
			t.Fatalf("attempt %d: expected timeout error, got: %s", i, body)
		}
		t.Logf("attempt %d error: %v", i, resp["error"])
	}

	// Circuit should be open — echo should fail immediately
	code, body := httpDo(t, "POST", ts.URL+"/v1/execute", map[string]any{
		"tool":      "testpod.echo",
		"arguments": map[string]any{"message": "should fail"},
	})
	if code != 200 {
		t.Fatalf("circuit open check: status %d", code)
	}
	var resp map[string]any
	json.Unmarshal(body, &resp)
	errStr, _ := resp["error"].(string)
	if !strings.Contains(errStr, "circuit") {
		t.Errorf("expected circuit breaker error, got: %v", resp["error"])
	}
	t.Logf("circuit open: %v", resp["error"])

	// Wait for reset timeout (500ms) + margin for late responses to clear
	time.Sleep(1200 * time.Millisecond)

	// Breaker half-open → echo should succeed
	code, body = httpDo(t, "POST", ts.URL+"/v1/execute", map[string]any{
		"tool":      "testpod.echo",
		"arguments": map[string]any{"message": "recovered"},
	})
	if code != 200 {
		t.Fatalf("recovery: status %d; body: %s", code, body)
	}
	resp = map[string]any{} // reset — don't carry stale keys from previous unmarshal
	json.Unmarshal(body, &resp)
	if resp["error"] != nil && resp["error"] != "" {
		t.Fatalf("recovery should succeed, got error: %v; full body: %s", resp["error"], body)
	}
	result, _ := resp["result"].(map[string]any)
	if result["message"] != "recovered" {
		t.Errorf("expected message=recovered, got %v", result)
	}
	t.Logf("recovered: %v", result)
}

func TestE2E_Retry(t *testing.T) {
	ts := startServer(t, manifestWithRetry())

	// First call echo to make sure pod is running
	code, body := httpDo(t, "POST", ts.URL+"/v1/execute", map[string]any{
		"tool":      "testpod.echo",
		"arguments": map[string]any{"message": "warmup"},
	})
	if code != 200 {
		t.Fatalf("warmup: status %d; body: %s", code, body)
	}

	// Call very_slow with 50ms timeout — all 3 attempts (1 + 2 retries) will timeout
	code, body = httpDo(t, "POST", ts.URL+"/v1/execute", map[string]any{
		"tool":      "testpod.very_slow",
		"arguments": map[string]any{},
	})
	if code != 200 {
		t.Fatalf("retry test: status %d", code)
	}

	var resp map[string]any
	json.Unmarshal(body, &resp)
	errStr, _ := resp["error"].(string)
	if errStr == "" {
		t.Fatal("expected timeout error after retries exhausted")
	}
	t.Logf("retry exhausted error: %v", errStr)

	// Check metrics to verify retries happened
	code, metricsBody := httpDo(t, "GET", ts.URL+"/metrics", nil)
	if code != 200 {
		t.Fatalf("GET /metrics = %d", code)
	}
	metricsStr := string(metricsBody)
	if !strings.Contains(metricsStr, "mcp_retry_total") {
		t.Error("expected mcp_retry_total in metrics after retries")
	}
	t.Logf("retry metrics present: %v", strings.Contains(metricsStr, `mcp_retry_total{attempt="1"`))
}

func TestE2E_OutputSizeLimit(t *testing.T) {
	ts := startServer(t, manifestWithSizeLimit())

	// Send large message — response will exceed 200 byte limit
	largeMessage := strings.Repeat("A", 500)
	code, body := httpDo(t, "POST", ts.URL+"/v1/execute", map[string]any{
		"tool": "testpod.echo",
		"arguments": map[string]any{
			"message": largeMessage,
		},
	})
	if code != 200 {
		t.Fatalf("execute = %d; body: %s", code, body)
	}

	var resp map[string]any
	json.Unmarshal(body, &resp)
	if resp["error"] != nil && resp["error"] != "" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}

	// The result should be the truncation wrapper
	result, _ := resp["result"].(map[string]any)
	if result["_truncated"] != true {
		t.Errorf("expected _truncated=true, got %v", result)
	}
	if result["_max_size"] == nil {
		t.Error("expected _max_size in truncated response")
	}
	origSize, _ := result["_original_size"].(float64)
	if origSize <= 200 {
		t.Errorf("expected _original_size > 200, got %.0f", origSize)
	}
	t.Logf("truncated: original=%.0f, max=%.0f", origSize, result["_max_size"])
}
