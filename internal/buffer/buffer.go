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
	mu       sync.Mutex
	filePath string
	maxSize  int
	aesKey   []byte // NEU: 32-Byte Key für AES-256
	queue    []MetricPayload
}

// NewDiskBuffer nimmt nun optional oder direkt einen MasterKey entgegen
func NewDiskBuffer(storagePath string, maxSize int, masterKey []byte) (*DiskBuffer, error) {
	if len(masterKey) != 32 {
		return nil, errors.New("AES-256 Key muss exakt 32 Byte lang sein")
	}
	if err := os.MkdirAll(filepath.Dir(storagePath), 0700); err != nil {
		return nil, fmt.Errorf("buffer dir creation failed: %w", err)
	}

	buf := &DiskBuffer{
		filePath: storagePath,
		maxSize:  maxSize,
		aesKey:   masterKey,
		queue:    make([]MetricPayload, 0),
	}

	if err := buf.loadFromDisk(); err != nil && !os.IsNotExist(err) {
		slog.Warn("Failed to restore buffer from disk, starting fresh", "error", err)
	}

	return buf, nil
}

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
		b.queue = b.queue[1:]
		slog.Warn("Ringbuffer full: dropped oldest record", "max_size", b.maxSize)
	}

	b.queue = append(b.queue, item)
	return b.persistToDisk()
}

// FlushCompress (Ihr genialer Code bleibt erhalten!)
func (b *DiskBuffer) FlushCompress(ctx context.Context, sendFunc func(ctx context.Context, compressedGzip []byte) error) error {
	b.mu.Lock()
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
	b.queue = b.queue[len(itemsToSend):]
	return b.persistToDisk()
}

// NEU: Verschlüsseltes Speichern auf die Festplatte (AES-256-GCM)
func (b *DiskBuffer) persistToDisk() error {
	rawJSON, err := json.Marshal(b.queue)
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

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}

	ciphertext := gcm.Seal(nonce, nonce, rawJSON, nil)
	tmpFile := b.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, ciphertext, 0600); err != nil {
		return err
	}
	return os.Rename(tmpFile, b.filePath)
}

// NEU: Entschlüsseltes Laden von der Festplatte
func (b *DiskBuffer) loadFromDisk() error {
	b.mu.Lock()
	defer b.mu.Unlock()

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

	return json.Unmarshal(rawJSON, &b.queue)
}
