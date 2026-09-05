package buffer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewDiskBufferRequiresAES256Key(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buffer.dat")
	if _, err := NewDiskBuffer(path, 10, []byte("too-short")); err == nil {
		t.Fatal("expected invalid key length to fail")
	}
}

func TestDiskBufferPersistsAndFlushesWithAES256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buffer.dat")
	key := []byte("01234567890123456789012345678901")

	buf, err := NewDiskBuffer(path, 10, key)
	if err != nil {
		t.Fatalf("create buffer: %v", err)
	}
	if err := buf.Enqueue("demo", map[string]string{"status": "healthy"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted buffer: %v", err)
	}
	if len(persisted) == 0 {
		t.Fatal("expected encrypted buffer data on disk")
	}

	restored, err := NewDiskBuffer(path, 10, key)
	if err != nil {
		t.Fatalf("restore buffer: %v", err)
	}
	called := false
	if err := restored.FlushCompress(context.Background(), func(_ context.Context, payload []byte) error {
		called = len(payload) > 0
		return nil
	}); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if !called {
		t.Fatal("expected compressed payload to be sent")
	}
}

func TestDiskBufferRejectsWrongKeyOnRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buffer.dat")
	key := []byte("01234567890123456789012345678901")
	buf, err := NewDiskBuffer(path, 10, key)
	if err != nil {
		t.Fatalf("create buffer: %v", err)
	}
	if err := buf.Enqueue("demo", "payload"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	wrongKey := []byte("abcdefghijklmnopqrstuvwxyz123456")
	if _, err := NewDiskBuffer(path, 10, wrongKey); err == nil {
		t.Fatal("expected wrong key to prevent restoring encrypted data")
	}
}
