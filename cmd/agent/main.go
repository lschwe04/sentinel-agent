package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"sentinel-agent/internal/buffer"
	"sentinel-agent/internal/executor"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	nodeID := os.Getenv("NODE_ID")
	tenantID := os.Getenv("TENANT_ID")
	customerID := os.Getenv("CUSTOMER_ID")
	hubMetricsURL := os.Getenv("HUB_METRICS_URL")
	hubBaseURL := os.Getenv("HUB_BASE_URL")
	enrollToken := os.Getenv("ENROLL_TOKEN")
	authToken := os.Getenv("ENTERPRISE_AUTH_TOKEN")

	if nodeID == "" || tenantID == "" || customerID == "" || hubMetricsURL == "" {
		slog.Error("CRITICAL: Erforderliche Umgebungsvariablen für Enterprise-Betrieb fehlen.")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Lokalen Disk-Buffer initialisieren (Sicherheit Nr. 1 & Skalierung Nr. 2)
	bufferPath := filepath.Join(getEnvOrDefault("AGENT_STATE_DIR", "/var/lib/sentinel"), "buffer.dat")
	diskBuffer, err := buffer.NewDiskBuffer(bufferPath, 10000, nil)
	if err != nil {
		slog.Error("Konnte lokalen Disk-Buffer nicht initialisieren", "error", err)
		os.Exit(1)
	}

	// 2. Automatisches Self-Enrollment beim Start
	if hubBaseURL != "" && enrollToken != "" {
		if custInt, err := strconv.Atoi(customerID); err == nil {
			performAutoEnrollment(ctx, hubBaseURL, enrollToken, nodeID, custInt)
		}
	}

	// 3. Watchdog für Hochverfügbarkeit starten
	watchdog := executor.NewWatchdog("sentinel-agent", 30*time.Second)
	go watchdog.MonitorAndHeal(ctx)

	// 4. Resilienten Collector & Reporter im Hintergrund starten
	go startResilientEngine(ctx, diskBuffer, nodeID, tenantID, customerID, hubMetricsURL, authToken)

	slog.Info("Sentinel Agent Enterprise Edition erfolgreich gestartet", "node_id", nodeID, "tenant_id", tenantID)
	<-ctx.Done()
	slog.Info("Agent wird geordnet heruntergefahren...")
}

func performAutoEnrollment(ctx context.Context, hubBaseURL, enrollToken, nodeID string, customerID int) {
	enrollPayload := map[string]interface{}{
		"enroll_token": enrollToken,
		"node_id":      nodeID,
		"customer_id":  customerID,
	}

	data, _ := json.Marshal(enrollPayload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubBaseURL+"/enroll", bytes.NewBuffer(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("Auto-Enrollment Verbindung zum Hub fehlgeschlagen (offline mode active)", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		slog.Info("Agent erfolgreich am Hub eingeschrieben", "node_id", nodeID)
	}
}

func startResilientEngine(ctx context.Context, buf *buffer.DiskBuffer, nodeID, tenantID, customerID, hubMetricsURL, authToken string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	client, err := createMTLSClient()
	if err != nil {
		slog.Error("mTLS Client Initialisierungsfehler", "error", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Metriken einsammeln und in den lokalen Ringpuffer schreiben
			collectAndBufferMetrics(ctx, buf, nodeID, tenantID, customerID)

			// Puffer komprimiert an den Hub übertragen (GZIP Batch Flush)
			err := buf.FlushCompress(ctx, func(ctx context.Context, compressedGzip []byte) error {
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubMetricsURL, bytes.NewBuffer(compressedGzip))
				if err != nil {
					return err
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Content-Encoding", "gzip")
				if authToken != "" {
					req.Header.Set("Authorization", "Bearer "+authToken)
				}
				req.Header.Set("X-Tenant-ID", tenantID)

				resp, err := client.Do(req)
				if err != nil {
					return err
				}
				defer resp.Body.Close()

				if resp.StatusCode >= 400 {
					return fmt.Errorf("hub rejected batch payload with status: %d", resp.StatusCode)
				}
				return nil
			})

			if err != nil {
				slog.Debug("Flush vorübergehend nicht möglich (Netzwerk offline), Daten sicher im Puffer", "error", err)
			}
		}
	}
}

func collectAndBufferMetrics(ctx context.Context, buf *buffer.DiskBuffer, nodeID, tenantID, customerID string) {
	cpuPercentages, _ := cpu.PercentWithContext(ctx, 0, false)
	cpuUsage := 0.0
	if len(cpuPercentages) > 0 {
		cpuUsage = cpuPercentages[0]
	}

	vmStat, _ := mem.VirtualMemoryWithContext(ctx)
	ramUsage := 0.0
	if vmStat != nil {
		ramUsage = vmStat.UsedPercent
	}

	diskStat, _ := disk.UsageWithContext(ctx, "/")
	diskUsage := 0.0
	if diskStat != nil {
		diskUsage = diskStat.UsedPercent
	}

	payload := map[string]interface{}{
		"node_id":        nodeID,
		"tenant_id":      tenantID,
		"customer_id":    customerID,
		"cpu_usage_pct":  cpuUsage,
		"ram_usage_pct":  ramUsage,
		"disk_usage_pct": diskUsage,
		"timestamp":      time.Now().Format(time.RFC3339),
	}

	_ = buf.Enqueue("system_metric", payload)
}

func createMTLSClient() (*http.Client, error) {
	certPath := getEnvOrDefault("AGENT_CERT_PATH", "/etc/sentinel/certs/agent.crt")
	keyPath := getEnvOrDefault("AGENT_KEY_PATH", "/etc/sentinel/certs/agent.key")
	caPath := getEnvOrDefault("CA_CERT_PATH", "/etc/sentinel/certs/ca.crt")

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}

	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return nil, err
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to append CA cert")
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      caCertPool,
				MinVersion:   tls.VersionTLS13, // Strengstes TLS für Enterprise
			},
		},
		Timeout: 15 * time.Second,
	}, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
