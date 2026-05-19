package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"mcp-meta/internal/jsonrpc"
	"mcp-meta/internal/registry"
	"mcp-meta/internal/router"
	"mcp-meta/internal/supervisor"
)

const Version = "0.1.0"

type Handler struct {
	registry   registry.Registry
	router     router.Router
	supervisor supervisor.Supervisor
	logger     *slog.Logger

	initialized bool
}

func NewHandler(reg registry.Registry, rtr router.Router, sup supervisor.Supervisor, logger *slog.Logger) *Handler {
	return &Handler{
		registry:   reg,
		router:     rtr,
		supervisor: sup,
		logger:     logger,
	}
}

func (h *Handler) Handle(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	if req.ID.IsNull() {
		h.handleNotification(ctx, req)
		return nil, nil
	}

	resp := &jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	result, err := h.dispatch(ctx, req.Method, req.Params)
	if err != nil {
		if jsonErr, ok := err.(*jsonrpc.Error); ok {
			resp.Error = jsonErr
		} else {
			resp.Error = &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: err.Error(),
			}
		}
		return resp, nil
	}

	resp.Result = result
	return resp, nil
}

func (h *Handler) dispatch(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "initialize":
		return h.handleInitialize(ctx, params)
	case "ping":
		return h.handlePing(ctx, params)
	case "tools/list":
		return h.handleToolsList(ctx, params)
	case "tools/call":
		return h.handleToolsCall(ctx, params)
	default:
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeMethodNotFound,
			Message: fmt.Sprintf("unknown method: %s", method),
		}
	}
}

func (h *Handler) handleNotification(_ context.Context, req *jsonrpc.Request) {
	switch req.Method {
	case "notifications/initialized":
		if h.logger != nil {
			h.logger.Info("client initialized")
		}
	case "notifications/cancelled":
		if h.logger != nil {
			h.logger.Debug("request cancelled", "params", string(req.Params))
		}
	default:
		if h.logger != nil {
			h.logger.Debug("unknown notification", "method", req.Method)
		}
	}
}

func (h *Handler) handleInitialize(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
	var initParams InitializeParams
	if params != nil {
		if err := json.Unmarshal(params, &initParams); err != nil {
			return nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInvalidParams,
				Message: fmt.Sprintf("invalid initialize params: %v", err),
			}
		}
	}

	h.initialized = true

	result := InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: ServerCapabilities{
			Tools: &ToolsCapability{
				ListChanged: true,
			},
		},
		ServerInfo: ServerInfo{
			Name:    "mcp-meta",
			Version: Version,
		},
	}

	return json.Marshal(result)
}

func (h *Handler) handlePing(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (h *Handler) handleToolsList(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	toolInfos := h.registry.AllTools()

	tools := make([]Tool, len(toolInfos))
	for i, info := range toolInfos {
		tools[i] = Tool{
			Name:        info.Name,
			Description: info.Description,
			InputSchema: info.InputSchema,
		}
	}

	return json.Marshal(ListToolsResult{Tools: tools})
}

func (h *Handler) handleToolsCall(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var callParams CallToolParams
	if err := json.Unmarshal(params, &callParams); err != nil {
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeInvalidParams,
			Message: fmt.Sprintf("invalid tools/call params: %v", err),
		}
	}

	result, err := h.router.CallTool(ctx, callParams.Name, callParams.Arguments)
	if err != nil {
		if jsonErr, ok := err.(*jsonrpc.Error); ok {
			return nil, jsonErr
		}
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodePodError,
			Message: err.Error(),
		}
	}

	return json.Marshal(NewCallToolResultFromJSON(result))
}
