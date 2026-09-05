// sentinel-agent: internal/hardening/fim.go
package hardening

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"time"
)

type FIMScanner struct {
	monitoredPaths []string
	baselineCache  map[string]string
}

type FIMAlert struct {
	NodeID    string    `json:"node_id"`
	FilePath  string    `json:"file_path"`
	Event     string    `json:"event"` // "MODIFIED", "DELETED"
	Timestamp time.Time `json:"timestamp"`
}

func NewFIMScanner(paths []string) *FIMScanner {
	return &FIMScanner{
		monitoredPaths: paths,
		baselineCache:  make(map[string]string),
	}
}

// BuildBaseline erstellt den initialen kryptografischen Fingerabdruck kritischer Dateien
func (f *FIMScanner) BuildBaseline() {
	for _, path := range f.monitoredPaths {
		hash, err := calculateSHA256(path)
		if err == nil {
			f.baselineCache[path] = hash
			slog.Info("FIM Baseline registriert", "path", path, "hash", hash[:12])
		}
	}
}

// CheckIntegrity prüft im laufenden Betrieb auf unautorisierte Manipulationen (Sicherheit Prio 1)
func (f *FIMScanner) CheckIntegrity(nodeID string) []FIMAlert {
	var alerts []FIMAlert

	for _, path := range f.monitoredPaths {
		currentHash, err := calculateSHA256(path)
		if err != nil {
			alerts = append(alerts, FIMAlert{
				NodeID:    nodeID,
				FilePath:  path,
				Event:     "DELETED_OR_UNACCESSIBLE",
				Timestamp: time.Now().UTC(),
			})
			continue
		}

		if oldHash, exists := f.baselineCache[path]; exists {
			if oldHash != currentHash {
				slog.Error("SICHERHEITSALARM: Kritische Datei manipuliert!", "path", path)
				alerts = append(alerts, FIMAlert{
					NodeID:    nodeID,
					FilePath:  path,
					Event:     "MODIFIED",
					Timestamp: time.Now().UTC(),
				})
			}
		}
	}
	return alerts
}

func calculateSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
