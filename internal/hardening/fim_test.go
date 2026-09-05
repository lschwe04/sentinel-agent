package hardening

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFIMKeepsOriginalBaselineAfterModification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "critical.conf")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatalf("write initial file: %v", err)
	}

	scanner := NewFIMScanner([]string{path})
	scanner.BuildBaseline()
	if err := os.WriteFile(path, []byte("tampered"), 0600); err != nil {
		t.Fatalf("write modified file: %v", err)
	}

	if alerts := scanner.CheckIntegrity("test-node"); len(alerts) != 1 {
		t.Fatalf("expected first modification alert, got %d", len(alerts))
	}
	if alerts := scanner.CheckIntegrity("test-node"); len(alerts) != 1 {
		t.Fatalf("expected persistent modification alert, got %d", len(alerts))
	}
}
