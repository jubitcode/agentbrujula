package resilience

import (
	"context"
	"errors"
	"testing"
	"time"

	"mcp-meta/internal/jsonrpc"
)

func TestRetryNoRetriesOnSuccess(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond}, func(ctx context.Context) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestRetryRecoversAfterTransientFailure(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}, func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return &jsonrpc.Error{Code: jsonrpc.CodePodCrashed, Message: "crashed"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success on 3rd attempt, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRetryStopsOnNonRetryableError(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond}, func(ctx context.Context) error {
		calls++
		return &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "not found"}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (non-retryable), got %d", calls)
	}
}

func TestRetryStopsOnCircuitOpen(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond}, func(ctx context.Context) error {
		calls++
		return ErrCircuitOpen
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestRetryRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := Retry(ctx, RetryConfig{MaxRetries: 5, BaseDelay: time.Millisecond}, func(ctx context.Context) error {
		calls++
		if calls == 2 {
			cancel()
		}
		return errors.New("transient")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls > 3 {
		t.Fatalf("expected <= 3 calls after cancel, got %d", calls)
	}
}

func TestRetryZeroMaxRetries(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), RetryConfig{MaxRetries: 0}, func(ctx context.Context) error {
		calls++
		return errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 call with MaxRetries=0, got %d", calls)
	}
}

func TestBackoffDelay(t *testing.T) {
	base := 100 * time.Millisecond
	max := 5 * time.Second

	// Attempt 0: delay should be in [0, 100ms)
	for range 100 {
		d := backoffDelay(0, base, max)
		if d >= base {
			t.Fatalf("attempt 0 delay %v >= base %v", d, base)
		}
	}

	// Attempt 3: computed delay is 800ms, jitter in [0, 800ms)
	for range 100 {
		d := backoffDelay(3, base, max)
		if d >= 800*time.Millisecond {
			t.Fatalf("attempt 3 delay %v >= 800ms", d)
		}
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"circuit open", ErrCircuitOpen, false},
		{"pod crashed", &jsonrpc.Error{Code: jsonrpc.CodePodCrashed}, true},
		{"pod timeout", &jsonrpc.Error{Code: jsonrpc.CodePodTimeout}, true},
		{"method not found", &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound}, false},
		{"invalid params", &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams}, false},
		{"contract mismatch", &jsonrpc.Error{Code: jsonrpc.CodeContractMismatch}, false},
		{"unknown error", errors.New("something"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.expected {
				t.Fatalf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}
