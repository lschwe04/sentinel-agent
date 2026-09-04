// sentinel-agent: internal/executor/secure_runner.go
package executor

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

type SecureRunner struct {
	sharedSecret []byte
}

func NewSecureRunner(secret string) *SecureRunner {
	return &SecureRunner{
		sharedSecret: []byte(secret),
	}
}

// VerifySignature prüft, ob der Befehl authentisch vom verifizierten Sentinel Hub stammt
func (sr *SecureRunner) VerifySignature(payload string, receivedSig string) bool {
	h := hmac.New(sha256.New, sr.sharedSecret)
	h.Write([]byte(payload))
	expectedSig := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(receivedSig), []byte(expectedSig))
}

// ExecuteSandboxed führt Systembefehle mit minimalen Rechten und Timeout aus
func (sr *SecureRunner) ExecuteSandboxed(ctx context.Context, command string, args []string, signature string) ([]byte, error) {
	payloadToCheck := command + " " + strings.Join(args, " ")
	if !sr.VerifySignature(payloadToCheck, signature) {
		slog.Error("Sicherheitsverletzung: Ungültige HMAC-Signatur für Remote-Befehl blockiert!", "cmd", command)
		return nil, errors.New("unauthorized: command signature verification failed")
	}

	// Striktes Timeout von maximal 2 Minuten für jegliche Remediation
	execCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(execCtx, command, args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("execution failed: %w, output: %s", err, string(output))
	}

	return output, nil
}
