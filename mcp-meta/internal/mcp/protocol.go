package mcp

import "encoding/json"

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type ToolResult struct {
	Type     string `json:"type"` // "text", "image", "resource"
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"` // base64 for binary
}

type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo         `json:"clientInfo"`
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
}

// ClientCapabilities Currently empty as we don't use any client capabilities.
type ClientCapabilities struct{}

type ServerCapabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type CallToolResult struct {
	Content []ToolResult `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

func NewTextResult(text string) ToolResult {
	return ToolResult{
		Type: "text",
		Text: text,
	}
}

func NewCallToolResult(text string) *CallToolResult {
	return &CallToolResult{
		Content: []ToolResult{NewTextResult(text)},
		IsError: false,
	}
}

func NewCallToolErrorResult(text string) *CallToolResult {
	return &CallToolResult{
		Content: []ToolResult{NewTextResult(text)},
		IsError: true,
	}
}

func NewCallToolResultFromJSON(result json.RawMessage) *CallToolResult {
	return &CallToolResult{
		Content: []ToolResult{NewTextResult(string(result))},
		IsError: false,
	}
}
