package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"time"
)

type BackupStatus struct {
	Status  string `json:"status"`
	LastRun string `json:"last_run"`
	Details string `json:"details"`
}

func CheckResticStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	binary := os.Getenv("RESTIC_BINARY")
	if binary == "" {
		binary = "/usr/bin/restic"
	}
	status, err := readResticStatus(r.Context(), binary)
	if err != nil {
		status = BackupStatus{Status: "error", Details: err.Error()}
		writeBackupStatus(w, status, http.StatusServiceUnavailable)
		return
	}
	writeBackupStatus(w, status, http.StatusOK)
}

type resticSnapshot struct {
	Time time.Time `json:"time"`
}

var runRestic = func(ctx context.Context, binary string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	return cmd.Output()
}

func readResticStatus(parent context.Context, binary string) (BackupStatus, error) {
	if binary == "" {
		return BackupStatus{}, fmt.Errorf("RESTIC_BINARY ist leer")
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()

	args := []string{"snapshots", "--json"}
	output, err := runRestic(ctx, binary, args...)
	if err != nil {
		return BackupStatus{}, fmt.Errorf("restic snapshots fehlgeschlagen: %w", err)
	}

	var snapshots []resticSnapshot
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return BackupStatus{}, fmt.Errorf("restic JSON konnte nicht verarbeitet werden: %w", err)
	}
	if len(snapshots) == 0 {
		return BackupStatus{Status: "warning", Details: "Restic ist erreichbar, enthält aber keine Snapshots."}, nil
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Time.After(snapshots[j].Time) })
	return BackupStatus{
		Status:  "success",
		LastRun: snapshots[0].Time.UTC().Format(time.RFC3339),
		Details: fmt.Sprintf("%d Restic-Snapshot(s) gefunden.", len(snapshots)),
	}, nil
}

func writeBackupStatus(w http.ResponseWriter, status BackupStatus, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(status)
}
