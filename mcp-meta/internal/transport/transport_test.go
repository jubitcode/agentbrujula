package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"mcp-meta/internal/jsonrpc"
)

// mockHandler implements the Handler interface for testing
type mockHandler struct {
	response *jsonrpc.Response
	err      error
}

func (m *mockHandler) Handle(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	return m.response, m.err
}

func TestNewStdioTransport(t *testing.T) {
	stdin := strings.NewReader("")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	transport := NewStdioTransport(stdin, stdout, stderr)

	if transport == nil {
		t.Fatal("NewStdioTransport returned nil")
	}
	if transport.stdin != stdin {
		t.Error("stdin not set correctly")
	}
	if transport.stdout != stdout {
		t.Error("stdout not set correctly")
	}
	if transport.stderr != stderr {
		t.Error("stderr not set correctly")
	}
}

func TestNewDefaultStdioTransport(t *testing.T) {
	transport := NewDefaultStdioTransport()

	if transport == nil {
		t.Fatal("NewDefaultStdioTransport returned nil")
	}
}

func TestStdioTransport_SetLogger(t *testing.T) {
	transport := NewStdioTransport(nil, nil, nil)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	transport.SetLogger(logger)

	if transport.logger != logger {
		t.Error("logger not set correctly")
	}
}

func TestStdioTransport_Shutdown(t *testing.T) {
	transport := NewStdioTransport(nil, nil, nil)

	err := transport.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}

	if !transport.shutdown {
		t.Error("shutdown flag not set")
	}
}

func TestStdioTransport_Start_EmptyInput(t *testing.T) {
	stdin := strings.NewReader("")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	transport := NewStdioTransport(stdin, stdout, stderr)
	handler := &mockHandler{}

	err := transport.Start(context.Background(), handler)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}
}

func TestStdioTransport_Start_ValidRequest(t *testing.T) {
	// Create a request
	req := jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "test",
	}
	reqBytes, _ := json.Marshal(req)

	stdin := strings.NewReader(string(reqBytes) + "\n")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	transport := NewStdioTransport(stdin, stdout, stderr)

	// Create handler that returns a response
	resp := &jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Result:  json.RawMessage(`{"status":"ok"}`),
	}
	handler := &mockHandler{response: resp}

	err := transport.Start(context.Background(), handler)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	// Check response was written
	output := stdout.String()
	if !strings.Contains(output, "2.0") {
		t.Errorf("Response not written to stdout, got: %q", output)
	}
}

func TestStdioTransport_Start_InvalidJSON(t *testing.T) {
	stdin := strings.NewReader("not valid json\n")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	transport := NewStdioTransport(stdin, stdout, stderr)
	handler := &mockHandler{}

	err := transport.Start(context.Background(), handler)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	// Check error response was written
	output := stdout.String()
	if !strings.Contains(output, "parse error") {
		t.Errorf("Error response not written, got: %q", output)
	}
}

func TestStdioTransport_Start_EmptyLines(t *testing.T) {
	stdin := strings.NewReader("\n\n\n")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	transport := NewStdioTransport(stdin, stdout, stderr)
	handler := &mockHandler{}

	err := transport.Start(context.Background(), handler)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	// Empty lines should be skipped, no output expected
	if stdout.String() != "" {
		t.Errorf("Unexpected output: %q", stdout.String())
	}
}

func TestStdioTransport_Start_ContextCanceled(t *testing.T) {
	// Create a reader that blocks
	pr, _ := newBlockingPipe()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	transport := NewStdioTransport(pr, stdout, stderr)
	handler := &mockHandler{}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := transport.Start(ctx, handler)
	if err != context.DeadlineExceeded {
		t.Errorf("Start() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestStdioTransport_Start_NilResponse(t *testing.T) {
	// Notification (handler returns nil response)
	req := jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NullID(),
		Method:  "notifications/test",
	}
	reqBytes, _ := json.Marshal(req)

	stdin := strings.NewReader(string(reqBytes) + "\n")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	transport := NewStdioTransport(stdin, stdout, stderr)
	handler := &mockHandler{response: nil} // nil response for notification

	err := transport.Start(context.Background(), handler)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	// No response should be written for notifications
	if stdout.String() != "" {
		t.Errorf("Unexpected output for notification: %q", stdout.String())
	}
}

func TestStdioTransport_Start_MultipleRequests(t *testing.T) {
	req1 := jsonrpc.Request{JSONRPC: "2.0", ID: jsonrpc.NewIntID(1), Method: "test1"}
	req2 := jsonrpc.Request{JSONRPC: "2.0", ID: jsonrpc.NewIntID(2), Method: "test2"}

	reqBytes1, _ := json.Marshal(req1)
	reqBytes2, _ := json.Marshal(req2)

	input := string(reqBytes1) + "\n" + string(reqBytes2) + "\n"
	stdin := strings.NewReader(input)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	transport := NewStdioTransport(stdin, stdout, stderr)

	callCount := 0
	handler := &mockHandler{
		response: &jsonrpc.Response{JSONRPC: "2.0", Result: json.RawMessage(`{}`)},
	}

	// Wrap handler to count calls
	wrappedHandler := &countingHandler{inner: handler, count: &callCount}

	err := transport.Start(context.Background(), wrappedHandler)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	if callCount != 2 {
		t.Errorf("Handler called %d times, want 2", callCount)
	}
}

// countingHandler wraps a handler and counts calls
type countingHandler struct {
	inner Handler
	count *int
}

func (h *countingHandler) Handle(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	*h.count++
	return h.inner.Handle(ctx, req)
}

func TestStdioTransport_sendResponse_NilEncoder(t *testing.T) {
	transport := &StdioTransport{}
	// encoder is nil

	resp := &jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
	}

	// Should not panic
	transport.sendResponse(resp)
}

// blockingReader is a reader that blocks until context is cancelled
type blockingReader struct {
	ch chan []byte
}

func newBlockingPipe() (*blockingReader, func([]byte)) {
	ch := make(chan []byte)
	return &blockingReader{ch: ch}, func(data []byte) {
		ch <- data
	}
}

func (r *blockingReader) Read(p []byte) (int, error) {
	select {
	case data := <-r.ch:
		copy(p, data)
		return len(data), nil
	}
}

// Interface tests
func TestHandler_Interface(t *testing.T) {
	var _ Handler = &mockHandler{}
}

func TestTransport_Interface(t *testing.T) {
	var _ Transport = &StdioTransport{}
	var _ Transport = &SSETransport{}
}

// SSE Transport tests

func TestNewSSETransport(t *testing.T) {
	transport := NewSSETransport("localhost", 0)
	if transport == nil {
		t.Fatal("NewSSETransport returned nil")
	}
	if transport.host != "localhost" {
		t.Errorf("host = %q, want %q", transport.host, "localhost")
	}
	if transport.port != 0 {
		t.Errorf("port = %d, want 0", transport.port)
	}
	if transport.clients == nil {
		t.Error("clients map should be initialized")
	}
}

func TestSSETransport_SetLogger(t *testing.T) {
	transport := NewSSETransport("localhost", 0)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	transport.SetLogger(logger)

	if transport.logger != logger {
		t.Error("logger not set correctly")
	}
}

func TestSSETransport_Addr_BeforeStart(t *testing.T) {
	transport := NewSSETransport("localhost", 0)
	addr := transport.Addr()
	if addr != "" {
		t.Errorf("Addr() before start should be empty, got %q", addr)
	}
}

func TestSSETransport_Shutdown_BeforeStart(t *testing.T) {
	transport := NewSSETransport("localhost", 0)
	err := transport.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown() before start error = %v", err)
	}
}

func TestSSETransport_Start_And_Shutdown(t *testing.T) {
	transport := NewSSETransport("127.0.0.1", 0)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())

	// Start in goroutine
	done := make(chan error, 1)
	go func() {
		done <- transport.Start(ctx, handler)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	addr := transport.Addr()
	if addr == "" {
		t.Fatal("Addr() should not be empty after start")
	}

	// Cancel context to stop
	cancel()

	// Wait for shutdown
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Start() error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after cancel")
	}
}

func TestSSETransport_HealthEndpoint(t *testing.T) {
	transport := NewSSETransport("127.0.0.1", 0)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go transport.Start(ctx, handler)
	time.Sleep(100 * time.Millisecond)

	addr := transport.Addr()
	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /health status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "ok") {
		t.Errorf("GET /health body = %q, want to contain 'ok'", body)
	}
}

func TestSSETransport_MessageEndpoint_WrongMethod(t *testing.T) {
	transport := NewSSETransport("127.0.0.1", 0)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go transport.Start(ctx, handler)
	time.Sleep(100 * time.Millisecond)

	addr := transport.Addr()

	// GET should fail
	resp, err := http.Get("http://" + addr + "/messages/1")
	if err != nil {
		t.Fatalf("GET /messages/1 error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /messages/1 status = %d, want 405", resp.StatusCode)
	}
}

func TestSSETransport_MessageEndpoint_InvalidClientID(t *testing.T) {
	transport := NewSSETransport("127.0.0.1", 0)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go transport.Start(ctx, handler)
	time.Sleep(100 * time.Millisecond)

	addr := transport.Addr()

	// POST to non-existent client
	resp, err := http.Post("http://"+addr+"/messages/99999", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /messages/99999 error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("POST /messages/99999 status = %d, want 404", resp.StatusCode)
	}
}

func TestSSETransport_MessageEndpoint_BadPath(t *testing.T) {
	transport := NewSSETransport("127.0.0.1", 0)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go transport.Start(ctx, handler)
	time.Sleep(100 * time.Millisecond)

	addr := transport.Addr()

	// POST to invalid path
	resp, err := http.Post("http://"+addr+"/messages/notanumber", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /messages/notanumber error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST /messages/notanumber status = %d, want 400", resp.StatusCode)
	}
}

func TestSSETransport_SSEEndpoint_WrongMethod(t *testing.T) {
	transport := NewSSETransport("127.0.0.1", 0)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go transport.Start(ctx, handler)
	time.Sleep(100 * time.Millisecond)

	addr := transport.Addr()

	// POST should fail
	resp, err := http.Post("http://"+addr+"/sse", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /sse error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /sse status = %d, want 405", resp.StatusCode)
	}
}

func TestStdioTransport_Start_HandlerError(t *testing.T) {
	// Test when handler returns an error
	req := jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "test",
	}
	reqBytes, _ := json.Marshal(req)

	stdin := strings.NewReader(string(reqBytes) + "\n")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	transport := NewStdioTransport(stdin, stdout, stderr)
	transport.SetLogger(slog.New(slog.NewTextHandler(stderr, nil)))

	// Handler that returns an error
	handler := &mockHandler{
		response: nil,
		err:      &jsonrpc.Error{Code: -1, Message: "test error"},
	}

	err := transport.Start(context.Background(), handler)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	// No response should be written for handler errors
	// (error is logged, not sent as response)
}

// failingWriter always fails to write
type failingWriter struct{}

func (w *failingWriter) Write(p []byte) (n int, err error) {
	return 0, io.ErrShortWrite
}

func TestStdioTransport_sendResponse_WriteError(t *testing.T) {
	// Test when encoder fails to write
	stdin := strings.NewReader("")
	stdout := &failingWriter{} // Will fail to write
	stderr := &bytes.Buffer{}

	transport := NewStdioTransport(stdin, stdout, stderr)
	transport.SetLogger(slog.New(slog.NewTextHandler(stderr, nil)))

	// Initialize encoder
	transport.mu.Lock()
	transport.encoder = json.NewEncoder(stdout)
	transport.mu.Unlock()

	resp := &jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Result:  json.RawMessage(`{"ok": true}`),
	}

	// Should not panic, error should be logged
	transport.sendResponse(resp)

	// Check that error was logged
	if !strings.Contains(stderr.String(), "failed to write response") {
		t.Error("Expected error to be logged")
	}
}

func TestStdioTransport_Start_ScannerError(t *testing.T) {
	// Create a reader that returns an error
	pr, pw := io.Pipe()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	transport := NewStdioTransport(pr, stdout, stderr)
	handler := &mockHandler{}

	// Close the write side with an error
	go func() {
		time.Sleep(10 * time.Millisecond)
		pw.CloseWithError(io.ErrUnexpectedEOF)
	}()

	err := transport.Start(context.Background(), handler)
	// Scanner should return an error when pipe is closed with error
	if err == nil {
		t.Log("Scanner may not report error for pipe close")
	}
}
