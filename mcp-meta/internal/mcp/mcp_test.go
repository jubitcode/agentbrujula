package mcp

import (
	"context"
	"encoding/json"
	"testing"

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
	return nil, nil, nil
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

func TestNewHandler(t *testing.T) {
	reg := &mockRegistry{}
	rtr := &mockRouter{}
	h := NewHandler(reg, rtr, nil, nil)

	if h == nil {
		t.Fatal("NewHandler() returned nil")
	}
	if h.registry != reg {
		t.Error("Handler registry not set correctly")
	}
	if h.router != rtr {
		t.Error("Handler router not set correctly")
	}
}

func TestHandler_HandleInitialize(t *testing.T) {
	h := NewHandler(&mockRegistry{}, &mockRouter{}, nil, nil)

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","clientInfo":{"name":"test"}}`),
	}

	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if resp == nil {
		t.Fatal("Handle() returned nil response")
	}
	if resp.Error != nil {
		t.Errorf("Handle() returned error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("Handle() returned nil result")
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if result.ServerInfo.Name != "mcp-meta" {
		t.Errorf("ServerInfo.Name = %q, want %q", result.ServerInfo.Name, "mcp-meta")
	}
	if result.ServerInfo.Version != Version {
		t.Errorf("ServerInfo.Version = %q, want %q", result.ServerInfo.Version, Version)
	}
}

func TestHandler_HandlePing(t *testing.T) {
	h := NewHandler(&mockRegistry{}, &mockRouter{}, nil, nil)

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "ping",
	}

	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if resp.Error != nil {
		t.Errorf("Handle() returned error: %v", resp.Error.Message)
	}
	if string(resp.Result) != "{}" {
		t.Errorf("Handle() result = %s, want {}", string(resp.Result))
	}
}

func TestHandler_HandleToolsList(t *testing.T) {
	reg := &mockRegistry{
		tools: []registry.ToolInfo{
			{Name: "tool1", Description: "Tool 1"},
			{Name: "tool2", Description: "Tool 2"},
		},
	}
	h := NewHandler(reg, &mockRouter{}, nil, nil)

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "tools/list",
	}

	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if resp.Error != nil {
		t.Errorf("Handle() returned error: %v", resp.Error.Message)
	}

	var result ListToolsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if len(result.Tools) != 2 {
		t.Errorf("Got %d tools, want 2", len(result.Tools))
	}
}

func TestHandler_HandleToolsCall(t *testing.T) {
	rtr := &mockRouter{
		result: json.RawMessage(`{"message":"Hello, World!"}`),
	}
	h := NewHandler(&mockRegistry{}, rtr, nil, nil)

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"hello","arguments":{"name":"World"}}`),
	}

	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if resp.Error != nil {
		t.Errorf("Handle() returned error: %v", resp.Error.Message)
	}

	var result CallToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if len(result.Content) == 0 {
		t.Error("CallToolResult has no content")
	}
}

func TestHandler_HandleToolsCall_InvalidParams(t *testing.T) {
	h := NewHandler(&mockRegistry{}, &mockRouter{}, nil, nil)

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "tools/call",
		Params:  json.RawMessage(`{invalid}`),
	}

	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if resp.Error == nil {
		t.Error("Handle() should return error for invalid params")
	}
	if resp.Error.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("Error code = %d, want %d", resp.Error.Code, jsonrpc.CodeInvalidParams)
	}
}

func TestHandler_HandleToolsCall_RouterError(t *testing.T) {
	rtr := &mockRouter{
		err: &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "tool not found"},
	}
	h := NewHandler(&mockRegistry{}, rtr, nil, nil)

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"unknown"}`),
	}

	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if resp.Error == nil {
		t.Error("Handle() should return error from router")
	}
}

func TestHandler_HandleUnknownMethod(t *testing.T) {
	h := NewHandler(&mockRegistry{}, &mockRouter{}, nil, nil)

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "unknown/method",
	}

	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if resp.Error == nil {
		t.Error("Handle() should return error for unknown method")
	}
	if resp.Error.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("Error code = %d, want %d", resp.Error.Code, jsonrpc.CodeMethodNotFound)
	}
}

func TestHandler_HandleNotification(t *testing.T) {
	h := NewHandler(&mockRegistry{}, &mockRouter{}, nil, nil)

	// Notification (null ID)
	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NullID(),
		Method:  "notifications/initialized",
	}

	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if resp != nil {
		t.Error("Handle() should return nil for notifications")
	}
}

// Protocol type tests

func TestNewTextResult(t *testing.T) {
	r := NewTextResult("hello")
	if r.Type != "text" {
		t.Errorf("Type = %q, want %q", r.Type, "text")
	}
	if r.Text != "hello" {
		t.Errorf("Text = %q, want %q", r.Text, "hello")
	}
}

func TestNewCallToolResult(t *testing.T) {
	r := NewCallToolResult("success")
	if r.IsError {
		t.Error("IsError should be false")
	}
	if len(r.Content) != 1 {
		t.Fatalf("Content length = %d, want 1", len(r.Content))
	}
	if r.Content[0].Text != "success" {
		t.Errorf("Content[0].Text = %q, want %q", r.Content[0].Text, "success")
	}
}

func TestNewCallToolErrorResult(t *testing.T) {
	r := NewCallToolErrorResult("error message")
	if !r.IsError {
		t.Error("IsError should be true")
	}
	if len(r.Content) != 1 {
		t.Fatalf("Content length = %d, want 1", len(r.Content))
	}
	if r.Content[0].Text != "error message" {
		t.Errorf("Content[0].Text = %q, want %q", r.Content[0].Text, "error message")
	}
}

func TestNewCallToolResultFromJSON(t *testing.T) {
	data := json.RawMessage(`{"key":"value"}`)
	r := NewCallToolResultFromJSON(data)
	if r.IsError {
		t.Error("IsError should be false")
	}
	if len(r.Content) != 1 {
		t.Fatalf("Content length = %d, want 1", len(r.Content))
	}
	if r.Content[0].Text != `{"key":"value"}` {
		t.Errorf("Content[0].Text = %q, want %q", r.Content[0].Text, `{"key":"value"}`)
	}
}

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Error("Version should not be empty")
	}
}

func TestProtocolTypes_Serialization(t *testing.T) {
	// InitializeResult
	init := InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities:    ServerCapabilities{Tools: &ToolsCapability{ListChanged: true}},
		ServerInfo:      ServerInfo{Name: "test", Version: "1.0.0"},
	}
	data, err := json.Marshal(init)
	if err != nil {
		t.Errorf("Failed to marshal InitializeResult: %v", err)
	}
	var parsed InitializeResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Errorf("Failed to unmarshal InitializeResult: %v", err)
	}
	if parsed.ServerInfo.Name != "test" {
		t.Error("Round-trip failed for InitializeResult")
	}

	// ListToolsResult
	list := ListToolsResult{
		Tools: []Tool{
			{Name: "tool1", Description: "desc"},
		},
	}
	data, err = json.Marshal(list)
	if err != nil {
		t.Errorf("Failed to marshal ListToolsResult: %v", err)
	}

	// CallToolParams
	params := CallToolParams{
		Name:      "test",
		Arguments: json.RawMessage(`{"arg":"value"}`),
	}
	data, err = json.Marshal(params)
	if err != nil {
		t.Errorf("Failed to marshal CallToolParams: %v", err)
	}

	// CallToolResult
	result := CallToolResult{
		Content: []ToolResult{{Type: "text", Text: "result"}},
		IsError: false,
	}
	data, err = json.Marshal(result)
	if err != nil {
		t.Errorf("Failed to marshal CallToolResult: %v", err)
	}
}
