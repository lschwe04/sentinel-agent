package buffer

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MetricPayload repräsentiert gepufferte Telemetrie- oder Hardening-Daten
type MetricPayload struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // "metric" oder "hardening"
	Data      any       `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

// DiskBuffer verwaltet einen thread-sicheren, dateibasierten FIFO-Ringbuffer
type DiskBuffer struct {
	mu       sync.Mutex
	filePath string
	maxSize  int
	queue    []MetricPayload
}

// NewDiskBuffer erstellt einen Buffer mit automatischer Wiederherstellung beim Start
func NewDiskBuffer(storagePath string, maxSize int) (*DiskBuffer, error) {
	if err := os.MkdirAll(filepath.Dir(storagePath), 0700); err != nil {
		return nil, fmt.Errorf("buffer dir creation failed: %w", err)
	}

	buf := &DiskBuffer{
		filePath: storagePath,
		maxSize:  maxSize,
		queue:    make([]MetricPayload, 0),
	}

	if err := buf.loadFromDisk(); err != nil && !os.IsNotExist(err) {
		slog.Warn("Failed to restore buffer from disk, starting fresh", "error", err)
	}

	return buf, nil
}

// Enqueue fügt ein Event hinzu. Bei Überschreitung von maxSize wird das älteste verworfen (Ringbuffer).
func (b *DiskBuffer) Enqueue(eventType string, data any) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	item := MetricPayload{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}

	if len(b.queue) >= b.maxSize {
		b.queue = b.queue[1:] // Drop oldest
		slog.Warn("Ringbuffer full: dropped oldest record", "max_size", b.maxSize)
	}

	b.queue = append(b.queue, item)
	return b.persistToDisk()
}

// FlushCompress komprimiert alle gepufferten Daten per GZIP und sendet sie geordnet an die Hub-Funktion
func (b *DiskBuffer) FlushCompress(ctx context.Context, sendFunc func(ctx context.Context, compressedGzip []byte) error) error {
	b.mu.Lock()
	if len(b.queue) == 0 {
		b.mu.Unlock()
		return nil
	}

	// Kopie ziehen für die Übertragung
	itemsToSend := make([]MetricPayload, len(b.queue))
	copy(itemsToSend, b.queue)
	b.mu.Unlock()

	rawJSON, err := json.Marshal(itemsToSend)
	if err != nil {
		return fmt.Errorf("buffer serialization failed: %w", err)
	}

	var gzipBuffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&gzipBuffer)
	if _, err := gzipWriter.Write(rawJSON); err != nil {
		_ = gzipWriter.Close()
		return fmt.Errorf("gzip compression failed: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("gzip writer close failed: %w", err)
	}

	// Sendevorgang ausführen
	if err := sendFunc(ctx, gzipBuffer.Bytes()); err != nil {
		return fmt.Errorf("flush callback failed: %w", err)
	}

	// Nach erfolgreichem Senden den Buffer leeren
	b.mu.Lock()
	defer b.mu.Unlock()
	b.queue = b.queue[len(itemsToSend):]
	return b.persistToDisk()
}

func (b *DiskBuffer) persistToDisk() error {
	data, err := json.Marshal(b.queue)
	if err != nil {
		return err
	}
	tmpFile := b.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpFile, b.filePath)
}

func (b *DiskBuffer) loadFromDisk() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	file, err := os.Open(b.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &b.queue)
}
