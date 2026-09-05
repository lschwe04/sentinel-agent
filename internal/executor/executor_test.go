package executor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunAnsiblePlaybookRejectsMissingOrInvalidSignature(t *testing.T) {
	t.Setenv("REMOTE_HARDENING_HMAC_SECRET", "test-secret")

	request := httptest.NewRequest(http.MethodPost, "/hardening", nil)
	recorder := httptest.NewRecorder()
	RunAnsiblePlaybook(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized response, got %d", recorder.Code)
	}
}

func TestSecureRunnerAcceptsSignatureForFixedHardeningCommand(t *testing.T) {
	secret := []byte("test-secret")
	payload := ansibleBinary + " " + ansiblePlaybook
	hash := hmac.New(sha256.New, secret)
	_, _ = hash.Write([]byte(payload))
	signature := hex.EncodeToString(hash.Sum(nil))

	if !NewSecureRunner(string(secret)).VerifySignature(payload, signature) {
		t.Fatal("expected valid hardening command signature")
	}
	if NewSecureRunner(string(secret)).VerifySignature("/bin/sh -c id", signature) {
		t.Fatal("signature must not authorize a different command")
	}
}
