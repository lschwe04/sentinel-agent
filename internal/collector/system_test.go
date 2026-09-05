package collector

import (
	"context"
	"testing"
	"time"
)

func TestCollectSystemMetricsReturnsRealBoundedValues(t *testing.T) {
	metrics, err := collectSystemMetrics(context.Background(), "test-node")
	if err != nil {
		t.Fatalf("collect system metrics: %v", err)
	}

	for _, key := range []string{"cpu_usage_pct", "ram_usage_pct"} {
		value, ok := metrics[key].(float64)
		if !ok {
			t.Fatalf("metric %q has type %T, want float64", key, metrics[key])
		}
		if value < 0 || value > 100 {
			t.Fatalf("metric %q out of range: %v", key, value)
		}
	}
	if metrics["node_id"] != "test-node" {
		t.Fatalf("unexpected node id: %v", metrics["node_id"])
	}
}

func TestCollectSystemMetricsHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := collectSystemMetrics(ctx, "test-node"); err == nil {
		t.Fatal("expected canceled context to fail")
	}
}

func TestNewSystemCollectorNormalizesInvalidInterval(t *testing.T) {
	collector := NewSystemCollector(nil, 0, "test-node")
	if collector.interval != 30*time.Second {
		t.Fatalf("unexpected normalized interval: %s", collector.interval)
	}
}
