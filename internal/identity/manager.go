package identity

import (
	"encoding/json"
	"fmt"
	"os"
)

type Identity struct {
	AgentID  string `json:"agent_id"`
	TenantID string `json:"tenant_id"`
}

func Load(path string) (Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Identity{}, fmt.Errorf("read identity file: %w", err)
	}
	var result Identity
	if err := json.Unmarshal(data, &result); err != nil {
		return Identity{}, fmt.Errorf("decode identity file: %w", err)
	}
	if result.AgentID == "" || result.TenantID == "" {
		return Identity{}, fmt.Errorf("identity file must contain agent_id and tenant_id")
	}
	return result, nil
}

func LoadFromEnvironmentOrFile(path string) (Identity, error) {
	if path != "" {
		result, err := Load(path)
		if err == nil {
			return result, nil
		}
		if !os.IsNotExist(err) {
			return Identity{}, err
		}
	}
	result := Identity{AgentID: os.Getenv("AGENT_ID"), TenantID: os.Getenv("TENANT_ID")}
	if result.AgentID == "" || result.TenantID == "" {
		return Identity{}, fmt.Errorf("identity file is missing and AGENT_ID/TENANT_ID are incomplete")
	}
	return result, nil
}
