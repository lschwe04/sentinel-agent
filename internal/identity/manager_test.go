package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	if err := os.WriteFile(path, []byte(`{"agent_id":"agent-1","tenant_id":"tenant-1"}`), 0600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	result, err := Load(path)
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	if result.AgentID != "agent-1" || result.TenantID != "tenant-1" {
		t.Fatalf("unexpected identity: %+v", result)
	}
}

func TestLoadIdentityRejectsIncompleteData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	if err := os.WriteFile(path, []byte(`{"agent_id":"agent-1"}`), 0600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected incomplete identity to fail")
	}
}
