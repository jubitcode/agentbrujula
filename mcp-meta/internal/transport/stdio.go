package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"sync"

	"mcp-meta/internal/jsonrpc"
)

type StdioTransport struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	logger *slog.Logger

	mu       sync.Mutex
	encoder  *json.Encoder
	shutdown bool
}

func NewStdioTransport(stdin io.Reader, stdout, stderr io.Writer) *StdioTransport {
	return &StdioTransport{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}
}

func NewDefaultStdioTransport() *StdioTransport {
	return NewStdioTransport(os.Stdin, os.Stdout, os.Stderr)
}

func (t *StdioTransport) SetLogger(logger *slog.Logger) {
	t.logger = logger
}

// Start Returns when stdin is closed (EOF) or the context is cancelled.
func (t *StdioTransport) Start(ctx context.Context, handler Handler) error {
	t.mu.Lock()
	t.encoder = json.NewEncoder(t.stdout)
	t.mu.Unlock()

	scanner := bufio.NewScanner(t.stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	done := make(chan struct{})

	go func() {
		defer close(done)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()
			if line == "" {
				continue
			}

			t.processLine(ctx, handler, line)
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		if err := scanner.Err(); err != nil {
			return err
		}
		return nil
	}
}

func (t *StdioTransport) processLine(ctx context.Context, handler Handler, line string) {
	var req jsonrpc.Request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		t.sendResponse(&jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      jsonrpc.NullID(),
			Error: &jsonrpc.Error{
				Code:    jsonrpc.CodeParseError,
				Message: "parse error: " + err.Error(),
			},
		})
		return
	}

	resp, err := handler.Handle(ctx, &req)
	if err != nil {
		if t.logger != nil {
			t.logger.Error("handler error", "error", err)
		}
		return
	}

	if resp != nil {
		t.sendResponse(resp)
	}
}

func (t *StdioTransport) sendResponse(resp *jsonrpc.Response) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.encoder == nil {
		return
	}

	if err := t.encoder.Encode(resp); err != nil {
		if t.logger != nil {
			t.logger.Error("failed to write response", "error", err)
		}
	}
}

func (t *StdioTransport) Shutdown(ctx context.Context) error {
	t.mu.Lock()
	t.shutdown = true
	t.mu.Unlock()
	return nil
}
