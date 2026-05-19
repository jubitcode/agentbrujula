package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"io"
	"os"
	"os/exec"
	"path/filepath"

	"mcp-meta/internal/a2a"
	"mcp-meta/internal/correlation"
	"mcp-meta/internal/deploy"
	"mcp-meta/internal/jsonrpc"
	"mcp-meta/internal/registry"
)

// Health Endpoints

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// Check if registry has tools loaded
	tools := s.registry.AllTools()
	if len(tools) == 0 {
		s.writeError(w, http.StatusServiceUnavailable, "no tools loaded")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ready",
		"tool_count": len(tools),
	})
}

// Registry API

// ToolResponse is the API representation of a tool.
type ToolResponse struct {
	Namespace   string          `json:"namespace"`
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	Description string          `json:"description"`
	Version     string          `json:"version,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Pricing     *PricingInfo    `json:"pricing,omitempty"`
	Stats       *ToolStats      `json:"stats,omitempty"`
}

// PricingInfo represents tool pricing.
type PricingInfo struct {
	Model        string  `json:"model"` // "free", "per-call", "subscription"
	PricePerCall float64 `json:"pricePerCall,omitempty"`
	FreeQuota    int     `json:"freeQuota,omitempty"`
}

// ToolStats represents usage statistics.
type ToolStats struct {
	TotalCalls   int64   `json:"totalCalls"`
	AvgLatencyMs float64 `json:"avgLatencyMs"`
}

// ListToolsResponse is the response for listing tools.
type ListToolsResponse struct {
	Tools      []ToolResponse `json:"tools"`
	TotalCount int            `json:"totalCount"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	query := r.URL.Query().Get("q")
	namespace := r.URL.Query().Get("namespace")

	allTools := s.registry.AllTools()
	var filtered []registry.ToolInfo

	for _, tool := range allTools {
		// Filter by query (search in name and description)
		if query != "" {
			if !strings.Contains(strings.ToLower(tool.Name), strings.ToLower(query)) &&
				!strings.Contains(strings.ToLower(tool.Description), strings.ToLower(query)) {
				continue
			}
		}

		// Filter by namespace
		if namespace != "" {
			parts := strings.SplitN(tool.Name, "/", 2)
			if len(parts) < 2 || parts[0] != namespace {
				continue
			}
		}

		filtered = append(filtered, tool)
	}

	// Convert to response format
	tools := make([]ToolResponse, 0, len(filtered))
	for _, tool := range filtered {
		tr := ToolResponse{
			Name:        tool.Name,
			Slug:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		}

		// Extract namespace if present (supports both "/" and "." separators)
		// mcp-meta uses "pod.tool" format, marketplaces might use "namespace/name"
		if parts := strings.SplitN(tool.Name, "/", 2); len(parts) == 2 {
			tr.Namespace = parts[0]
			tr.Name = parts[1]
			tr.Slug = parts[0] + "/" + parts[1]
		} else if parts := strings.SplitN(tool.Name, ".", 2); len(parts) == 2 {
			// Handle pod.tool format
			tr.Namespace = parts[0]
			tr.Name = parts[1]
			tr.Slug = parts[0] + "/" + parts[1]
		}

		tools = append(tools, tr)
	}

	s.writeJSON(w, http.StatusOK, ListToolsResponse{
		Tools:      tools,
		TotalCount: len(tools),
	})
}

// PublishToolRequest is the request body for publishing a tool.
type PublishToolRequest struct {
	Manifest string `json:"manifest"` // YAML manifest content
}

func (s *Server) handlePublishTool(w http.ResponseWriter, r *http.Request) {
	var req PublishToolRequest
	if err := s.parseJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	if req.Manifest == "" {
		s.writeError(w, http.StatusBadRequest, "manifest is required")
		return
	}

	// Parse and validate manifest
	manifest, err := registry.ParseManifest([]byte(req.Manifest))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid manifest: "+err.Error())
		return
	}

	// Validate for publishing
	if errs := registry.ValidateForPublish(manifest); len(errs) > 0 {
		messages := make([]string, len(errs))
		for i, e := range errs {
			messages[i] = e.Error()
		}
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "validation failed",
			"details": messages,
		})
		return
	}

	// TODO: Store manifest in registry database
	// For now, just validate and return success

	s.writeJSON(w, http.StatusCreated, map[string]any{
		"slug":    manifest.Slug(),
		"version": manifest.Metadata.Version,
		"message": "tool published successfully",
	})
}

func (s *Server) handleGetTool(w http.ResponseWriter, r *http.Request) {
	namespace, name := extractPathParams(r)
	if namespace == "" || name == "" {
		s.writeError(w, http.StatusBadRequest, "namespace and name are required")
		return
	}

	// Try multiple formats: "namespace/name", "namespace.name", just "name"
	toolName := namespace + "/" + name
	podSpec, toolDef, err := s.registry.ResolveTool(toolName)
	if err != nil {
		// Try dot format (pod.tool)
		toolName = namespace + "." + name
		podSpec, toolDef, err = s.registry.ResolveTool(toolName)
		if err != nil {
			// Try without namespace
			podSpec, toolDef, err = s.registry.ResolveTool(name)
			if err != nil {
				s.writeError(w, http.StatusNotFound, "tool not found: "+namespace+"/"+name)
				return
			}
		}
	}

	resp := ToolResponse{
		Namespace:   namespace,
		Name:        toolDef.Name,
		Slug:        toolName,
		Description: toolDef.Description,
		InputSchema: toolDef.InputSchema,
	}

	// Add version from contract if available
	if toolDef.Contract != nil {
		resp.Version = toolDef.Contract.Version
	}

	// Add pricing if available (from pod spec marketplace config)
	if podSpec != nil {
		// Could extract pricing info from marketplace spec
		_ = podSpec
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetToolVersions(w http.ResponseWriter, r *http.Request) {
	namespace, name := extractPathParams(r)
	if namespace == "" || name == "" {
		s.writeError(w, http.StatusBadRequest, "namespace and name are required")
		return
	}

	// Try multiple formats: "namespace/name", "namespace.name"
	toolName := namespace + "/" + name
	_, toolDef, err := s.registry.ResolveTool(toolName)
	if err != nil {
		toolName = namespace + "." + name
		_, toolDef, err = s.registry.ResolveTool(toolName)
		if err != nil {
			s.writeError(w, http.StatusNotFound, "tool not found: "+namespace+"/"+name)
			return
		}
	}

	versions := []map[string]any{
		{
			"version":   "latest",
			"published": time.Now().UTC().Format(time.RFC3339),
		},
	}

	if toolDef.Contract != nil && toolDef.Contract.Version != "" {
		versions[0]["version"] = toolDef.Contract.Version
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"tool":     toolName,
		"versions": versions,
	})
}

// Execution API

// ExecuteRequest is the request body for tool execution.
type ExecuteRequest struct {
	Tool      string          `json:"tool"`                // Tool name (e.g., "namespace/tool")
	Arguments json.RawMessage `json:"arguments,omitempty"` // Tool arguments
	Timeout   int             `json:"timeout,omitempty"`   // Timeout in seconds (max 300)
}

// ExecuteResponse is the response from tool execution.
type ExecuteResponse struct {
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	Duration  int64           `json:"durationMs"`
	RequestID string          `json:"requestId"`
}

func (s *Server) handleExecute(w http.ResponseWriter, r *http.Request) {
	var req ExecuteRequest
	if err := s.parseJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	if req.Tool == "" {
		s.writeError(w, http.StatusBadRequest, "tool is required")
		return
	}

	// Set timeout (default 120s, max 600s — must accommodate LLM calls)
	timeout := 120 * time.Second
	if req.Timeout > 0 && req.Timeout <= 600 {
		timeout = time.Duration(req.Timeout) * time.Second
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	start := time.Now()
	requestID := correlation.FromContext(r.Context())
	if requestID == "" {
		requestID = fmt.Sprintf("req_%d", time.Now().UnixNano())
	}

	// Try the tool name as provided first
	toolName := req.Tool
	result, err := s.router.CallTool(ctx, toolName, req.Arguments)

	// If not found and contains "/", try with "." instead
	if err != nil && strings.Contains(req.Tool, "/") {
		altToolName := strings.Replace(req.Tool, "/", ".", 1)
		result, err = s.router.CallTool(ctx, altToolName, req.Arguments)
	}

	resp := ExecuteResponse{
		Duration:  time.Since(start).Milliseconds(),
		RequestID: requestID,
	}

	if err != nil {
		// Check for JSON-RPC error
		var jsonErr *jsonrpc.Error
		if errors.As(err, &jsonErr) {
			resp.Error = jsonErr.Message
			s.writeJSON(w, http.StatusOK, resp) // Return 200 with error in body
			return
		}
		resp.Error = err.Error()
		s.writeJSON(w, http.StatusOK, resp)
		return
	}

	resp.Result = result
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCallTool(w http.ResponseWriter, r *http.Request) {
	namespace, name := extractPathParams(r)
	if namespace == "" || name == "" {
		s.writeError(w, http.StatusBadRequest, "namespace and name are required")
		return
	}

	// Parse arguments from request body
	var arguments json.RawMessage
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&arguments); err != nil {
			// Empty body is OK
			arguments = json.RawMessage("{}")
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Try multiple formats: "namespace/name", "namespace.name"
	toolName := namespace + "/" + name
	start := time.Now()
	result, err := s.router.CallTool(ctx, toolName, arguments)
	if err != nil {
		// Try dot format
		toolName = namespace + "." + name
		start = time.Now()
		result, err = s.router.CallTool(ctx, toolName, arguments)
	}

	resp := ExecuteResponse{
		Duration:  time.Since(start).Milliseconds(),
		RequestID: fmt.Sprintf("req_%d", time.Now().UnixNano()),
	}

	if err != nil {
		if jsonErr, ok := err.(*jsonrpc.Error); ok {
			resp.Error = jsonErr.Message
		} else {
			resp.Error = err.Error()
		}
		s.writeJSON(w, http.StatusOK, resp)
		return
	}

	resp.Result = result
	s.writeJSON(w, http.StatusOK, resp)
}

// MCP Protocol

func (s *Server) handleMCPSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Send endpoint event (MCP protocol)
	endpoint := fmt.Sprintf("/v1/mcp?session=%s", r.URL.Query().Get("session"))
	_, err := fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpoint)
	if err != nil {
		return
	}
	flusher.Flush()

	// Keep connection open until context is cancelled
	<-r.Context().Done()
}

func (s *Server) handleMCPMessage(w http.ResponseWriter, r *http.Request) {
	if s.mcpHandler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "MCP handler not configured")
		return
	}

	var req jsonrpc.Request
	if err := s.parseJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON-RPC request: "+err.Error())
		return
	}

	resp, err := s.mcpHandler.Handle(r.Context(), &req)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if resp == nil {
		// Notification - no response
		w.WriteHeader(http.StatusNoContent)
		return
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// Admin Endpoints

func (s *Server) handlePoolStatus(w http.ResponseWriter, r *http.Request) {
	status := s.router.GetWarmPoolStatus()
	if status == nil {
		s.writeJSON(w, http.StatusOK, map[string]any{
			"message": "warm pool manager not enabled",
			"pools":   []any{},
		})
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"pools": status,
	})
}

// A2A Endpoints

func (s *Server) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	if s.agentIndex == nil || !s.agentIndex.HasAgents() {
		s.writeError(w, http.StatusNotFound, "no agent pods configured")
		return
	}

	baseURL := fmt.Sprintf("http://%s", r.Host)
	card := s.agentIndex.CompositeCard(baseURL)
	if card == nil {
		s.writeError(w, http.StatusNotFound, "no agent card available")
		return
	}

	s.writeJSON(w, http.StatusOK, card)
}

func (s *Server) handleA2AJSONRPC(w http.ResponseWriter, r *http.Request) {
	if s.a2aHandler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "A2A not configured")
		return
	}

	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	if req.Method == "" {
		s.writeError(w, http.StatusBadRequest, "method is required")
		return
	}

	// Build a jsonrpc.Request for the A2A handler
	var id jsonrpc.ID
	if req.ID != nil {
		err := json.Unmarshal(req.ID, &id)
		if err != nil {
			return
		}
	}

	jrpcReq := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  req.Method,
		Params:  req.Params,
	}

	// Propagate tenant ID into A2A context for cost isolation (V6).
	ctx := r.Context()
	if tenant := GetTenant(ctx); tenant != nil {
		ctx = a2a.WithTenantID(ctx, tenant.ID)
	}

	resp, err := s.a2aHandler.Handle(ctx, jrpcReq)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, resp)
}

//  Marketplace Endpoints

func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	if s.deployer == nil {
		s.writeError(w, http.StatusServiceUnavailable, "marketplace not configured")
		return
	}

	if err := r.ParseMultipartForm(50 << 20); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid multipart form: "+err.Error())
		return
	}

	manifestFile, _, err := r.FormFile("manifest")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "manifest file required")
		return
	}
	defer manifestFile.Close()

	manifestData, err := io.ReadAll(manifestFile)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "read manifest: "+err.Error())
		return
	}

	tmpDir, err := os.MkdirTemp("", "mcp-meta-deploy-*")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "create temp dir: "+err.Error())
		return
	}
	defer func(path string) {
		err := os.RemoveAll(path)
		if err != nil {
			return
		}
	}(tmpDir)

	err = os.WriteFile(filepath.Join(tmpDir, "manifest.yaml"), manifestData, 0644)
	if err != nil {
		return
	}

	// Save ALL uploaded files to tmpDir (any field name, any number of files)
	if r.MultipartForm != nil {
		for fieldName, fileHeaders := range r.MultipartForm.File {
			if fieldName == "manifest" {
				continue // already handled
			}
			for _, fh := range fileHeaders {
				if fh.Filename == "" {
					continue
				}
				f, err := fh.Open()
				if err != nil {
					continue
				}
				data, _ := io.ReadAll(f)
				f.Close()

				// Sanitize filename to prevent path traversal (P1-7)
				fileName := filepath.Base(fh.Filename)
				if fileName == "." || fileName == ".." || fileName == "" {
					continue
				}

				// Extract tarballs
				if strings.HasSuffix(fileName, ".tar.gz") || strings.HasSuffix(fileName, ".tgz") {
					codePath := filepath.Join(tmpDir, "code.tar.gz")
					err := os.WriteFile(codePath, data, 0644)
					if err != nil {
						return
					}
					cmd := exec.Command("tar", "xzf", codePath, "-C", tmpDir)
					if output, err := cmd.CombinedOutput(); err != nil {
						s.writeError(w, http.StatusBadRequest, fmt.Sprintf("extract code: %v\n%s", err, output))
						return
					}
					// Validate no extracted files escape tmpDir
					if err := validateNoPathTraversal(tmpDir); err != nil {
						s.writeError(w, http.StatusBadRequest, fmt.Sprintf("unsafe archive: %v", err))
						return
					}
				} else {
					err := os.WriteFile(filepath.Join(tmpDir, fileName), data, 0644)
					if err != nil {
						return
					}
				}
			}
		}
	}

	result := s.deployer.Deploy(r.Context(), deploy.Input{
		SourceDir:    tmpDir,
		ManifestData: manifestData,
	})

	status := http.StatusOK
	if result.Status == deploy.DeployStatusFailed {
		status = http.StatusUnprocessableEntity
	}
	s.writeJSON(w, status, result)
}

func (s *Server) handleListDeployedAgents(w http.ResponseWriter, r *http.Request) {
	if s.deployer == nil {
		s.writeError(w, http.StatusServiceUnavailable, "marketplace not configured")
		return
	}
	tools := s.registry.AllTools()
	agents := s.registry.AllAgentPods()
	s.writeJSON(w, http.StatusOK, map[string]any{
		"tools":  len(tools),
		"agents": len(agents),
	})
}

func (s *Server) handleGetDeployedAgent(w http.ResponseWriter, r *http.Request) {
	if s.deployer == nil {
		s.writeError(w, http.StatusServiceUnavailable, "marketplace not configured")
		return
	}
	name := r.PathValue("name")
	spec := s.registry.GetPodSpec(name)
	if spec == nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("agent %q not found", name))
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"name":     spec.Name,
		"language": spec.Language,
		"tools":    len(spec.Tools),
		"skills":   len(spec.Skills),
	})
}

func (s *Server) handleUndeployAgent(w http.ResponseWriter, r *http.Request) {
	if s.deployer == nil {
		s.writeError(w, http.StatusServiceUnavailable, "marketplace not configured")
		return
	}
	name := r.PathValue("name")
	if err := s.deployer.Undeploy(name); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "undeployed", "agent": name})
}

//  Task History

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	if s.taskStore == nil {
		s.writeError(w, http.StatusServiceUnavailable, "task store not configured")
		return
	}

	contextID := r.URL.Query().Get("contextId")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		sscanf, err := fmt.Sscanf(limitStr, "%d", &limit)
		if err != nil {
			return
		}
	}

	var tasks []*a2a.Task
	var err error

	if contextID != "" {
		tasks, err = s.taskStore.ListByContext(contextID)
	} else {
		tasks, err = s.taskStore.ListRecent(limit)
	}

	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"tasks": tasks,
		"count": len(tasks),
	})
}

//  A2A SSE Stream

func (s *Server) handleA2AStream(w http.ResponseWriter, r *http.Request) {
	if s.a2aHandler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "A2A not configured")
		return
	}

	// Parse the request body (same as message/send but returns SSE)
	var req struct {
		Message   json.RawMessage `json:"message"`
		Metadata  map[string]any  `json:"metadata,omitempty"`
		ContextID string          `json:"contextId,omitempty"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Send initial event
	writeSSE(w, flusher, "step", `{"step":"submitted","detail":"Task received"}`)

	// Build JSON-RPC request for A2A handler
	params, _ := json.Marshal(map[string]any{
		"message":   req.Message,
		"metadata":  req.Metadata,
		"contextId": req.ContextID,
	})

	jrpcReq := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "message/send",
		Params:  params,
	}

	writeSSE(w, flusher, "step", `{"step":"working","detail":"Processing..."}`)

	// Resolve which pool to subscribe for progress (e.g. "adr-moderator" for debates)
	skillID := ""
	if req.Metadata != nil {
		if sid, ok := req.Metadata["skillId"].(string); ok {
			skillID = sid
		}
	}

	// Subscribe to progress from the moderator pool if this is a debate
	var progressCh <-chan json.RawMessage
	var cancelProgress func()
	if skillID == "adr-debate" {
		progressCh, cancelProgress = s.router.SubscribePoolProgress("adr-moderator")
	}
	if cancelProgress != nil {
		defer cancelProgress()
	}

	// Run A2A in goroutine, forward progress events while waiting
	type a2aResult struct {
		resp *jsonrpc.Response
		err  error
	}
	resultCh := make(chan a2aResult, 1)
	go func() {
		resp, err := s.a2aHandler.Handle(r.Context(), jrpcReq)
		resultCh <- a2aResult{resp, err}
	}()

	// Stream progress events until A2A completes
	for {
		select {
		case evt, ok := <-progressCh:
			if !ok {
				progressCh = nil // channel closed, stop reading
				continue
			}
			writeSSE(w, flusher, "progress", string(evt))

		case result := <-resultCh:
			// A2A completed
			if result.err != nil {
				writeSSE(w, flusher, "error", fmt.Sprintf(`{"error":"%s"}`, result.err.Error()))
				return
			}
			if result.resp.Error != nil {
				errJSON, _ := json.Marshal(result.resp.Error)
				writeSSE(w, flusher, "error", string(errJSON))
				return
			}
			resultJSON, _ := json.Marshal(result.resp.Result)
			writeSSE(w, flusher, "step", `{"step":"complete","detail":"Done"}`)
			writeSSE(w, flusher, "result", string(resultJSON))
			return

		case <-r.Context().Done():
			return
		}
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	flusher.Flush()
}

// validateNoPathTraversal walks the directory and ensures no file paths escape it.
// This prevents tarball path traversal attacks (e.g., entries like "../../etc/crontab").
func validateNoPathTraversal(dir string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve dir: %w", err)
	}
	return filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absDir, path)
		if err != nil {
			return fmt.Errorf("path %q escapes extraction directory", path)
		}
		if strings.HasPrefix(rel, "..") {
			return fmt.Errorf("path %q escapes extraction directory", path)
		}
		return nil
	})
}

//  Cost Tracking Endpoints

func (s *Server) handleGetCost(w http.ResponseWriter, r *http.Request) {
	contextID := r.PathValue("contextID")
	if contextID == "" {
		s.writeError(w, http.StatusBadRequest, "contextID is required")
		return
	}

	// The task store implements CostReader if it's a MemoryTaskStore
	ct, ok := s.taskStore.(a2a.CostReader)
	if !ok || ct == nil {
		s.writeError(w, http.StatusNotImplemented, "cost tracking not available")
		return
	}

	// Tenant isolation: verify the authenticated tenant owns this contextID.
	if tr, ok := s.taskStore.(a2a.TenantReader); ok {
		tenant := GetTenant(r.Context())
		ownerID := tr.GetContextTenant(contextID)
		if ownerID != "" && (tenant == nil || tenant.ID != ownerID) {
			s.writeError(w, http.StatusForbidden, "access denied: context belongs to another tenant")
			return
		}
	}

	ledger := ct.GetCost(contextID)
	if ledger == nil {
		s.writeJSON(w, http.StatusOK, map[string]any{
			"context_id":    contextID,
			"total_stroops": 0,
			"total_usdc":    "0",
			"entries":       []any{},
			"by_agent":      map[string]int64{},
		})
		return
	}

	totalUSDC := fmt.Sprintf("%.7f", float64(ledger.TotalStroops)/10_000_000)

	s.writeJSON(w, http.StatusOK, map[string]any{
		"context_id":    ledger.ContextID,
		"total_stroops": ledger.TotalStroops,
		"total_usdc":    totalUSDC,
		"entries":       ledger.Entries,
		"by_agent":      ledger.ByAgent(),
	})
}
