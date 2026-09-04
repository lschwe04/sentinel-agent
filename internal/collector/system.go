package collector

import (
	"context"
	"math/rand"
	"time"

	"sentinel-agent/internal/buffer"
)

type SystemCollector struct {
	buffer   *buffer.DiskBuffer
	interval time.Duration
	nodeID   string
}

func NewSystemCollector(buf *buffer.DiskBuffer, interval time.Duration, nodeID string) *SystemCollector {
	return &SystemCollector{
		buffer:   buf,
		interval: interval,
		nodeID:   nodeID,
	}
}

// Start blockiert und sammelt kontinuierlich Metriken
func (c *SystemCollector) Start(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// In Produktion: gopsutil oder cgroups auslesen
			// Hier als Mock für die Auslastung
			cpuPct := 20.0 + rand.Float64()*30.0
			ramPct := 40.0 + rand.Float64()*10.0

			payload := map[string]any{
				"node_id":       c.nodeID,
				"cpu_usage_pct": cpuPct,
				"ram_usage_pct": ramPct,
				"status":        "healthy",
			}

			if err := c.buffer.Enqueue("system_metric", payload); err != nil {
				// Hier reicht ein lokaler Log, der Buffer verarbeitet Drops selbst
				_ = err
			}
		}
	}
}
