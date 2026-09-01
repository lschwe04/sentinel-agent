package executor

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"time"
)

type ExecutionResponse struct {
	JobID   string `json:"job_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func RunAnsiblePlaybook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := fmt.Sprintf("job-%d", time.Now().UnixNano())

	// Asynchrone Ausführung verhindert HTTP-Timeouts
	go func(id string) {
		slog.Info("Starte asynchronen Härtungsprozess", "job_id", id)
		cmd := exec.Command("ansible-playbook", "/etc/sentinel/playbooks/hardening.yml")

		if err := cmd.Run(); err != nil {
			slog.Error("Ansible Härtung fehlgeschlagen", "job_id", id, "error", err)
			// Hier würde künftig ein Webhook an den Hub den Fehler melden
			return
		}
		slog.Info("Ansible Härtung erfolgreich abgeschlossen", "job_id", id)
	}(jobID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted) // 202 Accepted statt 200 OK

	json.NewEncoder(w).Encode(ExecutionResponse{
		JobID:   jobID,
		Status:  "accepted",
		Message: "Hardening process queued and running in background",
	})
}
