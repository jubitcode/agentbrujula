package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"mcp-meta/internal/jsonrpc"
)

// getZombieCount returns the number of zombie processes (Linux/macOS)
func getZombieCount() int {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("ps", "aux")
	} else {
		cmd = exec.Command("ps", "-eo", "stat")
	}
	out, err := cmd.Output()
	if err != nil {
		return -1
	}
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "Z") && !strings.Contains(line, "ps") {
			count++
		}
	}
	return count
}

// getMemUsageMB returns memory usage in MB
func getMemUsageMB() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc / 1024 / 1024
}

// echoHandler returns the request as a response (for testing)
type echoHandler struct {
	callCount int
	mu        sync.Mutex
}

func (h *echoHandler) Handle(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	h.mu.Lock()
	h.callCount++
	h.mu.Unlock()

	result, _ := json.Marshal(map[string]interface{}{
		"method":   req.Method,
		"received": true,
	})
	return &jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}, nil
}

func (h *echoHandler) getCallCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.callCount
}

// SSE client helper
type sseTestClient struct {
	tb         testing.TB
	addr       string
	endpoint   string
	resp       *http.Response
	reader     *bufio.Reader
	messages   chan []byte
	done       chan struct{}
	httpClient *http.Client
}

func newSSETestClient(tb testing.TB, addr string) (*sseTestClient, error) {
	client := &sseTestClient{
		tb:       tb,
		addr:     addr,
		messages: make(chan []byte, 100),
		done:     make(chan struct{}),
		httpClient: &http.Client{
			Timeout: 0, // No timeout for SSE
		},
	}

	resp, err := client.httpClient.Get("http://" + addr + "/sse")
	if err != nil {
		return nil, fmt.Errorf("connect to SSE: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	client.resp = resp
	client.reader = bufio.NewReader(resp.Body)

	// Read endpoint from first message
	line, err := client.reader.ReadString('\n')
	if err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("read endpoint: %w", err)
	}

	if strings.HasPrefix(line, "data: ") {
		data := strings.TrimPrefix(line, "data: ")
		data = strings.TrimSpace(data)
		var endpointMsg struct {
			Endpoint string `json:"endpoint"`
		}
		if err := json.Unmarshal([]byte(data), &endpointMsg); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("parse endpoint: %w", err)
		}
		client.endpoint = endpointMsg.Endpoint
	}

	// Skip empty line after data
	client.reader.ReadString('\n')

	// Start reading messages in background
	go client.readMessages()

	return client, nil
}

func (c *sseTestClient) readMessages() {
	defer close(c.messages)

	for {
		select {
		case <-c.done:
			return
		default:
		}

		line, err := c.reader.ReadString('\n')
		if err != nil {
			return
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			data = strings.TrimSpace(data)
			c.messages <- []byte(data)
		}
	}
}

func (c *sseTestClient) sendRequest(req *jsonrpc.Request) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Post(c.endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

func (c *sseTestClient) waitForResponse(timeout time.Duration) ([]byte, error) {
	select {
	case msg := <-c.messages:
		return msg, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for response")
	}
}

func (c *sseTestClient) close() {
	close(c.done)
	if c.resp != nil {
		c.resp.Body.Close()
	}
}

func TestSSETransport_FullRequestFlow(t *testing.T) {
	zombiesBefore := getZombieCount()

	transport := NewSSETransport("127.0.0.1", 0)
	handler := &echoHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server
	done := make(chan error, 1)
	go func() {
		done <- transport.Start(ctx, handler)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	addr := transport.Addr()
	if addr == "" {
		t.Fatal("Server address should not be empty")
	}

	// Connect SSE client
	client, err := newSSETestClient(t, addr)
	if err != nil {
		t.Fatalf("Failed to create SSE client: %v", err)
	}
	defer client.close()

	if client.endpoint == "" {
		t.Fatal("Should receive endpoint from SSE")
	}

	// Send a request
	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "test/echo",
		Params:  json.RawMessage(`{"hello":"world"}`),
	}

	if err := client.sendRequest(req); err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}

	// Wait for response
	respData, err := client.waitForResponse(5 * time.Second)
	if err != nil {
		t.Fatalf("Failed to get response: %v", err)
	}

	var resp jsonrpc.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.JSONRPC != "2.0" {
		t.Errorf("Response JSONRPC = %q, want '2.0'", resp.JSONRPC)
	}

	// Verify handler was called
	if handler.getCallCount() != 1 {
		t.Errorf("Handler call count = %d, want 1", handler.getCallCount())
	}

	// Shutdown
	cancel()
	<-done

	// Verify no zombie processes
	zombiesAfter := getZombieCount()
	if zombiesAfter > zombiesBefore {
		t.Errorf("Zombie processes increased: before=%d, after=%d", zombiesBefore, zombiesAfter)
	}
}

func TestSSETransport_MultipleClients(t *testing.T) {
	zombiesBefore := getZombieCount()
	memBefore := getMemUsageMB()

	transport := NewSSETransport("127.0.0.1", 0)
	handler := &echoHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- transport.Start(ctx, handler)
	}()

	time.Sleep(100 * time.Millisecond)
	addr := transport.Addr()

	// Connect multiple clients
	const numClients = 5
	clients := make([]*sseTestClient, numClients)

	for i := 0; i < numClients; i++ {
		client, err := newSSETestClient(t, addr)
		if err != nil {
			t.Fatalf("Failed to create client %d: %v", i, err)
		}
		clients[i] = client
	}

	// Each client sends a request
	for i, client := range clients {
		req := &jsonrpc.Request{
			JSONRPC: "2.0",
			ID:      jsonrpc.NewIntID(int64(i + 1)),
			Method:  fmt.Sprintf("test/client%d", i),
		}
		if err := client.sendRequest(req); err != nil {
			t.Errorf("Client %d failed to send: %v", i, err)
		}
	}

	// Wait for all responses
	for i, client := range clients {
		_, err := client.waitForResponse(5 * time.Second)
		if err != nil {
			t.Errorf("Client %d failed to receive: %v", i, err)
		}
	}

	// Close all clients
	for _, client := range clients {
		client.close()
	}

	// Verify handler was called for each client
	if handler.getCallCount() != numClients {
		t.Errorf("Handler call count = %d, want %d", handler.getCallCount(), numClients)
	}

	cancel()
	<-done

	// Verify no zombies
	zombiesAfter := getZombieCount()
	if zombiesAfter > zombiesBefore {
		t.Errorf("Zombie processes: before=%d, after=%d", zombiesBefore, zombiesAfter)
	}

	// Verify memory didn't grow excessively
	memAfter := getMemUsageMB()
	if memAfter > memBefore+50 {
		t.Errorf("Memory grew significantly: before=%dMB, after=%dMB", memBefore, memAfter)
	}
}

func TestSSETransport_ConcurrentRequests(t *testing.T) {
	transport := NewSSETransport("127.0.0.1", 0)
	handler := &echoHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- transport.Start(ctx, handler)
	}()

	time.Sleep(100 * time.Millisecond)
	addr := transport.Addr()

	client, err := newSSETestClient(t, addr)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.close()

	// Send many concurrent requests
	const numRequests = 20
	var wg sync.WaitGroup

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := &jsonrpc.Request{
				JSONRPC: "2.0",
				ID:      jsonrpc.NewIntID(int64(id)),
				Method:  "test/concurrent",
			}
			client.sendRequest(req)
		}(i)
	}

	wg.Wait()

	// Wait for all responses
	received := 0
	timeout := time.After(10 * time.Second)

responseLoop:
	for received < numRequests {
		select {
		case <-client.messages:
			received++
		case <-timeout:
			break responseLoop
		}
	}

	if received < numRequests {
		t.Errorf("Received %d responses, want %d", received, numRequests)
	}

	cancel()
	<-done
}

func TestSSETransport_InvalidJSON(t *testing.T) {
	transport := NewSSETransport("127.0.0.1", 0)
	handler := &echoHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- transport.Start(ctx, handler)
	}()

	time.Sleep(100 * time.Millisecond)
	addr := transport.Addr()

	client, err := newSSETestClient(t, addr)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.close()

	// Send invalid JSON directly
	httpClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpClient.Post(client.endpoint, "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("Failed to send invalid JSON: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("Status = %d, want 202", resp.StatusCode)
	}

	// Should receive error response via SSE
	respData, err := client.waitForResponse(5 * time.Second)
	if err != nil {
		t.Fatalf("Failed to get error response: %v", err)
	}

	var errResp jsonrpc.Response
	if err := json.Unmarshal(respData, &errResp); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	if errResp.Error == nil {
		t.Error("Should receive error response for invalid JSON")
	} else if !strings.Contains(errResp.Error.Message, "parse error") {
		t.Errorf("Error message = %q, want to contain 'parse error'", errResp.Error.Message)
	}

	cancel()
	<-done
}

func TestSSETransport_ClientDisconnect(t *testing.T) {
	zombiesBefore := getZombieCount()

	transport := NewSSETransport("127.0.0.1", 0)
	handler := &echoHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- transport.Start(ctx, handler)
	}()

	time.Sleep(100 * time.Millisecond)
	addr := transport.Addr()

	// Connect and immediately disconnect multiple times
	for i := 0; i < 5; i++ {
		client, err := newSSETestClient(t, addr)
		if err != nil {
			t.Fatalf("Failed to create client %d: %v", i, err)
		}
		client.close()
		time.Sleep(20 * time.Millisecond) // Give server time to cleanup
	}

	// Give server time to cleanup
	time.Sleep(100 * time.Millisecond)

	cancel()
	<-done

	// Verify no zombies
	zombiesAfter := getZombieCount()
	if zombiesAfter > zombiesBefore {
		t.Errorf("Zombie processes: before=%d, after=%d", zombiesBefore, zombiesAfter)
	}
}

func TestSSETransport_GracefulShutdown(t *testing.T) {
	transport := NewSSETransport("127.0.0.1", 0)
	handler := &echoHandler{}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- transport.Start(ctx, handler)
	}()

	time.Sleep(100 * time.Millisecond)
	addr := transport.Addr()

	// Connect client
	client, err := newSSETestClient(t, addr)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Send a request before shutdown
	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      jsonrpc.NewIntID(1),
		Method:  "test/before_shutdown",
	}
	client.sendRequest(req)

	// Wait for response
	_, err = client.waitForResponse(5 * time.Second)
	if err != nil {
		t.Errorf("Request before shutdown failed: %v", err)
	}

	// Close client before shutdown (otherwise server waits for SSE connection)
	client.close()

	// Trigger shutdown
	cancel()

	// Wait for shutdown to complete
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Start() returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not complete in time")
	}

	// Verify server is no longer accepting connections
	httpClient := &http.Client{Timeout: 1 * time.Second}
	_, err = httpClient.Get("http://" + addr + "/health")
	if err == nil {
		t.Error("Server should not be accepting connections after shutdown")
	}
}

func TestSSETransport_MemoryUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test in short mode")
	}

	runtime.GC()
	memBefore := getMemUsageMB()

	transport := NewSSETransport("127.0.0.1", 0)
	handler := &echoHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- transport.Start(ctx, handler)
	}()

	time.Sleep(100 * time.Millisecond)
	addr := transport.Addr()

	// Create many clients and send many requests
	const numClients = 10
	const numRequestsPerClient = 50

	var wg sync.WaitGroup
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			client, err := newSSETestClient(t, addr)
			if err != nil {
				t.Errorf("Client %d: failed to connect: %v", clientID, err)
				return
			}
			defer client.close()

			for j := 0; j < numRequestsPerClient; j++ {
				req := &jsonrpc.Request{
					JSONRPC: "2.0",
					ID:      jsonrpc.NewIntID(int64(clientID*1000 + j)),
					Method:  "test/load",
					Params:  json.RawMessage(`{"data":"` + strings.Repeat("x", 100) + `"}`),
				}
				client.sendRequest(req)
			}

			// Drain responses
			for j := 0; j < numRequestsPerClient; j++ {
				select {
				case <-client.messages:
				case <-time.After(10 * time.Second):
					return
				}
			}
		}(i)
	}

	wg.Wait()

	// Force GC and check memory
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	memAfter := getMemUsageMB()

	cancel()
	<-done

	// Memory should not grow more than 100MB under load
	if memAfter > memBefore+100 {
		t.Errorf("Memory grew too much: before=%dMB, after=%dMB", memBefore, memAfter)
	}
}

func BenchmarkSSETransport_RequestResponse(b *testing.B) {
	transport := NewSSETransport("127.0.0.1", 0)
	handler := &echoHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go transport.Start(ctx, handler)
	time.Sleep(100 * time.Millisecond)

	addr := transport.Addr()
	client, err := newSSETestClient(b, addr)
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer client.close()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := &jsonrpc.Request{
			JSONRPC: "2.0",
			ID:      jsonrpc.NewIntID(int64(i)),
			Method:  "test/bench",
		}
		client.sendRequest(req)
		client.waitForResponse(5 * time.Second)
	}
}

func BenchmarkSSETransport_ConcurrentClients(b *testing.B) {
	transport := NewSSETransport("127.0.0.1", 0)
	handler := &echoHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go transport.Start(ctx, handler)
	time.Sleep(100 * time.Millisecond)

	addr := transport.Addr()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		client, err := newSSETestClient(b, addr)
		if err != nil {
			b.Fatalf("Failed to create client: %v", err)
		}
		defer client.close()

		i := 0
		for pb.Next() {
			req := &jsonrpc.Request{
				JSONRPC: "2.0",
				ID:      jsonrpc.NewIntID(int64(i)),
				Method:  "test/bench",
			}
			client.sendRequest(req)
			client.waitForResponse(5 * time.Second)
			i++
		}
	})
}
