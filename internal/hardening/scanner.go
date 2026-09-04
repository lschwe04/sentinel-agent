// sentinel-agent: internal/hardening/scanner.go (Ausgebessert)
package hardening

import (
	"context"
	"os"
	"os/exec"
)

func ValidateCISLevel1(ctx context.Context) (bool, int) {
	openIssues := 0

	// Check 1: sshd_config Permissions (muss 0600 sein)
	if stat, err := os.Stat("/etc/ssh/sshd_config"); err == nil {
		if stat.Mode().Perm() != 0600 {
			openIssues++
		}
	} else {
		openIssues++
	}

	// Check 2: Auditd Service aktiv und nicht manipuliert
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", "auditd")
	if err := cmd.Run(); err != nil {
		openIssues++
	}

	return openIssues == 0, openIssues
}
