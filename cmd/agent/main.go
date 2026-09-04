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
	"strconv"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

type MetricsPayload struct {
	NodeID     string  `json:"node_id"`
	TenantID   string  `json:"tenant_id"`
	CustomerID string  `json:"customer_id"`
	CPUUsage   float64 `json:"cpu_usage_pct"`
	RAMUsage   float64 `json:"ram_usage_pct"`
	DiskUsage  float64 `json:"disk_usage_pct"`
	Timestamp  string  `json:"timestamp"`
}

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
		slog.Error("CRITICAL: Erforderliche Umgebungsvariablen (NODE_ID, TENANT_ID, CUSTOMER_ID, HUB_METRICS_URL) fehlen.")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Automatisches Self-Enrollment beim Start
	if hubBaseURL != "" && enrollToken != "" {
		custInt, err := strconv.Atoi(customerID)
		if err == nil {
			performAutoEnrollment(ctx, hubBaseURL, enrollToken, nodeID, custInt)
		} else {
			slog.Warn("CUSTOMER_ID ist kein gültiger Integer, überspringe Auto-Enrollment", "customer_id", customerID)
		}
	}

	// 2. Hintergrund-Worker mit Context-Steuerung starten
	go startMetricsReporter(ctx, nodeID, tenantID, customerID, hubMetricsURL, authToken)

	slog.Info("Sentinel Agent erfolgreich gestartet", "node_id", nodeID, "tenant_id", tenantID)
	<-ctx.Done()
	slog.Info("Agent wird geordnet heruntergefahren...")
}

func performAutoEnrollment(ctx context.Context, hubBaseURL, enrollToken, nodeID string, customerID int) {
	enrollPayload := map[string]interface{}{
		"enroll_token": enrollToken,
		"node_id":      nodeID,
		"customer_id":  customerID,
	}

	data, err := json.Marshal(enrollPayload)
	if err != nil {
		slog.Error("Fehler beim Marshalling des Enrollment-Payloads", "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubBaseURL+"/enroll", bytes.NewBuffer(data))
	if err != nil {
		slog.Error("Konnte Enrollment Request nicht erstellen", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("Auto-Enrollment Verbindung fehlgeschlagen (wird fortgesetzt)", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		slog.Info("Agent erfolgreich am Hub eingeschrieben", "node_id", nodeID)
	} else {
		slog.Error("Enrollment vom Hub abgelehnt", "status_code", resp.StatusCode)
	}
}

func startMetricsReporter(ctx context.Context, nodeID, tenantID, customerID, hubMetricsURL, authToken string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	client, err := createMTLSClient()
	if err != nil {
		slog.Error("Sicherer mTLS Client konnte nicht initialisiert werden. Metrik-Reporter wird gestoppt.", "error", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("Metrik-Reporter beendet.")
			return
		case <-ticker.C:
			reportMetrics(ctx, client, nodeID, tenantID, customerID, hubMetricsURL, authToken)
		}
	}
}

func reportMetrics(ctx context.Context, client *http.Client, nodeID, tenantID, customerID, hubMetricsURL, authToken string) {
	cpuPercentages, err := cpu.PercentWithContext(ctx, 0, false)
	var cpuUsage float64 = 0.0
	if err == nil && len(cpuPercentages) > 0 {
		cpuUsage = cpuPercentages[0]
	}

	vmStat, err := mem.VirtualMemoryWithContext(ctx)
	var ramUsage float64 = 0.0
	if err == nil && vmStat != nil {
		ramUsage = vmStat.UsedPercent
	}

	diskStat, err := disk.UsageWithContext(ctx, "/")
	var diskUsage float64 = 0.0
	if err == nil && diskStat != nil {
		diskUsage = diskStat.UsedPercent
	}

	payload := MetricsPayload{
		NodeID:     nodeID,
		TenantID:   tenantID,
		CustomerID: customerID,
		CPUUsage:   cpuUsage,
		RAMUsage:   ramUsage,
		DiskUsage:  diskUsage,
		Timestamp:  time.Now().Format(time.RFC3339),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("Fehler beim Serialisieren der Metriken", "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubMetricsURL, bytes.NewBuffer(data))
	if err != nil {
		slog.Error("Fehler beim Erstellen des Metrik-Requests", "error", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	req.Header.Set("X-Tenant-ID", tenantID)

	resp, err := client.Do(req)
	if err != nil {
		slog.Error("Fehler beim Übertragen der Metriken an den Hub", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Warn("Hub hat Metrik-Übertragung abgelehnt", "status_code", resp.StatusCode)
	}
}

func createMTLSClient() (*http.Client, error) {
	certPath := getEnvOrDefault("AGENT_CERT_PATH", "/etc/sentinel/certs/agent.crt")
	keyPath := getEnvOrDefault("AGENT_KEY_PATH", "/etc/sentinel/certs/agent.key")
	caPath := getEnvOrDefault("CA_CERT_PATH", "/etc/sentinel/certs/ca.crt")

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("mTLS KeyPair Ladefehler: %w", err)
	}

	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("CA Zertifikat Ladefehler: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("konnte CA Zertifikat nicht zum Pool hinzufügen")
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      caCertPool,
				MinVersion:   tls.VersionTLS13,
			},
		},
		Timeout: 10 * time.Second,
	}, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
