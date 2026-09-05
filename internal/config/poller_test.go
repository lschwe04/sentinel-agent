package config

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestPollUpdatesRuntimeConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collector_interval_seconds":7,"log_level":"DEBUG"}`))
	}))
	defer server.Close()

	level := new(slog.LevelVar)
	level.Set(slog.LevelInfo)
	store := NewStore(RuntimeConfig{CollectorInterval: time.Minute, LogLevel: slog.LevelInfo})
	poller, err := NewPoller(server.URL, time.Minute, store, level, func() (*http.Client, error) {
		return server.Client(), nil
	})
	if err != nil {
		t.Fatalf("create poller: %v", err)
	}
	if err := poller.poll(context.Background()); err != nil {
		t.Fatalf("poll config: %v", err)
	}
	current := store.Load()
	if current.CollectorInterval != 7*time.Second || current.LogLevel != slog.LevelDebug || level.Level() != slog.LevelDebug {
		t.Fatalf("unexpected runtime config: %+v level=%s", current, level.Level())
	}
}

func TestStoreIsRaceFreeUnderConcurrentUpdates(t *testing.T) {
	store := NewStore(RuntimeConfig{CollectorInterval: time.Second, LogLevel: slog.LevelInfo})
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			for count := 0; count < 1000; count++ {
				_ = store.Update(RuntimeConfig{CollectorInterval: time.Duration(index+1) * time.Second, LogLevel: slog.LevelDebug})
				_ = store.Load()
			}
		}(i)
	}
	wait.Wait()
}
