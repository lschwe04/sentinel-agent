package updater

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifySignatureAcceptsValidBinary(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	path := filepath.Join(t.TempDir(), "agent")
	payload := []byte("signed-agent-binary")
	if err := os.WriteFile(path, payload, 0700); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	digest := sha256.Sum256(payload)
	signature := hex.EncodeToString(ed25519.Sign(privateKey, digest[:]))
	if err := verifySignature(path, hex.EncodeToString(digest[:]), signature, publicKey); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
}

func TestVerifySignatureRejectsTamperedBinary(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	path := filepath.Join(t.TempDir(), "agent")
	original := []byte("signed-agent-binary")
	if err := os.WriteFile(path, original, 0700); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	digest := sha256.Sum256(original)
	signature := hex.EncodeToString(ed25519.Sign(privateKey, digest[:]))
	if err := os.WriteFile(path, []byte("tampered-binary"), 0700); err != nil {
		t.Fatalf("tamper binary: %v", err)
	}
	if err := verifySignature(path, hex.EncodeToString(digest[:]), signature, publicKey); err == nil {
		t.Fatal("tampered binary passed signature verification")
	}
}

func TestAtomicReplaceSynchronizesAndPreservesNewBinary(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "download")
	target := filepath.Join(directory, "agent")
	if err := os.WriteFile(source, []byte("new-agent"), 0600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(target, []byte("old-agent"), 0700); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := atomicReplace(source, target); err != nil {
		t.Fatalf("atomic replace: %v", err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(content) != "new-agent" {
		t.Fatalf("unexpected target content: %q", content)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source should remain available for caller cleanup: %v", err)
	}
}
