package buffer

import (
	"context"
	"os"
	"path/filepath"
	"sync"
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

func TestDiskBufferRestoresAfterProcessRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buffer.dat")
	key := []byte("01234567890123456789012345678901")

	firstProcess, err := NewDiskBuffer(path, 10, key)
	if err != nil {
		t.Fatalf("create initial buffer: %v", err)
	}
	if err := firstProcess.Enqueue("demo", map[string]string{"sequence": "persisted-before-crash"}); err != nil {
		t.Fatalf("enqueue before simulated crash: %v", err)
	}

	// Do not flush or call a shutdown hook: constructing a second instance models a restart.
	restarted, err := NewDiskBuffer(path, 10, key)
	if err != nil {
		t.Fatalf("restore after simulated crash: %v", err)
	}
	if len(restarted.queue) != 1 || restarted.queue[0].Data.(map[string]any)["sequence"] != "persisted-before-crash" {
		t.Fatalf("persisted queue was not restored: %+v", restarted.queue)
	}
}

func TestDiskBufferDropsOldestEntriesAtFileLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buffer.dat")
	key := []byte("01234567890123456789012345678901")
	buf, err := NewDiskBufferWithLimits(path, 100, 180, key)
	if err != nil {
		t.Fatalf("create limited buffer: %v", err)
	}
	for sequence := 0; sequence < 10; sequence++ {
		if err := buf.Enqueue("demo", map[string]int{"sequence": sequence}); err != nil {
			t.Fatalf("enqueue %d: %v", sequence, err)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat limited buffer: %v", err)
	}
	if info.Size() > 180 {
		t.Fatalf("buffer file exceeded hard limit: %d", info.Size())
	}
	if len(buf.queue) >= 10 {
		t.Fatal("expected oldest entries to be dropped at file limit")
	}
	last := buf.queue[len(buf.queue)-1].Data.(map[string]int)["sequence"]
	if last != 9 {
		t.Fatalf("newest entry was not retained: %v", last)
	}
}

func TestDiskBufferReleasesMemoryWithoutLosingDiskQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buffer.dat")
	key := []byte("01234567890123456789012345678901")
	buf, err := NewDiskBuffer(path, 10, key)
	if err != nil {
		t.Fatalf("create buffer: %v", err)
	}
	if err := buf.Enqueue("demo", "survive-memory-release"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := buf.ReleaseMemoryToDisk(); err != nil {
		t.Fatalf("release memory: %v", err)
	}
	if len(buf.queue) != 0 || !buf.diskOnly {
		t.Fatalf("buffer was not released to disk: queue=%d diskOnly=%v", len(buf.queue), buf.diskOnly)
	}
	if err := buf.Enqueue("demo", "new-item"); err != nil {
		t.Fatalf("enqueue after memory release: %v", err)
	}
	if len(buf.queue) != 2 {
		t.Fatalf("disk queue was not reloaded before enqueue: %d items", len(buf.queue))
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

func TestDiskBufferRejectsCorruptFileOnRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buffer.dat")
	key := []byte("01234567890123456789012345678901")
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatalf("write corrupt buffer: %v", err)
	}
	if _, err := NewDiskBuffer(path, 10, key); err == nil {
		t.Fatal("expected corrupt buffer to be rejected")
	}
}

func TestDiskBufferClosePersistsAndRejectsWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buffer.dat")
	key := []byte("01234567890123456789012345678901")
	buf, err := NewDiskBuffer(path, 10, key)
	if err != nil {
		t.Fatalf("create buffer: %v", err)
	}
	if err := buf.Enqueue("demo", "before-close"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := buf.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := buf.Enqueue("demo", "after-close"); err == nil {
		t.Fatal("expected enqueue after close to fail")
	}
	restarted, err := NewDiskBuffer(path, 10, key)
	if err != nil {
		t.Fatalf("restore after close: %v", err)
	}
	if len(restarted.queue) != 1 {
		t.Fatalf("expected one persisted item, got %d", len(restarted.queue))
	}
}

func TestDiskBufferSerializesConcurrentFlushes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buffer.dat")
	key := []byte("01234567890123456789012345678901")
	buf, err := NewDiskBuffer(path, 10, key)
	if err != nil {
		t.Fatalf("create buffer: %v", err)
	}
	if err := buf.Enqueue("demo", "once"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	var wait sync.WaitGroup
	var calls int
	var callsMu sync.Mutex
	flush := func() {
		defer wait.Done()
		if err := buf.FlushCompress(context.Background(), func(context.Context, []byte) error {
			callsMu.Lock()
			calls++
			callsMu.Unlock()
			return nil
		}); err != nil {
			t.Errorf("flush: %v", err)
		}
	}
	wait.Add(2)
	go flush()
	go flush()
	wait.Wait()
	if calls != 1 {
		t.Fatalf("expected one send for one queue item, got %d", calls)
	}
}
