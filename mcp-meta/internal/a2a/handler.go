package a2a

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"mcp-meta/internal/jsonrpc"
	"mcp-meta/internal/router"
)

// tenantIDContextKey is used to pass the authenticated tenant ID into A2A handlers.
type tenantIDContextKey struct{}

// WithTenantID returns a context carrying the tenant ID for cost isolation.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDContextKey{}, tenantID)
}

// TenantIDFromContext extracts the tenant ID from context, or "" if absent.
func TenantIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(tenantIDContextKey{}).(string); ok {
		return id
	}
	return ""
}

// Handler processes A2A JSON-RPC requests.
type Handler struct {
	store  TaskStore
	router router.Router
	index  *AgentIndex
	logger *slog.Logger
}

// NewHandler creates an A2A handler.
func NewHandler(store TaskStore, rtr router.Router, index *AgentIndex, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		store:  store,
		router: rtr,
		index:  index,
		logger: logger,
	}
}

// Handle dispatches an A2A JSON-RPC request.
func (h *Handler) Handle(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	result, err := h.dispatch(ctx, req.Method, req.Params)
	if err != nil {
		var jsonErr *jsonrpc.Error
		if errors.As(err, &jsonErr) {
			return &jsonrpc.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   jsonErr,
			}, nil
		}
		return &jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   jsonrpc.NewError(jsonrpc.CodeInternalError, err.Error()),
		}, nil
	}

	return &jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}, nil
}

func (h *Handler) dispatch(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "message/send":
		return h.handleMessageSend(ctx, params)
	case "tasks/get":
		return h.handleTasksGet(ctx, params)
	case "tasks/cancel":
		return h.handleTasksCancel(ctx, params)
	default:
		return nil, jsonrpc.NewError(jsonrpc.CodeMethodNotFound, fmt.Sprintf("unknown method: %s", method))
	}
}

// SendMessageParams is the A2A message/send request.
type SendMessageParams struct {
	Message  Message        `json:"message"`
	Metadata map[string]any `json:"metadata,omitempty"`
	// ContextID groups related tasks in a conversation.
	ContextID string `json:"contextId,omitempty"`
}

func (h *Handler) handleMessageSend(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p SendMessageParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, fmt.Sprintf("invalid params: %v", err))
	}

	if len(p.Message.Parts) == 0 {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "message must have at least one part")
	}

	// Extract skill ID from metadata
	skillID, _ := p.Metadata["skillId"].(string)

	// Resolve skill to tool FQN
	var toolFQN string
	var err error
	if skillID != "" {
		toolFQN, err = h.index.ResolveSkill(skillID)
	}
	if toolFQN == "" {
		// Skill not found or not provided — infer from message text
		inferred := h.inferSkill(p.Message)
		toolFQN, err = h.index.ResolveSkill(inferred)
		if err != nil {
			return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, fmt.Sprintf("cannot resolve skill: %v", err))
		}
	}

	// Generate IDs
	taskID := generateID()
	contextID := p.ContextID
	if contextID == "" {
		contextID = generateID()
	}

	// Create task
	task := &Task{
		ID:        taskID,
		ContextID: contextID,
		Status:    TaskStatus{State: TaskStateSubmitted},
		History:   []Message{p.Message},
		Metadata:  p.Metadata,
	}

	if err := h.store.Create(task); err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInternalError, fmt.Sprintf("create task: %v", err))
	}

	// Record context→tenant mapping for cost isolation (V6).
	if tenantID := TenantIDFromContext(ctx); tenantID != "" {
		if ts, ok := h.store.(*MemoryTaskStore); ok {
			ts.SetContextTenant(contextID, tenantID)
		}
	}

	// Transition to working
	if err := h.store.Update(taskID, func(t *Task) error {
		t.Status.State = TaskStateWorking
		return nil
	}); err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInternalError, fmt.Sprintf("update task: %v", err))
	}

	// Build tool arguments from message parts
	args := h.buildToolArgs(p.Message)

	// Propagate contextId so inter-pod calls share the same A2A conversation
	ctx = router.WithContextID(ctx, contextID)

	h.logger.Info("a2a dispatching tool call",
		"task_id", taskID,
		"context_id", contextID,
		"skill", skillID,
		"tool", toolFQN,
	)

	result, callErr := h.router.CallTool(ctx, toolFQN, args)

	if callErr != nil {
		// Mark task as failed
		err := h.store.Update(taskID, func(t *Task) error {
			t.Status.State = TaskStateFailed
			t.Status.Message = &Message{
				Role:  "agent",
				Parts: []Part{{Kind: "text", Text: callErr.Error()}},
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		// Mark task as completed with artifact
		err := h.store.Update(taskID, func(t *Task) error {
			t.Status.State = TaskStateCompleted
			t.Artifacts = []Artifact{{
				ID:   generateID(),
				Name: "result",
				Parts: []Part{{
					Kind: "text",
					Text: string(result),
				}},
			}}
			t.Status.Message = &Message{
				Role:  "agent",
				Parts: []Part{{Kind: "text", Text: "completed"}},
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// Return the final task state
	finalTask, err := h.store.Get(taskID)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInternalError, fmt.Sprintf("get task: %v", err))
	}

	return json.Marshal(finalTask)
}

// TasksGetParams is the A2A tasks/get request.
type TasksGetParams struct {
	ID string `json:"id"`
}

func (h *Handler) handleTasksGet(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p TasksGetParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, fmt.Sprintf("invalid params: %v", err))
	}

	if p.ID == "" {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "task id is required")
	}

	task, err := h.store.Get(p.ID)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInternalError, fmt.Sprintf("task not found: %v", err))
	}

	if err := h.verifyTaskOwnership(ctx, task); err != nil {
		return nil, err
	}

	return json.Marshal(task)
}

// TasksCancelParams is the A2A tasks/cancel request.
type TasksCancelParams struct {
	ID string `json:"id"`
}

func (h *Handler) handleTasksCancel(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p TasksCancelParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, fmt.Sprintf("invalid params: %v", err))
	}

	if p.ID == "" {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "task id is required")
	}

	task, err := h.store.Get(p.ID)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInternalError, fmt.Sprintf("task not found: %v", err))
	}
	if err := h.verifyTaskOwnership(ctx, task); err != nil {
		return nil, err
	}

	err = h.store.Update(p.ID, func(t *Task) error {
		t.Status.State = TaskStateCanceled
		return nil
	})
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInternalError, fmt.Sprintf("cancel task: %v", err))
	}

	task, _ = h.store.Get(p.ID)
	return json.Marshal(task)
}

// verifyTaskOwnership checks that the requesting tenant (from context) owns the
// task via its contextID→tenant mapping. Returns a JSON-RPC error on mismatch.
func (h *Handler) verifyTaskOwnership(ctx context.Context, task *Task) error {
	reqTenant := TenantIDFromContext(ctx)
	if reqTenant == "" {
		// No tenant in context — unauthenticated or internal call; skip check.
		return nil
	}

	if tr, ok := h.store.(TenantReader); ok {
		taskTenant := tr.GetContextTenant(task.ContextID)
		if taskTenant != "" && taskTenant != reqTenant {
			return jsonrpc.NewError(jsonrpc.CodeInvalidParams, "task not found")
		}
	}
	return nil
}

// inferSkill classifies user intent to find the best matching skill.
func (h *Handler) inferSkill(msg Message) string {
	// Extract text from message parts
	var text string
	for _, part := range msg.Parts {
		if part.Kind == "text" && part.Text != "" {
			text += " " + part.Text
		}
	}
	if text == "" {
		// Fallback: first available skill
		for skillID := range h.index.skills {
			return skillID
		}
		return ""
	}
	return h.index.ClassifyIntent(text)
}

// buildToolArgs converts message parts into JSON tool arguments.
func (h *Handler) buildToolArgs(msg Message) json.RawMessage {
	// Extract text from all text parts
	var text string
	for _, part := range msg.Parts {
		if part.Kind == "text" && part.Text != "" {
			if text != "" {
				text += "\n"
			}
			text += part.Text
		}
	}

	args := map[string]any{
		"message":   text,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	data, _ := json.Marshal(args)
	return data
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
