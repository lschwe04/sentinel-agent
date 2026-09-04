// sentinel-agent: internal/executor/watchdog.go
package executor

import (
	"context"
	"log/slog"
	"os/exec"
	"time"
)

type Watchdog struct {
	targetService string
	interval      time.Duration
}

func NewWatchdog(serviceName string, checkInterval time.Duration) *Watchdog {
	return &Watchdog{
		targetService: serviceName,
		interval:      checkInterval,
	}
}

// MonitorAndHeal prüft periodisch den Zustand des Agenten-Subsystems und greift autonom ein
func (w *Watchdog) MonitorAndHeal(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Watchdog-Prozess beendet.")
			return
		case <-ticker.C:
			cmd := exec.CommandContext(ctx, "systemctl", "is-active", w.targetService)
			if err := cmd.Run(); err != nil {
				slog.Warn("Watchdog hat Ausfall detektiert, starte automatische Wiederherstellung...", "service", w.targetService)

				healCmd := exec.CommandContext(ctx, "systemctl", "restart", w.targetService)
				if healErr := healCmd.Run(); healErr != nil {
					slog.Error("Kritisch: Automatischer Neustart des Services fehlgeschlagen", "error", healErr)
				} else {
					slog.Info("Service erfolgreich durch Watchdog wiederhergestellt", "service", w.targetService)
				}
			}
		}
	}
}
