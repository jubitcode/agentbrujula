package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"mcp-meta/internal/jsonrpc"
)

type SSETransport struct {
	host   string
	port   int
	server *http.Server
	logger *slog.Logger

	mu       sync.Mutex
	clients  map[int64]*sseClient
	clientID atomic.Int64
	addr     string
}

type sseClient struct {
	id       int64
	handler  Handler
	messages chan []byte
	done     chan struct{}
	endpoint string
}

func NewSSETransport(host string, port int) *SSETransport {
	return &SSETransport{
		host:    host,
		port:    port,
		clients: make(map[int64]*sseClient),
	}
}

func (t *SSETransport) SetLogger(logger *slog.Logger) {
	t.logger = logger
}

func (t *SSETransport) Addr() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.addr
}

func (t *SSETransport) Start(ctx context.Context, handler Handler) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		t.handleSSE(w, r, handler)
	})

	mux.HandleFunc("/messages/", func(w http.ResponseWriter, r *http.Request) {
		t.handleMessage(w, r)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", t.host, t.port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	t.mu.Lock()
	t.addr = listener.Addr().String()
	t.server = &http.Server{
		Handler: mux,
	}
	t.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		if err := t.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		t.server.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (t *SSETransport) handleSSE(w http.ResponseWriter, r *http.Request, handler Handler) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	id := t.clientID.Add(1)
	client := &sseClient{
		id:       id,
		handler:  handler,
		messages: make(chan []byte, 100),
		done:     make(chan struct{}),
		endpoint: fmt.Sprintf("http://%s/messages/%d", t.Addr(), id),
	}

	t.mu.Lock()
	t.clients[id] = client
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		delete(t.clients, id)
		t.mu.Unlock()
		close(client.done)
	}()

	endpointMsg := map[string]string{"endpoint": client.endpoint}
	endpointData, _ := json.Marshal(endpointMsg)
	fmt.Fprintf(w, "data: %s\n\n", endpointData)

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-client.messages:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

func (t *SSETransport) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var clientID int64
	_, err := fmt.Sscanf(r.URL.Path, "/messages/%d", &clientID)
	if err != nil {
		http.Error(w, "Invalid client ID", http.StatusBadRequest)
		return
	}

	t.mu.Lock()
	client, ok := t.clients[clientID]
	t.mu.Unlock()

	if !ok {
		http.Error(w, "Client not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	var req jsonrpc.Request
	if err := json.Unmarshal(body, &req); err != nil {
		errResp := &jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      jsonrpc.NullID(),
			Error: &jsonrpc.Error{
				Code:    jsonrpc.CodeParseError,
				Message: "parse error: " + err.Error(),
			},
		}
		respData, _ := json.Marshal(errResp)
		select {
		case client.messages <- respData:
		default:
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := client.handler.Handle(ctx, &req)
		if err != nil {
			if t.logger != nil {
				t.logger.Error("handler error", "error", err)
			}
			return
		}

		if resp == nil {
			return
		}

		respData, err := json.Marshal(resp)
		if err != nil {
			if t.logger != nil {
				t.logger.Error("failed to marshal response", "error", err)
			}
			return
		}

		select {
		case client.messages <- respData:
		case <-client.done:
		}
	}()

	w.WriteHeader(http.StatusAccepted)
}

func (t *SSETransport) Shutdown(ctx context.Context) error {
	t.mu.Lock()
	server := t.server
	for _, client := range t.clients {
		close(client.messages)
	}
	t.clients = make(map[int64]*sseClient)
	t.mu.Unlock()

	if server == nil {
		return nil
	}

	return server.Shutdown(ctx)
}
