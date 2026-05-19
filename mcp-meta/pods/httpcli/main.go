package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	CodePodError       = -32000
)

var (
	httpcliPath string
	writeMu     sync.Mutex
)

func init() {
	// Try multiple possible names for the httpcli binary
	for _, name := range []string{"http", "httpcli"} {
		path, err := exec.LookPath(name)
		if err == nil {
			httpcliPath = path
			return
		}
	}
	httpcliPath = "" // Will fail gracefully on calls
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			writeResponse(Response{
				JSONRPC: "2.0",
				ID:      nil,
				Error:   &Error{Code: CodeParseError, Message: "Parse error: " + err.Error()},
			})
			continue
		}

		handleRequest(req)
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}
}

func handleRequest(req Request) {
	switch req.Method {
	case "ping":
		handlePing(req)
	case "shutdown":
		handleShutdown(req)
	case "http_get":
		handleHttpGet(req)
	case "http_post":
		handleHttpPost(req)
	case "execute_http_file":
		handleExecuteHttpFile(req)
	case "benchmark":
		handleBenchmark(req)
	case "test_mcp_server":
		handleTestMcpServer(req)
	default:
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodeMethodNotFound, Message: "Method not found: " + req.Method},
		})
	}
}

func handlePing(req Request) {
	result, _ := json.Marshal(map[string]string{"status": "ok", "pod": "httpcli"})
	writeResponse(Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	})
}

func handleShutdown(req Request) {
	result, _ := json.Marshal(map[string]string{"status": "shutting_down"})
	writeResponse(Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	})
	os.Exit(0)
}

// =============================================================================
// Security: URL Validation
// =============================================================================

func isInternalURL(urlStr string) bool {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return true // Block on parse error
	}

	host := parsed.Hostname()

	// Check for localhost variants
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}

	// Check for AWS/GCP/Azure metadata endpoints
	metadataHosts := []string{
		"169.254.169.254",
		"metadata.google.internal",
		"metadata.internal",
	}
	for _, mh := range metadataHosts {
		if host == mh {
			return true
		}
	}

	// Check for private IP ranges
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return true
		}
	}

	return false
}

type HttpGetParams struct {
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers,omitempty"`
	AllowInternal bool              `json:"allow_internal,omitempty"`
	Timeout       int               `json:"timeout,omitempty"`
}

func handleHttpGet(req Request) {
	var params HttpGetParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeResponse(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &Error{Code: CodeInvalidParams, Message: "Invalid params: " + err.Error()},
			})
			return
		}
	}

	// Validate required params first
	if params.URL == "" {
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodeInvalidParams, Message: "Missing required parameter: url"},
		})
		return
	}

	// Security: Block internal URLs unless explicitly allowed
	if !params.AllowInternal && isInternalURL(params.URL) {
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodeInvalidParams, Message: "URL blocked: internal/private addresses not allowed"},
		})
		return
	}

	// Check for httpcli binary
	if httpcliPath == "" {
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodePodError, Message: "httpcli not found in PATH"},
		})
		return
	}

	// Build command
	args := []string{"get", params.URL, "-o", "json"}
	for key, value := range params.Headers {
		args = append(args, "-H", fmt.Sprintf("%s: %s", key, value))
	}
	if params.Timeout > 0 {
		args = append(args, "-t", fmt.Sprintf("%d", params.Timeout))
	}

	cmd := exec.Command(httpcliPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			writeResponse(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &Error{Code: CodePodError, Message: err.Error()},
			})
			return
		}
	}

	result, _ := json.Marshal(map[string]any{
		"exit_code": exitCode,
		"response":  stdout.String(),
		"stderr":    stderr.String(),
	})
	writeResponse(Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	})
}

type HttpPostParams struct {
	URL           string            `json:"url"`
	Body          string            `json:"body,omitempty"`
	ContentType   string            `json:"content_type,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	AllowInternal bool              `json:"allow_internal,omitempty"`
	Timeout       int               `json:"timeout,omitempty"`
}

func handleHttpPost(req Request) {
	var params HttpPostParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeResponse(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &Error{Code: CodeInvalidParams, Message: "Invalid params: " + err.Error()},
			})
			return
		}
	}

	if params.URL == "" {
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodeInvalidParams, Message: "Missing required parameter: url"},
		})
		return
	}

	if !params.AllowInternal && isInternalURL(params.URL) {
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodeInvalidParams, Message: "URL blocked: internal/private addresses not allowed"},
		})
		return
	}

	if httpcliPath == "" {
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodePodError, Message: "httpcli not found in PATH"},
		})
		return
	}

	args := []string{"post", params.URL, "-o", "json"}
	if params.Body != "" {
		args = append(args, "-d", params.Body)
	}
	if params.ContentType != "" {
		args = append(args, "-c", params.ContentType)
	}
	for key, value := range params.Headers {
		args = append(args, "-H", fmt.Sprintf("%s: %s", key, value))
	}
	if params.Timeout > 0 {
		args = append(args, "-t", fmt.Sprintf("%d", params.Timeout))
	}

	cmd := exec.Command(httpcliPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			writeResponse(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &Error{Code: CodePodError, Message: err.Error()},
			})
			return
		}
	}

	result, _ := json.Marshal(map[string]any{
		"exit_code": exitCode,
		"response":  stdout.String(),
		"stderr":    stderr.String(),
	})
	writeResponse(Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	})
}

type ExecuteHttpFileParams struct {
	FilePath    string            `json:"file_path"`
	Variables   map[string]string `json:"variables,omitempty"`
	RequestName string            `json:"request_name,omitempty"`
}

func handleExecuteHttpFile(req Request) {
	var params ExecuteHttpFileParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeResponse(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &Error{Code: CodeInvalidParams, Message: "Invalid params: " + err.Error()},
			})
			return
		}
	}

	// Validate required params first
	if params.FilePath == "" {
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodeInvalidParams, Message: "Missing required parameter: file_path"},
		})
		return
	}

	// Security: Validate file path
	if strings.Contains(params.FilePath, "..") {
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodeInvalidParams, Message: "Path traversal not allowed"},
		})
		return
	}

	// Check for httpcli binary
	if httpcliPath == "" {
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodePodError, Message: "httpcli not found in PATH"},
		})
		return
	}

	args := []string{"--file", params.FilePath, "-o", "json"}
	for key, value := range params.Variables {
		args = append(args, "--var", fmt.Sprintf("%s=%s", key, value))
	}
	if params.RequestName != "" {
		args = append(args, "--request-name", params.RequestName)
	}

	cmd := exec.Command(httpcliPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			writeResponse(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &Error{Code: CodePodError, Message: err.Error()},
			})
			return
		}
	}

	result, _ := json.Marshal(map[string]any{
		"exit_code": exitCode,
		"response":  stdout.String(),
		"stderr":    stderr.String(),
	})
	writeResponse(Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	})
}

type BenchmarkParams struct {
	URL         string `json:"url"`
	Requests    int    `json:"requests,omitempty"`
	Concurrency int    `json:"concurrency,omitempty"`
}

func handleBenchmark(req Request) {
	var params BenchmarkParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeResponse(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &Error{Code: CodeInvalidParams, Message: "Invalid params: " + err.Error()},
			})
			return
		}
	}

	// Validate required params first
	if params.URL == "" {
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodeInvalidParams, Message: "Missing required parameter: url"},
		})
		return
	}

	// Check for httpcli binary
	if httpcliPath == "" {
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodePodError, Message: "httpcli not found in PATH"},
		})
		return
	}

	// Security: Limit requests to prevent abuse
	if params.Requests > 10000 {
		params.Requests = 10000
	}
	if params.Requests <= 0 {
		params.Requests = 100
	}
	if params.Concurrency > 100 {
		params.Concurrency = 100
	}
	if params.Concurrency <= 0 {
		params.Concurrency = 10
	}

	args := []string{"benchmark",
		"-n", fmt.Sprintf("%d", params.Requests),
		"-c", fmt.Sprintf("%d", params.Concurrency),
		params.URL,
	}

	cmd := exec.Command(httpcliPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			writeResponse(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &Error{Code: CodePodError, Message: err.Error()},
			})
			return
		}
	}

	result, _ := json.Marshal(map[string]any{
		"exit_code": exitCode,
		"output":    stdout.String(),
		"stderr":    stderr.String(),
	})
	writeResponse(Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	})
}

type TestMcpServerParams struct {
	TestFile  string `json:"test_file"`
	Transport string `json:"transport,omitempty"`
	ServerURL string `json:"server_url,omitempty"`
	Command   string `json:"server_command,omitempty"`
}

func handleTestMcpServer(req Request) {
	var params TestMcpServerParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeResponse(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &Error{Code: CodeInvalidParams, Message: "Invalid params: " + err.Error()},
			})
			return
		}
	}

	// Validate required params first
	if params.TestFile == "" {
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodeInvalidParams, Message: "Missing required parameter: test_file"},
		})
		return
	}

	// Security: Validate file path
	if strings.Contains(params.TestFile, "..") {
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodeInvalidParams, Message: "Path traversal not allowed"},
		})
		return
	}

	// Check for httpcli binary
	if httpcliPath == "" {
		writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: CodePodError, Message: "httpcli not found in PATH"},
		})
		return
	}

	args := []string{"mcp", params.TestFile}
	if params.Transport != "" {
		args = append(args, "--transport", params.Transport)
	}
	if params.ServerURL != "" {
		args = append(args, "--server", params.ServerURL)
	}
	if params.Command != "" {
		args = append(args, "--command", params.Command)
	}

	cmd := exec.Command(httpcliPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			writeResponse(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &Error{Code: CodePodError, Message: err.Error()},
			})
			return
		}
	}

	result, _ := json.Marshal(map[string]any{
		"exit_code": exitCode,
		"output":    stdout.String(),
		"stderr":    stderr.String(),
	})
	writeResponse(Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	})
}

func writeResponse(resp Response) {
	writeMu.Lock()
	defer writeMu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling response: %v\n", err)
		return
	}
	fmt.Println(string(data))
}
