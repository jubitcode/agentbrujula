package correlation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
)

type correlationKey struct{}

// NewID generates a random 16-byte hex request ID.
func NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// FromContext extracts the correlation ID from the context.
func FromContext(ctx context.Context) string {
	if id, ok := ctx.Value(correlationKey{}).(string); ok {
		return id
	}
	return ""
}

// WithID returns a new context with the correlation ID set.
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

// ContextLogger returns a logger enriched with the correlation ID from context.
// If logger is nil, returns slog.Default().
func ContextLogger(ctx context.Context, logger *slog.Logger) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	if id := FromContext(ctx); id != "" {
		return logger.With("request_id", id)
	}
	return logger
}
