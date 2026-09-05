// sentinel-agent: internal/collector/reporter.go (Neu)
package collector

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

func StartResilientReporter(ctx context.Context, _ *http.Client, reportFunc func() error) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			go func() {
				_ = executeWithRetry(ctx, reportFunc, 3, time.Second)
			}()
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
