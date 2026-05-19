package a2a

import (
	"context"
	"encoding/json"
	"testing"

	"mcp-meta/internal/jsonrpc"
	"mcp-meta/internal/registry"
	"mcp-meta/internal/router"
	"mcp-meta/internal/supervisor"
)

// mockRouter implements router.Router for testing.
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

func (m *mockRouter) SetWarmPoolManager(wpm *supervisor.WarmPoolManager)  {}
func (m *mockRouter) GetWarmPoolManager() *supervisor.WarmPoolManager     { return nil }
func (m *mockRouter) GetWarmPoolStatus() map[string]*supervisor.WarmPoolStatus { return nil }
func (m *mockRouter) SetCostTracker(ct router.CostTracker) {}
func (m *mockRouter) SubscribePoolProgress(_ string) (<-chan json.RawMessage, func()) {
	ch := make(chan json.RawMessage); close(ch); return ch, func() {}
}

func newTestHandler(routerResult json.RawMessage, routerErr error) *Handler {
	reg := &mockRegistry{
		agentPods: []*registry.PodSpec{testPodSpec()},
	}
	idx := NewAgentIndex(reg)
	store := NewMemoryTaskStore(DefaultTaskStoreConfig())

	rtr := &mockRouter{result: routerResult, err: routerErr}
	return NewHandler(store, rtr, idx, nil)
}

func TestHandler_MessageSend_Success(t *testing.T) {
	h := newTestHandler(json.RawMessage(`{"results":["found stuff"]}`), nil)
	defer h.store.(*MemoryTaskStore).Stop()

	params, _ := json.Marshal(SendMessageParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Kind: "text", Text: "search for Go patterns"}},
		},
		Metadata:  map[string]any{"skillId": "web-research"},
		ContextID: "ctx-test",
	})

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "message/send",
		Params:  params,
	}

	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("response error: %v", resp.Error.Message)
	}

	var task Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		t.Fatalf("unmarshal task: %v", err)
	}

	if task.ID == "" {
		t.Error("expected task ID")
	}
	if task.ContextID != "ctx-test" {
		t.Errorf("contextID = %q, want ctx-test", task.ContextID)
	}
	if task.Status.State != TaskStateCompleted {
		t.Errorf("state = %q, want completed", task.Status.State)
	}
	if len(task.Artifacts) == 0 {
		t.Error("expected artifacts")
	}
	if len(task.History) == 0 {
		t.Error("expected history with original message")
	}
}

func TestHandler_MessageSend_ToolFailure(t *testing.T) {
	h := newTestHandler(nil, &jsonrpc.Error{Code: jsonrpc.CodePodCrashed, Message: "pod crashed"})
	defer h.store.(*MemoryTaskStore).Stop()

	params, _ := json.Marshal(SendMessageParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Kind: "text", Text: "do something"}},
		},
		Metadata: map[string]any{"skillId": "web-research"},
	})

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "message/send",
		Params:  params,
	}

	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error.Message)
	}

	var task Task
	json.Unmarshal(resp.Result, &task)

	if task.Status.State != TaskStateFailed {
		t.Errorf("state = %q, want failed", task.Status.State)
	}
}

func TestHandler_MessageSend_AutoContextID(t *testing.T) {
	h := newTestHandler(json.RawMessage(`{}`), nil)
	defer h.store.(*MemoryTaskStore).Stop()

	params, _ := json.Marshal(SendMessageParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Kind: "text", Text: "hello"}},
		},
		Metadata: map[string]any{"skillId": "web-research"},
		// No ContextID — should auto-generate
	})

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "message/send",
		Params:  params,
	}

	resp, _ := h.Handle(context.Background(), req)
	var task Task
	json.Unmarshal(resp.Result, &task)

	if task.ContextID == "" {
		t.Error("expected auto-generated contextID")
	}
}

func TestHandler_MessageSend_EmptyParts(t *testing.T) {
	h := newTestHandler(json.RawMessage(`{}`), nil)
	defer h.store.(*MemoryTaskStore).Stop()

	params, _ := json.Marshal(SendMessageParams{
		Message:  Message{Role: "user", Parts: []Part{}},
		Metadata: map[string]any{"skillId": "web-research"},
	})

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "message/send",
		Params:  params,
	}

	resp, _ := h.Handle(context.Background(), req)
	if resp.Error == nil {
		t.Fatal("expected error for empty parts")
	}
}

func TestHandler_MessageSend_UnknownSkill(t *testing.T) {
	// The handler is deliberately permissive: when skillId can't be
	// resolved, it falls back to inferring a skill from the message text.
	// So an unknown skillId paired with a non-empty message succeeds via
	// the fallback. This test verifies that fallback path; the error path
	// is covered by `TestHandler_MessageSend_EmptyParts` (which has no
	// text to infer from).
	h := newTestHandler(json.RawMessage(`{}`), nil)
	defer h.store.(*MemoryTaskStore).Stop()

	params, _ := json.Marshal(SendMessageParams{
		Message:  Message{Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}}},
		Metadata: map[string]any{"skillId": "nonexistent-skill"},
	})

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "message/send",
		Params:  params,
	}

	resp, _ := h.Handle(context.Background(), req)
	if resp.Error != nil {
		t.Fatalf("expected fallback inference to succeed, got error: %v", resp.Error)
	}
}

func TestHandler_TasksGet_Success(t *testing.T) {
	h := newTestHandler(json.RawMessage(`{}`), nil)
	defer h.store.(*MemoryTaskStore).Stop()

	// Create a task first
	h.store.Create(&Task{
		ID:        "task-123",
		ContextID: "ctx-1",
		Status:    TaskStatus{State: TaskStateWorking},
	})

	params, _ := json.Marshal(TasksGetParams{ID: "task-123"})
	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(2),
		Method:  "tasks/get",
		Params:  params,
	}

	resp, _ := h.Handle(context.Background(), req)
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}

	var task Task
	json.Unmarshal(resp.Result, &task)
	if task.ID != "task-123" {
		t.Errorf("ID = %q", task.ID)
	}
	if task.Status.State != TaskStateWorking {
		t.Errorf("state = %q", task.Status.State)
	}
}

func TestHandler_TasksGet_NotFound(t *testing.T) {
	h := newTestHandler(json.RawMessage(`{}`), nil)
	defer h.store.(*MemoryTaskStore).Stop()

	params, _ := json.Marshal(TasksGetParams{ID: "nonexistent"})
	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(2),
		Method:  "tasks/get",
		Params:  params,
	}

	resp, _ := h.Handle(context.Background(), req)
	if resp.Error == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestHandler_TasksCancel(t *testing.T) {
	h := newTestHandler(json.RawMessage(`{}`), nil)
	defer h.store.(*MemoryTaskStore).Stop()

	h.store.Create(&Task{
		ID:     "task-cancel",
		Status: TaskStatus{State: TaskStateWorking},
	})

	params, _ := json.Marshal(TasksCancelParams{ID: "task-cancel"})
	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(3),
		Method:  "tasks/cancel",
		Params:  params,
	}

	resp, _ := h.Handle(context.Background(), req)
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}

	var task Task
	json.Unmarshal(resp.Result, &task)
	if task.Status.State != TaskStateCanceled {
		t.Errorf("state = %q, want canceled", task.Status.State)
	}
}

func TestHandler_MessageSend_ContextIDPropagated(t *testing.T) {
	// Verify that contextId is set on the context passed to CallTool
	var capturedCtx context.Context
	rtr := &mockRouter{
		result: json.RawMessage(`{}`),
	}
	// Wrap CallTool to capture context
	reg := &mockRegistry{agentPods: []*registry.PodSpec{testPodSpec()}}
	idx := NewAgentIndex(reg)
	store := NewMemoryTaskStore(DefaultTaskStoreConfig())
	defer store.Stop()

	// Use a custom router that captures context
	capturingRouter := &contextCapturingRouter{
		inner:       rtr,
		capturedCtx: &capturedCtx,
	}

	h := NewHandler(store, capturingRouter, idx, nil)

	params, _ := json.Marshal(SendMessageParams{
		Message:   Message{Role: "user", Parts: []Part{{Kind: "text", Text: "test"}}},
		Metadata:  map[string]any{"skillId": "web-research"},
		ContextID: "ctx-propagation-test",
	})

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "message/send",
		Params:  params,
	}

	h.Handle(context.Background(), req)

	if capturedCtx == nil {
		t.Fatal("expected context to be captured")
	}

	gotContextID := router.GetContextID(capturedCtx)
	if gotContextID != "ctx-propagation-test" {
		t.Errorf("contextId = %q, want %q", gotContextID, "ctx-propagation-test")
	}
}

// contextCapturingRouter wraps a router and captures the context from CallTool.
type contextCapturingRouter struct {
	inner       *mockRouter
	capturedCtx *context.Context
}

func (r *contextCapturingRouter) CallTool(ctx context.Context, toolName string, args json.RawMessage) (json.RawMessage, error) {
	*r.capturedCtx = ctx
	return r.inner.CallTool(ctx, toolName, args)
}

func (r *contextCapturingRouter) InternalCall(ctx context.Context, sourcePod string, targetTool string, args json.RawMessage) (json.RawMessage, error) {
	return r.inner.InternalCall(ctx, sourcePod, targetTool, args)
}

func (r *contextCapturingRouter) InternalCallWithContract(ctx context.Context, sourcePod string, targetTool string, args json.RawMessage, expect *jsonrpc.ContractExpectation) (json.RawMessage, error) {
	return r.inner.InternalCallWithContract(ctx, sourcePod, targetTool, args, expect)
}

func (r *contextCapturingRouter) SetWarmPoolManager(wpm *supervisor.WarmPoolManager) {}
func (r *contextCapturingRouter) GetWarmPoolManager() *supervisor.WarmPoolManager    { return nil }
func (r *contextCapturingRouter) GetWarmPoolStatus() map[string]*supervisor.WarmPoolStatus {
	return nil
}
func (r *contextCapturingRouter) SetCostTracker(ct router.CostTracker) {}
func (r *contextCapturingRouter) SubscribePoolProgress(_ string) (<-chan json.RawMessage, func()) {
	ch := make(chan json.RawMessage); close(ch); return ch, func() {}
}

func TestHandler_UnknownMethod(t *testing.T) {
	h := newTestHandler(json.RawMessage(`{}`), nil)
	defer h.store.(*MemoryTaskStore).Stop()

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "unknown/method",
		Params:  json.RawMessage(`{}`),
	}

	resp, _ := h.Handle(context.Background(), req)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, jsonrpc.CodeMethodNotFound)
	}
}
