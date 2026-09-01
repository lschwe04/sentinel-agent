package executor

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"time"
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

	// Asynchrone Ausführung des Härtungsprozesses
	go func(id string) {
		slog.Info("Starte asynchronen Härtungsprozess", "job_id", id)
		cmd := exec.Command("ansible-playbook", "/etc/sentinel/playbooks/hardening.yml")

		err := cmd.Run()
		success := true
		msg := "Hardening successfully applied"
		openIssues := 0

		if err != nil {
			slog.Error("Ansible Härtung fehlgeschlagen", "job_id", id, "error", err)
			success = false
			msg = err.Error()
			openIssues = 3
		} else {
			slog.Info("Ansible Härtung erfolgreich abgeschlossen", "job_id", id)
		}

		// Closed-Loop: Status direkt per mTLS an den Hub melden
		reportBackToHub(success, msg, openIssues)
	}(jobID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(ExecutionResponse{
		JobID:   jobID,
		Status:  "accepted",
		Message: "Hardening process queued and running in background",
	})
}

func reportBackToHub(success bool, message string, openIssues int) {
	hubURL := os.Getenv("HUB_CALLBACK_URL")
	if hubURL == "" {
		hubURL = "https://sentinel-hub:8443/api/v1/hardening/report"
	}
	nodeID := os.Getenv("NODE_ID")
	if nodeID == "" {
		nodeID = "node-local-docker"
	}

	report := HardeningReport{
		NodeID:     nodeID,
		Success:    success,
		Message:    message,
		OpenIssues: openIssues,
	}

	data, err := json.Marshal(report)
	if err != nil {
		slog.Error("Fehler beim Marshalling des Reports", "error", err)
		return
	}

	// mTLS Client Konfiguration für sicheren Callback
	cert, err := tls.LoadX509KeyPair("/etc/sentinel/certs/agent.crt", "/etc/sentinel/certs/agent.key")
	if err != nil {
		slog.Error("mTLS Zertifikat Fehler beim Callback", "error", err)
		return
	}
	caCert, _ := os.ReadFile("/etc/sentinel/certs/ca.crt")
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      caCertPool,
				MinVersion:   tls.VersionTLS13,
			},
		},
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequest(http.MethodPost, hubURL, bytes.NewBuffer(data))
	if err != nil {
		slog.Error("Fehler beim Erstellen des Requests", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		slog.Error("Konnte Hardening-Status nicht an Hub senden", "error", err)
		return
	}
	defer resp.Body.Close()
	slog.Info("Hardening-Status erfolgreich an Hub gemeldet", "status_code", resp.StatusCode)
}
