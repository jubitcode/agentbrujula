package transport

import (
	"context"

	"mcp-meta/internal/jsonrpc"
)

type Handler interface {
	Handle(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error)
}

type Transport interface {
	Start(ctx context.Context, handler Handler) error
	Shutdown(ctx context.Context) error
}
