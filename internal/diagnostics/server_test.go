package diagnostics

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestDiagnosticSocketIsRootOnlyAndShutsDown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix domain socket permissions are Linux-specific")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(t.TempDir(), "debug.sock")
	if _, err := Start(ctx, path); err != nil {
		t.Fatalf("start diagnostics: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat diagnostics socket: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("unexpected socket permissions: %o", info.Mode().Perm())
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("diagnostic socket was not removed on shutdown")
}
