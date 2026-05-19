package resilience

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Rejecting calls
	CircuitHalfOpen                     // Probing with limited calls
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig holds circuit breaker configuration.
type CircuitBreakerConfig struct {
	FailureThreshold int
	SuccessThreshold int
	ResetTimeout     time.Duration
}

// DefaultCircuitBreakerConfig returns sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		ResetTimeout:     30 * time.Second,
	}
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	mu           sync.Mutex
	name         string
	state        CircuitState
	failureCount int
	successCount int
	lastFailure  time.Time
	config       CircuitBreakerConfig
	logger       *slog.Logger
}

// NewCircuitBreaker creates a new circuit breaker with the given config.
func NewCircuitBreaker(name string, cfg CircuitBreakerConfig, logger *slog.Logger) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 2
	}
	if cfg.ResetTimeout <= 0 {
		cfg.ResetTimeout = 30 * time.Second
	}
	return &CircuitBreaker{
		name:   name,
		state:  CircuitClosed,
		config: cfg,
		logger: logger,
	}
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	// Check if open breaker should transition to half-open
	if cb.state == CircuitOpen && time.Since(cb.lastFailure) >= cb.config.ResetTimeout {
		return CircuitHalfOpen
	}
	return cb.state
}

// Execute runs fn if the circuit is closed or half-open. Returns ErrCircuitOpen
// if the circuit is open. Context cancellations are not counted as failures.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func(ctx context.Context) error) error {
	cb.mu.Lock()

	// Check for transition from Open -> HalfOpen
	if cb.state == CircuitOpen {
		if time.Since(cb.lastFailure) >= cb.config.ResetTimeout {
			cb.state = CircuitHalfOpen
			cb.successCount = 0
			cb.logger.Info("circuit breaker half-open, probing", "pod", cb.name)
		} else {
			cb.mu.Unlock()
			return fmt.Errorf("%w: pod %s", ErrCircuitOpen, cb.name)
		}
	}

	currentState := cb.state
	cb.mu.Unlock()

	err := fn(ctx)

	// Don't count context cancellations (client disconnect) as failures
	if err != nil && errors.Is(ctx.Err(), context.Canceled) {
		return err
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failureCount++
		cb.lastFailure = time.Now()

		if currentState == CircuitHalfOpen {
			// Any failure in half-open trips back to open
			cb.state = CircuitOpen
			cb.logger.Warn("circuit breaker re-opened", "pod", cb.name, "error", err)
		} else if cb.failureCount >= cb.config.FailureThreshold {
			cb.state = CircuitOpen
			cb.logger.Warn("circuit breaker opened",
				"pod", cb.name,
				"failures", cb.failureCount,
				"threshold", cb.config.FailureThreshold,
			)
		}
		return err
	}

	// Success
	if currentState == CircuitHalfOpen {
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.state = CircuitClosed
			cb.failureCount = 0
			cb.successCount = 0
			cb.logger.Info("circuit breaker closed", "pod", cb.name)
		}
	} else {
		// Reset failure count on success in closed state
		cb.failureCount = 0
	}

	return nil
}
