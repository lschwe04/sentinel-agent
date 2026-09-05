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
