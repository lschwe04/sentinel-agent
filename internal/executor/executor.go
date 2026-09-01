package executor

import (
	"encoding/json"
	"net/http"
	"os/exec"
)

type ExecutionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func RunAnsiblePlaybook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Ansible Playbook lokal ausführen
	cmd := exec.Command("ansible-playbook", "/etc/sentinel/playbooks/hardening.yml")
	err := cmd.Run()

	res := ExecutionResponse{
		Success: err == nil,
		Message: "Ansible playbook executed successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		res.Message = "Execution failed: " + err.Error()
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(res)
}
