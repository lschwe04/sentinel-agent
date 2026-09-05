package collector

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sentinel-agent/internal/buffer"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

type SystemCollector struct {
	buffer   *buffer.DiskBuffer
	interval time.Duration
	nodeID   string
}

func NewSystemCollector(buf *buffer.DiskBuffer, interval time.Duration, nodeID string) *SystemCollector {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &SystemCollector{
		buffer:   buf,
		interval: interval,
		nodeID:   nodeID,
	}
}

func collectSystemMetrics(ctx context.Context, nodeID string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cpuPercentages, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		return nil, fmt.Errorf("CPU-Metriken konnten nicht gelesen werden: %w", err)
	}
	if len(cpuPercentages) == 0 {
		return nil, fmt.Errorf("CPU-Metriken enthielten keinen Wert")
	}

	vmStat, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("RAM-Metriken konnten nicht gelesen werden: %w", err)
	}

	return map[string]any{
		"node_id":       nodeID,
		"cpu_usage_pct": cpuPercentages[0],
		"ram_usage_pct": vmStat.UsedPercent,
		"timestamp":     time.Now().UTC().Format(time.RFC3339Nano),
		"status":        "healthy",
	}, nil
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
			payload, err := collectSystemMetrics(ctx, c.nodeID)
			if err != nil {
				slog.Warn("Systemmetriken konnten nicht erfasst werden", "error", err)
				continue
			}
			if err := c.buffer.Enqueue("system_metric", payload); err != nil {
				slog.Error("Systemmetriken konnten nicht gepuffert werden", "error", err)
			}
		}
	}
}
