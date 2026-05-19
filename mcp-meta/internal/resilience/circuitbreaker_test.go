package resilience

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.Default()
}

func TestCircuitBreakerClosedOnSuccess(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig(), testLogger())

	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed, got %s", cb.State())
	}
}

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		ResetTimeout:     1 * time.Second,
	}
	cb := NewCircuitBreaker("test", cfg, testLogger())

	fail := errors.New("fail")
	for range 3 {
		_ = cb.Execute(context.Background(), func(ctx context.Context) error {
			return fail
		})
	}

	if cb.State() != CircuitOpen {
		t.Fatalf("expected open after %d failures, got %s", 3, cb.State())
	}

	// Calls should be rejected
	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		t.Fatal("should not be called when circuit is open")
		return nil
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreakerHalfOpenToClosedOnSuccess(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		ResetTimeout:     50 * time.Millisecond,
	}
	cb := NewCircuitBreaker("test", cfg, testLogger())

	// Trip the breaker
	fail := errors.New("fail")
	for range 2 {
		_ = cb.Execute(context.Background(), func(ctx context.Context) error {
			return fail
		})
	}
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open, got %s", cb.State())
	}

	// Wait for reset timeout
	time.Sleep(60 * time.Millisecond)

	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open after timeout, got %s", cb.State())
	}

	// Two successes should close it
	for range 2 {
		err := cb.Execute(context.Background(), func(ctx context.Context) error {
			return nil
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	}

	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed after successes, got %s", cb.State())
	}
}

func TestCircuitBreakerHalfOpenToOpenOnFailure(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		ResetTimeout:     50 * time.Millisecond,
	}
	cb := NewCircuitBreaker("test", cfg, testLogger())

	// Trip the breaker
	_ = cb.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("fail")
	})

	time.Sleep(60 * time.Millisecond)

	// Failure in half-open should re-open
	_ = cb.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("fail again")
	})

	if cb.State() != CircuitOpen {
		t.Fatalf("expected open after half-open failure, got %s", cb.State())
	}
}

func TestCircuitBreakerIgnoresContextCanceled(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		ResetTimeout:     1 * time.Second,
	}
	cb := NewCircuitBreaker("test", cfg, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_ = cb.Execute(ctx, func(ctx context.Context) error {
		return context.Canceled
	})

	// Circuit should remain closed — canceled requests aren't failures
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed (canceled should not count), got %s", cb.State())
	}
}

func TestCircuitBreakerResetsFailureCountOnSuccess(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 1,
		ResetTimeout:     1 * time.Second,
	}
	cb := NewCircuitBreaker("test", cfg, testLogger())

	fail := errors.New("fail")

	// 2 failures, then 1 success, then 2 failures — should NOT trip (threshold is 3)
	_ = cb.Execute(context.Background(), func(ctx context.Context) error { return fail })
	_ = cb.Execute(context.Background(), func(ctx context.Context) error { return fail })
	_ = cb.Execute(context.Background(), func(ctx context.Context) error { return nil })
	_ = cb.Execute(context.Background(), func(ctx context.Context) error { return fail })
	_ = cb.Execute(context.Background(), func(ctx context.Context) error { return fail })

	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed (success resets count), got %s", cb.State())
	}
}
