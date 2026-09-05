package collector

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadResticStatusUsesNewestSnapshot(t *testing.T) {
	original := runRestic
	t.Cleanup(func() { runRestic = original })
	runRestic = func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`[{"time":"2026-09-01T08:00:00Z"},{"time":"2026-09-02T08:00:00Z"}]`), nil
	}

	status, err := readResticStatus(context.Background(), "restic")
	if err != nil {
		t.Fatalf("read restic status: %v", err)
	}
	if status.Status != "success" || status.LastRun != "2026-09-02T08:00:00Z" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestReadResticStatusHandlesEmptyAndMalformedOutput(t *testing.T) {
	original := runRestic
	t.Cleanup(func() { runRestic = original })

	runRestic = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("[]"), nil
	}
	status, err := readResticStatus(context.Background(), "restic")
	if err != nil || status.Status != "warning" {
		t.Fatalf("expected empty snapshot warning, got status=%+v err=%v", status, err)
	}

	runRestic = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("not-json"), nil
	}
	if _, err := readResticStatus(context.Background(), "restic"); err == nil {
		t.Fatal("expected malformed JSON to fail")
	}
}

func TestReadResticStatusHandlesMissingBinary(t *testing.T) {
	original := runRestic
	t.Cleanup(func() { runRestic = original })
	runRestic = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("executable not found")
	}
	if _, err := readResticStatus(context.Background(), "/missing/restic"); err == nil {
		t.Fatal("expected missing binary error")
	}
}

func TestCheckResticStatusRejectsNonGet(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/restic", nil)
	recorder := httptest.NewRecorder()
	CheckResticStatus(recorder, req)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected status code: %d", recorder.Code)
	}
}
