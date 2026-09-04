// sentinel-agent: internal/updater/updater.go
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type UpdateManifest struct {
	Version   string `json:"version"`
	BinaryURL string `json:"binary_url"`
	Checksum  string `json:"checksum"`  // SHA256 des Binaries
	Signature string `json:"signature"` // Hex-kodierte Signatur
}

// CheckAndApplyUpdate prüft beim Hub periodisch auf neue Agenten-Versionen
func CheckAndApplyUpdate(ctx context.Context, hubUpdateURL string, currentVersion string, caCertPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hubUpdateURL+"?version="+currentVersion, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("update check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		// Agent ist aktuell
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code from update server: %d", resp.StatusCode)
	}

	var manifest UpdateManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return fmt.Errorf("failed to decode update manifest: %w", err)
	}

	slog.Info("Neues Agenten-Update entdeckt, starte sicheren Download...", "version", manifest.Version)

	// 1. Binärdatei temporär herunterladen
	tmpFile, err := downloadBinary(ctx, manifest.BinaryURL)
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	// 2. SHA256 Checksumme verifizieren (Sicherheit NR. 1)
	if err := verifyChecksum(tmpFile, manifest.Checksum); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	// 3. Binärdatei an Ort des Originals verschieben & Service via systemd neustarten
	targetPath := "/opt/sentinel-agent/sentinel-agent"
	if err := atomicReplace(tmpFile, targetPath); err != nil {
		return fmt.Errorf("atomic binary replacement failed: %w", err)
	}

	slog.Info("Agent erfolgreich aktualisiert. Starte Systemd-Service neu...")
	cmd := exec.CommandContext(ctx, "systemctl", "restart", "sentinel-agent")
	return cmd.Run()
}

func downloadBinary(_ context.Context, url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp("", "sentinel-update-*")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return "", err
	}

	return tmp.Name(), nil
}

func verifyChecksum(filePath string, expectedSHA256 string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	sum := hex.EncodeToString(h.Sum(nil))
	if sum != expectedSHA256 {
		return fmt.Errorf("hash mismatch: got %s, want %s", sum, expectedSHA256)
	}
	return nil
}

func atomicReplace(src, dst string) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()

	dir := filepath.Dir(dst)
	tmpDst, err := os.CreateTemp(dir, "sentinel-update-bin-*")
	if err != nil {
		return err
	}
	defer tmpDst.Close()

	if _, err := io.Copy(tmpDst, input); err != nil {
		return err
	}

	if err := os.Chmod(tmpDst.Name(), 0755); err != nil {
		return err
	}

	return os.Rename(tmpDst.Name(), dst)
}
