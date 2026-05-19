package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcp-meta/internal/a2a"
	"mcp-meta/internal/jsonrpc"
	"mcp-meta/internal/registry"
	"mcp-meta/internal/router"
	"mcp-meta/internal/supervisor"
)

// Mock registry for testing
type mockRegistry struct {
	tools []registry.ToolInfo
}

func (m *mockRegistry) LoadManifests(path string) error { return nil }
func (m *mockRegistry) AllTools() []registry.ToolInfo   { return m.tools }
func (m *mockRegistry) ResolveTool(toolName string) (*registry.PodSpec, *registry.ToolDef, error) {
	for _, t := range m.tools {
		if t.Name == toolName {
			return &registry.PodSpec{Name: "test-pod"}, &registry.ToolDef{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			}, nil
		}
	}
	return nil, nil, &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "tool not found"}
}
func (m *mockRegistry) GetPodSpec(podName string) *registry.PodSpec { return nil }
func (m *mockRegistry) PodsToPreWarm() []*registry.PodSpec          { return nil }
func (m *mockRegistry) Validate() []error                           { return nil }
func (m *mockRegistry) AllAgentPods() []*registry.PodSpec                              { return nil }
func (m *mockRegistry) RegisterManifest(manifest *registry.Manifest) error             { return nil }
func (m *mockRegistry) UnregisterPod(podName string) error                             { return nil }

// Mock router for testing
type mockRouter struct {
	result json.RawMessage
	err    error
}

func (m *mockRouter) CallTool(ctx context.Context, toolName string, args json.RawMessage) (json.RawMessage, error) {
	return m.result, m.err
}
func (m *mockRouter) InternalCall(ctx context.Context, sourcePod string, targetTool string, args json.RawMessage) (json.RawMessage, error) {
	return m.result, m.err
}
func (m *mockRouter) InternalCallWithContract(ctx context.Context, sourcePod string, targetTool string, args json.RawMessage, expect *jsonrpc.ContractExpectation) (json.RawMessage, error) {
	return m.result, m.err
}
func (m *mockRouter) SetWarmPoolManager(wpm *supervisor.WarmPoolManager) {}
func (m *mockRouter) GetWarmPoolManager() *supervisor.WarmPoolManager {
	return nil
}
func (m *mockRouter) GetWarmPoolStatus() map[string]*supervisor.WarmPoolStatus {
	return nil
}
func (m *mockRouter) SetCostTracker(ct router.CostTracker) {}

// SubscribePoolProgress is a no-op stub for tests. Required by router.Router
// since the streaming feature was added; tests don't exercise the bus.
func (m *mockRouter) SubscribePoolProgress(_ string) (<-chan json.RawMessage, func()) {
	ch := make(chan json.RawMessage)
	close(ch)
	return ch, func() {}
}

// testAuth returns a simple authenticator with a known key for handler tests.
func testAuth() Authenticator {
	auth := NewAPIKeyAuthenticator("X-API-Key")
	auth.RegisterKey("test-key", &Tenant{ID: "test", Name: "Test", Tier: "paid"})
	return auth
}

// testRequest creates a request with the test API key set.
func testRequest(method, path string, body *bytes.Buffer) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, body)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("X-API-Key", "test-key")
	return req
}

func TestServer_Health(t *testing.T) {
	reg := &mockRegistry{}
	rtr := &mockRouter{}
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %s", resp["status"])
	}
}

func TestServer_Ready(t *testing.T) {
	reg := &mockRegistry{
		tools: []registry.ToolInfo{
			{Name: "test-tool", Description: "A test tool"},
		},
	}
	rtr := &mockRouter{}
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080})

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "ready" {
		t.Errorf("expected status ready, got %v", resp["status"])
	}

	if resp["tool_count"].(float64) != 1 {
		t.Errorf("expected tool_count 1, got %v", resp["tool_count"])
	}
}

func TestServer_Ready_NoTools(t *testing.T) {
	reg := &mockRegistry{tools: []registry.ToolInfo{}}
	rtr := &mockRouter{}
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080})

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestServer_ListTools(t *testing.T) {
	reg := &mockRegistry{
		tools: []registry.ToolInfo{
			{Name: "acme/tool1", Description: "First tool"},
			{Name: "acme/tool2", Description: "Second tool"},
			{Name: "other/tool3", Description: "Third tool"},
		},
	}
	rtr := &mockRouter{}
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080, Authenticator: testAuth()})

	req := testRequest("GET", "/v1/tools", nil)
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp ListToolsResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.TotalCount != 3 {
		t.Errorf("expected 3 tools, got %d", resp.TotalCount)
	}
}

func TestServer_ListTools_WithQuery(t *testing.T) {
	reg := &mockRegistry{
		tools: []registry.ToolInfo{
			{Name: "acme/search", Description: "Search tool"},
			{Name: "acme/other", Description: "Other tool"},
		},
	}
	rtr := &mockRouter{}
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080, Authenticator: testAuth()})

	req := testRequest("GET", "/v1/tools?q=search", nil)
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp ListToolsResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.TotalCount != 1 {
		t.Errorf("expected 1 tool matching 'search', got %d", resp.TotalCount)
	}
}

func TestServer_ListTools_ByNamespace(t *testing.T) {
	reg := &mockRegistry{
		tools: []registry.ToolInfo{
			{Name: "acme/tool1", Description: "First tool"},
			{Name: "other/tool2", Description: "Second tool"},
		},
	}
	rtr := &mockRouter{}
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080, Authenticator: testAuth()})

	req := testRequest("GET", "/v1/tools?namespace=acme", nil)
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp ListToolsResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.TotalCount != 1 {
		t.Errorf("expected 1 tool in namespace 'acme', got %d", resp.TotalCount)
	}
}

func TestServer_GetTool(t *testing.T) {
	reg := &mockRegistry{
		tools: []registry.ToolInfo{
			{Name: "acme/hello", Description: "Hello tool", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	}
	rtr := &mockRouter{}
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080, Authenticator: testAuth()})

	req := testRequest("GET", "/v1/tools/acme/hello", nil)
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp ToolResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Name != "acme/hello" {
		t.Errorf("expected name acme/hello, got %s", resp.Name)
	}
}

func TestServer_GetTool_NotFound(t *testing.T) {
	reg := &mockRegistry{tools: []registry.ToolInfo{}}
	rtr := &mockRouter{}
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080, Authenticator: testAuth()})

	req := testRequest("GET", "/v1/tools/acme/notfound", nil)
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestServer_Execute(t *testing.T) {
	reg := &mockRegistry{
		tools: []registry.ToolInfo{
			{Name: "test/hello", Description: "Hello"},
		},
	}
	rtr := &mockRouter{
		result: json.RawMessage(`{"message":"Hello, World!"}`),
	}
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080, Authenticator: testAuth()})

	body := `{"tool":"test/hello","arguments":{"name":"World"}}`
	req := testRequest("POST", "/v1/execute", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp ExecuteResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}

	if resp.Duration < 0 {
		t.Error("expected duration >= 0")
	}

	if resp.RequestID == "" {
		t.Error("expected request ID")
	}
}

func TestServer_Execute_Error(t *testing.T) {
	reg := &mockRegistry{}
	rtr := &mockRouter{
		err: &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "tool not found"},
	}
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080, Authenticator: testAuth()})

	body := `{"tool":"unknown/tool"}`
	req := testRequest("POST", "/v1/execute", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp ExecuteResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Error == "" {
		t.Error("expected error in response")
	}
}

func TestServer_Execute_MissingTool(t *testing.T) {
	reg := &mockRegistry{}
	rtr := &mockRouter{}
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080, Authenticator: testAuth()})

	body := `{"arguments":{}}`
	req := testRequest("POST", "/v1/execute", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestServer_PublishTool_Invalid(t *testing.T) {
	reg := &mockRegistry{}
	rtr := &mockRouter{}
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080, Authenticator: testAuth()})

	body := `{"manifest":"invalid yaml: ["}`
	req := testRequest("POST", "/v1/tools", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestServer_PoolStatus(t *testing.T) {
	reg := &mockRegistry{}
	rtr := &mockRouter{}
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080, Authenticator: testAuth()})

	req := testRequest("GET", "/v1/admin/pools", nil)
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestServer_WithAuthentication(t *testing.T) {
	reg := &mockRegistry{
		tools: []registry.ToolInfo{{Name: "test", Description: "Test"}},
	}
	rtr := &mockRouter{}

	auth := NewAPIKeyAuthenticator("X-API-Key")
	auth.RegisterKey("valid-key", &Tenant{ID: "tenant1", Name: "Test Tenant"})

	srv := NewServer(reg, rtr, nil, nil, Config{
		Host:          "localhost",
		Port:          8080,
		Authenticator: auth,
	})

	// Without API key
	req := httptest.NewRequest("GET", "/v1/tools", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 without key, got %d", w.Code)
	}

	// With valid API key
	req = httptest.NewRequest("GET", "/v1/tools", nil)
	req.Header.Set("X-API-Key", "valid-key")
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 with valid key, got %d", w.Code)
	}

	// With invalid API key
	req = httptest.NewRequest("GET", "/v1/tools", nil)
	req.Header.Set("X-API-Key", "invalid-key")
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 with invalid key, got %d", w.Code)
	}
}

func TestServer_RateLimiting(t *testing.T) {
	reg := &mockRegistry{
		tools: []registry.ToolInfo{{Name: "test", Description: "Test"}},
	}
	rtr := &mockRouter{}

	// Very restrictive rate limiter: 1 request per second, burst 1
	limiter := NewTokenBucketLimiter(1, 1)

	srv := NewServer(reg, rtr, nil, nil, Config{
		Host:          "localhost",
		Port:          8080,
		RateLimiter:   limiter,
		Authenticator: testAuth(),
	})

	// First request should succeed
	req := testRequest("GET", "/v1/tools", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("first request: expected status 200, got %d", w.Code)
	}

	// Second immediate request should be rate limited
	req = testRequest("GET", "/v1/tools", nil)
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected status 429, got %d", w.Code)
	}

	if w.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header")
	}
}

func TestServer_MCPMessage(t *testing.T) {
	reg := &mockRegistry{}
	rtr := &mockRouter{}
	// No MCP handler
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080, Authenticator: testAuth()})

	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	req := testRequest("POST", "/v1/mcp", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	// Should fail because no MCP handler
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 without MCP handler, got %d", w.Code)
	}
}

// ============================================================
// P1-7: Tarball path traversal — filename sanitization
// ============================================================

func TestDeploy_FilenameSanitization(t *testing.T) {
	// Create a temp dir to simulate tmpDir
	tmpDir := t.TempDir()

	// Test that filepath.Base strips traversal components
	maliciousNames := []string{
		"../../../etc/passwd",
		"../../evil.sh",
		"foo/../../../bar.txt",
		"../secret",
	}

	for _, name := range maliciousNames {
		sanitized := filepath.Base(name)
		fullPath := filepath.Join(tmpDir, sanitized)

		// Write a file using sanitized name
		if err := os.WriteFile(fullPath, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}

		// Verify the file is inside tmpDir
		rel, err := filepath.Rel(tmpDir, fullPath)
		if err != nil {
			t.Fatalf("filepath.Rel failed: %v", err)
		}
		if strings.HasPrefix(rel, "..") {
			t.Errorf("sanitized file %q (from %q) escapes tmpDir: rel=%s", sanitized, name, rel)
		}
	}
}

func TestValidateNoPathTraversal(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some safe files
	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("ok"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "subdir", "nested.txt"), []byte("ok"), 0644)

	// Should pass — all files are within tmpDir
	if err := validateNoPathTraversal(tmpDir); err != nil {
		t.Errorf("expected no error for safe directory, got: %v", err)
	}
}

func TestDeploy_DotDotFilenameBecomesBase(t *testing.T) {
	// Verify filepath.Base behavior for edge cases
	cases := map[string]string{
		"../../../etc/crontab":  "crontab",
		"../../secret.key":     "secret.key",
		"normal.txt":           "normal.txt",
		"./local.txt":          "local.txt",
	}
	for input, expected := range cases {
		got := filepath.Base(input)
		if got != expected {
			t.Errorf("filepath.Base(%q) = %q, want %q", input, got, expected)
		}
	}
}

// ============================================================
// P1-8: Auth always required — no unauthenticated access
// ============================================================

func TestServer_NoAuth_RejectsProtectedEndpoints(t *testing.T) {
	reg := &mockRegistry{
		tools: []registry.ToolInfo{{Name: "test", Description: "Test"}},
	}
	rtr := &mockRouter{}

	// Create server WITHOUT explicit authenticator — ephemeral key is generated
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080})

	// Protected endpoints should reject requests without the ephemeral key
	protectedPaths := []struct {
		method string
		path   string
	}{
		{"GET", "/v1/tools"},
		{"POST", "/v1/execute"},
		{"POST", "/a2a"},
		{"POST", "/v1/mcp"},
	}

	for _, tc := range protectedPaths {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: expected 401 without auth, got %d", tc.method, tc.path, w.Code)
		}
	}
}

// ============================================================
// V6: Cost endpoint — tenant isolation
// ============================================================

func TestCostEndpoint_RequiresAuth(t *testing.T) {
	reg := &mockRegistry{
		tools: []registry.ToolInfo{{Name: "test", Description: "Test"}},
	}
	rtr := &mockRouter{}
	auth := NewAPIKeyAuthenticator("X-API-Key")
	auth.RegisterKey("key-a", &Tenant{ID: "tenant-a", Name: "Tenant A", Tier: "paid"})

	store := a2a.NewMemoryTaskStore(a2a.DefaultTaskStoreConfig())
	defer store.Stop()

	srv := NewServer(reg, rtr, nil, nil, Config{
		Host:          "localhost",
		Port:          8080,
		Authenticator: auth,
		TaskStore:     store,
	})

	// No API key → 401
	req := httptest.NewRequest("GET", "/v1/cost/ctx-123", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", w.Code)
	}
}

func TestCostEndpoint_TenantIsolation(t *testing.T) {
	reg := &mockRegistry{
		tools: []registry.ToolInfo{{Name: "test", Description: "Test"}},
	}
	rtr := &mockRouter{}
	auth := NewAPIKeyAuthenticator("X-API-Key")
	auth.RegisterKey("key-a", &Tenant{ID: "tenant-a", Name: "Tenant A", Tier: "paid"})
	auth.RegisterKey("key-b", &Tenant{ID: "tenant-b", Name: "Tenant B", Tier: "paid"})

	store := a2a.NewMemoryTaskStore(a2a.DefaultTaskStoreConfig())
	defer store.Stop()

	// Assign context to tenant-a
	store.SetContextTenant("ctx-owned-by-a", "tenant-a")
	store.RecordCost("ctx-owned-by-a", "tool/foo", "pod-1", 100)

	srv := NewServer(reg, rtr, nil, nil, Config{
		Host:          "localhost",
		Port:          8080,
		Authenticator: auth,
		TaskStore:     store,
	})

	// tenant-a can read their own context
	req := httptest.NewRequest("GET", "/v1/cost/ctx-owned-by-a", nil)
	req.Header.Set("X-API-Key", "key-a")
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("tenant-a reading own cost: expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	// tenant-b cannot read tenant-a's context
	req = httptest.NewRequest("GET", "/v1/cost/ctx-owned-by-a", nil)
	req.Header.Set("X-API-Key", "key-b")
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("tenant-b reading tenant-a cost: expected 403, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// ============================================================
// V4: A2A caller verification — tenant isolation for tasks
// ============================================================

// a2aTestServer creates a server wired with A2A support and two tenants.
func a2aTestServer(t *testing.T) (*Server, *a2a.MemoryTaskStore) {
	t.Helper()

	reg := &mockRegistry{
		tools: []registry.ToolInfo{{Name: "research-agent/web_search", Description: "Search"}},
	}
	rtr := &mockRouter{result: json.RawMessage(`{"ok":true}`)}

	// Build agent index from a pod spec with a skill
	agentReg := &mockAgentRegistry{agentPods: []*registry.PodSpec{{
		Name: "research-agent",
		AgentCard: &registry.AgentCardSpec{
			Name:               "Research Agent",
			Description:        "Test",
			Version:            "1.0.0",
			DefaultInputModes:  []string{"text"},
			DefaultOutputModes: []string{"text"},
		},
		Skills: []registry.SkillDef{{
			ID:         "web-research",
			Name:       "Web Research",
			Description: "Search the web",
			MapToTools: []string{"web_search"},
		}},
	}}}
	idx := a2a.NewAgentIndex(agentReg)

	store := a2a.NewMemoryTaskStore(a2a.DefaultTaskStoreConfig())
	t.Cleanup(store.Stop)

	a2aHandler := a2a.NewHandler(store, rtr, idx, nil)

	auth := NewAPIKeyAuthenticator("X-API-Key")
	auth.RegisterKey("key-a", &Tenant{ID: "tenant-a", Name: "Tenant A", Tier: "paid"})
	auth.RegisterKey("key-b", &Tenant{ID: "tenant-b", Name: "Tenant B", Tier: "paid"})

	srv := NewServer(reg, rtr, nil, nil, Config{
		Host:          "localhost",
		Port:          8080,
		Authenticator: auth,
		AgentIndex:    idx,
		A2AHandler:    a2aHandler,
		TaskStore:     store,
	})
	return srv, store
}

// mockAgentRegistry implements registry.Registry for A2A tests (returns agent pods).
type mockAgentRegistry struct {
	agentPods []*registry.PodSpec
}

func (m *mockAgentRegistry) LoadManifests(string) error                { return nil }
func (m *mockAgentRegistry) AllTools() []registry.ToolInfo             { return nil }
func (m *mockAgentRegistry) ResolveTool(string) (*registry.PodSpec, *registry.ToolDef, error) {
	return nil, nil, nil
}
func (m *mockAgentRegistry) GetPodSpec(string) *registry.PodSpec      { return nil }
func (m *mockAgentRegistry) PodsToPreWarm() []*registry.PodSpec       { return nil }
func (m *mockAgentRegistry) Validate() []error                        { return nil }
func (m *mockAgentRegistry) AllAgentPods() []*registry.PodSpec        { return m.agentPods }
func (m *mockAgentRegistry) RegisterManifest(*registry.Manifest) error { return nil }
func (m *mockAgentRegistry) UnregisterPod(string) error               { return nil }

// a2aCall sends a JSON-RPC request to /a2a with the given API key.
func a2aCall(t *testing.T, srv *Server, apiKey, method string, params any) *httptest.ResponseRecorder {
	t.Helper()
	p, _ := json.Marshal(params)
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  json.RawMessage(p),
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	return w
}

// extractTaskID parses the JSON-RPC result to get the task ID.
func extractTaskID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %s", resp.Error.Message)
	}
	var task struct {
		ID string `json:"id"`
	}
	json.Unmarshal(resp.Result, &task)
	if task.ID == "" {
		t.Fatal("expected task ID in response")
	}
	return task.ID
}

// hasJSONRPCError checks if the response contains a JSON-RPC error.
func hasJSONRPCError(w *httptest.ResponseRecorder) bool {
	var resp struct {
		Error *json.RawMessage `json:"error"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp.Error != nil
}

func TestA2A_TaskGet_RequiresTenantOwnership(t *testing.T) {
	srv, _ := a2aTestServer(t)

	// Tenant-A creates a task
	w := a2aCall(t, srv, "key-a", "message/send", map[string]any{
		"message": map[string]any{
			"role":  "user",
			"parts": []map[string]any{{"kind": "text", "text": "search for Go"}},
		},
		"metadata":  map[string]any{"skillId": "web-research"},
		"contextId": "ctx-owned-by-a",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create task: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	taskID := extractTaskID(t, w)

	// Tenant-A can read their own task
	w = a2aCall(t, srv, "key-a", "tasks/get", map[string]string{"id": taskID})
	if w.Code != http.StatusOK {
		t.Fatalf("tenant-a get: expected 200, got %d", w.Code)
	}
	if hasJSONRPCError(w) {
		t.Fatal("tenant-a should be able to read their own task")
	}

	// Tenant-B cannot read tenant-A's task
	w = a2aCall(t, srv, "key-b", "tasks/get", map[string]string{"id": taskID})
	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 (error is in JSON-RPC layer), got %d", w.Code)
	}
	if !hasJSONRPCError(w) {
		t.Fatal("tenant-b should NOT be able to read tenant-a's task")
	}
}

func TestA2A_TaskCancel_RequiresTenantOwnership(t *testing.T) {
	srv, _ := a2aTestServer(t)

	// Tenant-A creates a task (it will complete immediately with the mock router,
	// but we need a working-state task for cancel). Create directly in the store.
	// Actually, message/send completes the task. For cancel we need a working task.
	// Create a task directly and set the context-tenant mapping.
	store := srv.taskStore.(*a2a.MemoryTaskStore)
	store.Create(&a2a.Task{
		ID:        "task-for-cancel",
		ContextID: "ctx-cancel-test",
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
	})
	store.SetContextTenant("ctx-cancel-test", "tenant-a")

	// Tenant-A can cancel their own task
	w := a2aCall(t, srv, "key-a", "tasks/cancel", map[string]string{"id": "task-for-cancel"})
	if w.Code != http.StatusOK {
		t.Fatalf("tenant-a cancel: expected 200, got %d", w.Code)
	}
	if hasJSONRPCError(w) {
		t.Fatal("tenant-a should be able to cancel their own task")
	}

	// Create another task for the cross-tenant test
	store.Create(&a2a.Task{
		ID:        "task-for-cancel-2",
		ContextID: "ctx-cancel-test-2",
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
	})
	store.SetContextTenant("ctx-cancel-test-2", "tenant-a")

	// Tenant-B cannot cancel tenant-A's task
	w = a2aCall(t, srv, "key-b", "tasks/cancel", map[string]string{"id": "task-for-cancel-2"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 (error is in JSON-RPC layer), got %d", w.Code)
	}
	if !hasJSONRPCError(w) {
		t.Fatal("tenant-b should NOT be able to cancel tenant-a's task")
	}

	// Verify the task was NOT canceled
	task, _ := store.Get("task-for-cancel-2")
	if task.Status.State == a2a.TaskStateCanceled {
		t.Fatal("task should NOT have been canceled by unauthorized tenant")
	}
}

func TestServer_NoAuth_PublicEndpointsStillWork(t *testing.T) {
	reg := &mockRegistry{
		tools: []registry.ToolInfo{{Name: "test", Description: "Test"}},
	}
	rtr := &mockRouter{}

	// Server without explicit auth
	srv := NewServer(reg, rtr, nil, nil, Config{Host: "localhost", Port: 8080})

	// Public endpoints should still work
	publicPaths := []string{"/health", "/ready", "/.well-known/agent-card.json"}
	for _, path := range publicPaths {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)

		if w.Code == http.StatusUnauthorized {
			t.Errorf("GET %s: public endpoint should not require auth, got 401", path)
		}
	}
}
