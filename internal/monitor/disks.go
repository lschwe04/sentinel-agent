package monitor

import (
	"github.com/shirou/gopsutil/v3/disk"
)

// GetDiskUsage prüft die Auslastung der Root-Partition
func GetDiskUsage() float64 {
	// Auf Linux standardmäßig "/" prüfen
	usage, err := disk.Usage("/")
	if err != nil {
		return 0.0
	}
	return usage.UsedPercent
}
