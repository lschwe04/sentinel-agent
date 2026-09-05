package executor

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"time"

	"sentinel-agent/internal/identity"
)

type ExecutionResponse struct {
	JobID   string `json:"job_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type HardeningReport struct {
	NodeID     string `json:"node_id"`
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	OpenIssues int    `json:"open_issues"`
}

func RunAnsiblePlaybook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := fmt.Sprintf("job-%d", time.Now().UnixNano())
	go func(id string) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		slog.Info("Starte asynchronen Härtungsprozess", "job_id", id)
		cmd := exec.CommandContext(ctx, "/usr/bin/ansible-playbook", "/etc/sentinel/playbooks/hardening.yml")
		output, err := cmd.CombinedOutput()
		success := true
		msg := "Hardening successfully applied"
		openIssues := 0
		if err != nil {
			slog.Error("Ansible Härtung fehlgeschlagen", "job_id", id, "error", err, "output", string(output))
			success = false
			msg = fmt.Sprintf("Execution error: %v", err)
			openIssues = 3
		} else {
			slog.Info("Ansible Härtung erfolgreich abgeschlossen", "job_id", id)
		}
		reportBackToHub(success, msg, openIssues)
	}(jobID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(ExecutionResponse{JobID: jobID, Status: "accepted", Message: "Hardening process queued and running in background"})
}

func reportBackToHub(success bool, message string, openIssues int) {
	hubURL := os.Getenv("HUB_CALLBACK_URL")
	if hubURL == "" {
		hubURL = "https://10.0.0.1:8443/api/v1/hardening/report"
	}
	nodeID := os.Getenv("NODE_ID")
	if nodeID == "" {
		nodeID = "unknown-node"
	}
	report := HardeningReport{NodeID: nodeID, Success: success, Message: message, OpenIssues: openIssues}
	data, err := json.Marshal(report)
	if err != nil {
		slog.Error("Fehler beim Marshalling des Reports", "error", err)
		return
	}

	certPath := getEnvOrDefault("AGENT_CERT_PATH", "/etc/sentinel/certs/agent.crt")
	keyPath := getEnvOrDefault("AGENT_KEY_PATH", "/etc/sentinel/certs/agent.key")
	caPath := getEnvOrDefault("CA_CERT_PATH", "/etc/sentinel/certs/ca.crt")
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		slog.Error("mTLS Zertifikat Fehler beim Callback", "error", err)
		return
	}
	caCert, err := os.ReadFile(caPath)
	if err != nil {
		slog.Error("CA Zertifikat Ladefehler beim Callback", "error", err)
		return
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		slog.Error("CA Zertifikat konnte nicht geladen werden")
		return
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: caCertPool, MinVersion: tls.VersionTLS13}}, Timeout: 10 * time.Second}

	req, err := http.NewRequest(http.MethodPost, hubURL, bytes.NewBuffer(data))
	if err != nil {
		slog.Error("Fehler beim Erstellen des Requests", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	identityPath := getEnvOrDefault("AGENT_IDENTITY_PATH", "/etc/sentinel-agent/identity.json")
	if agentIdentity, identityErr := identity.LoadFromEnvironmentOrFile(identityPath); identityErr == nil {
		req.Header.Set("X-Tenant-ID", agentIdentity.TenantID)
		req.Header.Set("X-Agent-ID", agentIdentity.AgentID)
	}
	if authToken := os.Getenv("ENTERPRISE_AUTH_TOKEN"); authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		slog.Error("Konnte Hardening-Status nicht an Hub senden", "error", err)
		return
	}
	defer resp.Body.Close()
	slog.Info("Hardening-Status erfolgreich an Hub gemeldet", "status_code", resp.StatusCode)
}

func getEnvOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
