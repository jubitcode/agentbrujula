package resilience

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"mcp-meta/internal/jsonrpc"
)

// RetryConfig holds retry configuration.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// DefaultRetryConfig returns sensible defaults (no retries).
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 0,
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   5 * time.Second,
	}
}

// Retry executes fn with exponential backoff and jitter.
// Returns the result of the last attempt on failure.
func Retry(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) error) error {
	if cfg.MaxRetries <= 0 {
		return fn(ctx)
	}

	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}

		// Don't retry if context is done or error is not retryable
		if ctx.Err() != nil {
			return lastErr
		}
		if !IsRetryable(lastErr) {
			return lastErr
		}

		// Don't sleep after the last attempt
		if attempt == cfg.MaxRetries {
			break
		}

		delay := backoffDelay(attempt, cfg.BaseDelay, cfg.MaxDelay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return lastErr
		}
	}

	return lastErr
}

// IsRetryable returns true for transient errors that may succeed on retry.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Circuit open — don't retry, it will just fail again
	if errors.Is(err, ErrCircuitOpen) {
		return false
	}

	// Check JSON-RPC error codes
	var jsonErr *jsonrpc.Error
	if errors.As(err, &jsonErr) {
		switch jsonErr.Code {
		case jsonrpc.CodePodCrashed:
			return true // Pod died, another replica may work
		case jsonrpc.CodePodTimeout:
			return true // Transient timeout
		case jsonrpc.CodePodError:
			return true // Generic pod error, may be transient
		case jsonrpc.CodePodSpawnFailed:
			return true // Spawn may succeed on retry
		default:
			// Method not found, invalid params, contract mismatch, etc. are permanent
			return false
		}
	}

	// Unknown errors default to retryable
	return true
}

// backoffDelay computes exponential backoff with full jitter.
func backoffDelay(attempt int, base, max time.Duration) time.Duration {
	delay := base
	for range attempt {
		delay *= 2
		if delay > max {
			delay = max
			break
		}
	}
	// Full jitter: random value in [0, delay)
	if delay > 0 {
		delay = time.Duration(rand.Int64N(int64(delay)))
	}
	return delay
}
