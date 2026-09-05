// sentinel-agent: internal/collector/reporter.go (Neu)
package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("reporter circuit breaker is open")

type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half-open"
)

type CircuitBreaker struct {
	mu            sync.Mutex
	state         CircuitState
	failures      int
	failureLimit  int
	cooldown      time.Duration
	openedAt      time.Time
	halfOpenInUse bool
}

func NewCircuitBreaker(failureLimit int, cooldown time.Duration) (*CircuitBreaker, error) {
	if failureLimit <= 0 {
		return nil, fmt.Errorf("failure limit must be greater than zero")
	}
	if cooldown <= 0 {
		return nil, fmt.Errorf("cooldown must be greater than zero")
	}
	return &CircuitBreaker{state: CircuitClosed, failureLimit: failureLimit, cooldown: cooldown}, nil
}

func (b *CircuitBreaker) Allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case CircuitClosed:
		return nil
	case CircuitOpen:
		if time.Since(b.openedAt) < b.cooldown {
			return ErrCircuitOpen
		}
		b.state = CircuitHalfOpen
		b.halfOpenInUse = true
		return nil
	case CircuitHalfOpen:
		return ErrCircuitOpen
	default:
		return ErrCircuitOpen
	}
}

func (b *CircuitBreaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = CircuitClosed
	b.failures = 0
	b.halfOpenInUse = false
}

func (b *CircuitBreaker) RecordFailure(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == CircuitHalfOpen || b.failures+1 >= b.failureLimit {
		b.state = CircuitOpen
		b.openedAt = time.Now()
		b.halfOpenInUse = false
		slog.Warn("Reporter-Circuit-Breaker geöffnet", "component", "reporter", "failure_count", b.failures+1, "error", err)
		return
	}
	b.failures++
}

func (b *CircuitBreaker) State() CircuitState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func StartResilientReporter(ctx context.Context, _ *http.Client, reportFunc func() error) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = executeWithRetry(ctx, reportFunc, 3, time.Second)
		}
	}
}

func executeWithRetry(ctx context.Context, operation func() error, maxAttempts int, initialBackoff time.Duration) error {
	if operation == nil {
		return fmt.Errorf("retry operation is nil")
	}
	if maxAttempts <= 0 {
		return fmt.Errorf("maxAttempts must be greater than zero")
	}
	if initialBackoff <= 0 {
		initialBackoff = time.Millisecond
	}

	var lastErr error
	backoff := initialBackoff
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		started := time.Now()
		if err := operation(); err == nil {
			return nil
		} else {
			lastErr = err
			slog.Warn("Reporter-Versuch fehlgeschlagen", "component", "reporter", "retry_count", attempt-1, "duration", time.Since(started), "error", err)
		}
		if attempt == maxAttempts {
			break
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
	return lastErr
}
