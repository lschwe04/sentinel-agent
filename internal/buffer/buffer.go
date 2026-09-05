package buffer

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type MetricPayload struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // "metric" oder "hardening"
	Data      any       `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

type DiskBuffer struct {
	mu           sync.Mutex
	filePath     string
	maxSize      int
	maxFileBytes int64
	aesKey       []byte
	queue        []MetricPayload
	diskOnly     bool
}

const defaultMaxFileBytes int64 = 10 * 1024 * 1024

func NewDiskBuffer(storagePath string, maxSize int, masterKey []byte) (*DiskBuffer, error) {
	return NewDiskBufferWithLimits(storagePath, maxSize, defaultMaxFileBytes, masterKey)
}

// NewDiskBufferWithLimits creates an AES-GCM disk-backed FIFO with item and byte limits.
func NewDiskBufferWithLimits(storagePath string, maxSize int, maxFileBytes int64, masterKey []byte) (*DiskBuffer, error) {
	if len(masterKey) != 32 {
		return nil, errors.New("AES-256 Key muss exakt 32 Byte lang sein")
	}
	if maxSize <= 0 {
		return nil, errors.New("buffer max size must be greater than zero")
	}
	if maxFileBytes <= 0 {
		return nil, errors.New("buffer max file size must be greater than zero")
	}
	if err := os.MkdirAll(filepath.Dir(storagePath), 0700); err != nil {
		return nil, fmt.Errorf("buffer dir creation failed: %w", err)
	}

	buf := &DiskBuffer{
		filePath:     storagePath,
		maxSize:      maxSize,
		maxFileBytes: maxFileBytes,
		aesKey:       append([]byte(nil), masterKey...),
		queue:        make([]MetricPayload, 0),
	}

	if err := buf.loadFromDisk(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to restore encrypted buffer: %w", err)
	}

	return buf, nil
}

func (b *DiskBuffer) Enqueue(eventType string, data any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ensureLoadedLocked(); err != nil {
		return err
	}
	previousQueue := append([]MetricPayload(nil), b.queue...)

	item := MetricPayload{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}

	if len(b.queue) >= b.maxSize {
		b.queue = b.queue[1:]
		slog.Warn("Ringbuffer full: dropped oldest record", "max_size", b.maxSize)
	}

	b.queue = append(b.queue, item)
	if err := b.persistToDiskLocked(); err != nil {
		b.queue = previousQueue
		return err
	}
	return nil
}

// Sync durably persists the current in-memory queue without contacting the hub.
func (b *DiskBuffer) Sync() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.diskOnly {
		return nil
	}
	return b.persistToDiskLocked()
}

// ReleaseMemoryToDisk keeps the durable queue on disk and releases its in-memory copy.
func (b *DiskBuffer) ReleaseMemoryToDisk() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.diskOnly {
		return nil
	}
	if err := b.persistToDiskLocked(); err != nil {
		return err
	}
	b.queue = nil
	b.diskOnly = true
	return nil
}

// FlushCompress (Ihr genialer Code bleibt erhalten!)
func (b *DiskBuffer) FlushCompress(ctx context.Context, sendFunc func(ctx context.Context, compressedGzip []byte) error) error {
	b.mu.Lock()
	if err := b.ensureLoadedLocked(); err != nil {
		b.mu.Unlock()
		return err
	}
	if len(b.queue) == 0 {
		b.mu.Unlock()
		return nil
	}

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

	if err := sendFunc(ctx, gzipBuffer.Bytes()); err != nil {
		return fmt.Errorf("flush callback failed: %w", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	previousQueue := b.queue
	b.queue = b.queue[len(itemsToSend):]
	if err := b.persistToDiskLocked(); err != nil {
		b.queue = previousQueue
		return err
	}
	return nil
}

func (b *DiskBuffer) persistToDiskLocked() error {
	hadItems := len(b.queue) > 0
	block, err := aes.NewCipher(b.aesKey)
	if err != nil {
		return err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	for len(b.queue) > 0 {
		rawJSON, err := json.Marshal(b.queue)
		if err != nil {
			return err
		}
		if int64(len(rawJSON)+gcm.NonceSize()+gcm.Overhead()) <= b.maxFileBytes {
			nonce := make([]byte, gcm.NonceSize())
			if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
				return err
			}
			ciphertext := gcm.Seal(nonce, nonce, rawJSON, nil)
			return b.writeAtomically(ciphertext)
		}

		slog.Warn("Disk-Buffer-Limit erreicht; ältestes Ereignis verworfen", "component", "buffer", "max_file_bytes", b.maxFileBytes)
		b.queue = b.queue[1:]
	}

	if err := os.Remove(b.filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if hadItems {
		return fmt.Errorf("buffer item exceeds configured file limit of %d bytes", b.maxFileBytes)
	}
	return nil
}

func (b *DiskBuffer) writeAtomically(ciphertext []byte) error {
	tmpFile := b.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, ciphertext, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmpFile, b.filePath); err != nil {
		_ = os.Remove(tmpFile)
		return err
	}
	return nil
}

// NEU: Entschlüsseltes Laden von der Festplatte
func (b *DiskBuffer) loadFromDisk() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.loadFromDiskLocked()
}

func (b *DiskBuffer) ensureLoadedLocked() error {
	if !b.diskOnly {
		return nil
	}
	if err := b.loadFromDiskLocked(); err != nil {
		return err
	}
	b.diskOnly = false
	return nil
}

func (b *DiskBuffer) loadFromDiskLocked() error {
	info, err := os.Stat(b.filePath)
	if err != nil {
		return err
	}
	if info.Size() > b.maxFileBytes {
		return fmt.Errorf("buffer file exceeds configured limit: %d > %d bytes", info.Size(), b.maxFileBytes)
	}
	ciphertext, err := os.ReadFile(b.filePath)
	if err != nil {
		return err
	}

	block, err := aes.NewCipher(b.aesKey)
	if err != nil {
		return err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return errors.New("ciphertext zu kurz")
	}

	nonce, encryptedData := ciphertext[:nonceSize], ciphertext[nonceSize:]
	rawJSON, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return errors.New("entschlüsselung fehlgeschlagen - Integritätsverletzung oder falscher Key")
	}

	if err := json.Unmarshal(rawJSON, &b.queue); err != nil {
		return err
	}
	if len(b.queue) > b.maxSize {
		b.queue = b.queue[len(b.queue)-b.maxSize:]
	}
	return nil
}
