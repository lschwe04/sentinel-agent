package collector

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExecuteWithRetryRetriesUntilSuccess(t *testing.T) {
	attempts := 0
	err := executeWithRetry(context.Background(), func() error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary failure")
		}
		return nil
	}, 3, time.Millisecond)
	if err != nil {
		t.Fatalf("retry returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestExecuteWithRetryStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0
	err := executeWithRetry(ctx, func() error {
		attempts++
		return errors.New("failure")
	}, 3, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if attempts != 0 {
		t.Fatalf("expected no attempts after cancellation, got %d", attempts)
	}
}

func TestExecuteWithRetryValidatesArguments(t *testing.T) {
	if err := executeWithRetry(context.Background(), nil, 1, time.Millisecond); err == nil {
		t.Fatal("expected nil operation error")
	}
	if err := executeWithRetry(context.Background(), func() error { return nil }, 0, time.Millisecond); err == nil {
		t.Fatal("expected invalid attempt count error")
	}
}

func TestCircuitBreakerTransitionsThroughHalfOpen(t *testing.T) {
	breaker, err := NewCircuitBreaker(5, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("create breaker: %v", err)
	}
	for i := 0; i < 4; i++ {
		breaker.RecordFailure(errors.New("backend unavailable"))
	}
	if breaker.State() != CircuitClosed {
		t.Fatalf("breaker opened too early: %s", breaker.State())
	}
	breaker.RecordFailure(errors.New("backend unavailable"))
	if breaker.State() != CircuitOpen {
		t.Fatalf("expected open state, got %s", breaker.State())
	}
	if err := breaker.Allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected open fast-fail, got %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	if err := breaker.Allow(); err != nil {
		t.Fatalf("expected half-open probe to be allowed: %v", err)
	}
	if breaker.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open state, got %s", breaker.State())
	}
	if err := breaker.Allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected only one half-open probe, got %v", err)
	}
	breaker.RecordSuccess()
	if breaker.State() != CircuitClosed {
		t.Fatalf("expected closed state after successful probe, got %s", breaker.State())
	}
}

func TestCircuitBreakerReopensAfterFailedProbe(t *testing.T) {
	breaker, _ := NewCircuitBreaker(1, time.Millisecond)
	breaker.RecordFailure(errors.New("failure"))
	time.Sleep(3 * time.Millisecond)
	if err := breaker.Allow(); err != nil {
		t.Fatalf("expected half-open probe: %v", err)
	}
	breaker.RecordFailure(errors.New("failure"))
	if breaker.State() != CircuitOpen {
		t.Fatalf("expected failed probe to reopen breaker, got %s", breaker.State())
	}
}
