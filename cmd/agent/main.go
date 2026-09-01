package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// Erweitert um Tenant- und Customer-Zuordnung für das White-Label-Modell
type MetricsPayload struct {
	NodeID     string  `json:"node_id"`
	TenantID   string  `json:"tenant_id"`
	CustomerID string  `json:"customer_id"`
	CPUUsage   float64 `json:"cpu_usage_pct"`
	RAMUsage   float64 `json:"ram_usage_pct"`
	Timestamp  string  `json:"timestamp"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	addr := os.Getenv("AGENT_LISTEN_ADDR")
	if addr == "" {
		addr = "10.0.0.15:9443"
	}

	nodeID := os.Getenv("NODE_ID")
	tenantID := os.Getenv("TENANT_ID")
	customerID := os.Getenv("CUSTOMER_ID")
	hubMetricsURL := os.Getenv("HUB_METRICS_URL")
	authToken := os.Getenv("ENTERPRISE_AUTH_TOKEN")

	if nodeID == "" || tenantID == "" || hubMetricsURL == "" || authToken == "" {
		slog.Error("CRITICAL: Erforderliche Umgebungsvariablen (NODE_ID, TENANT_ID, HUB_METRICS_URL, ENTERPRISE_AUTH_TOKEN) fehlen.")
		os.Exit(1)
	}

	// Hintergrund-Worker mit Mandanten-Kontext starten
	go startMetricsReporter(nodeID, tenantID, customerID, hubMetricsURL, authToken)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	slog.Info("Agent wird heruntergefahren...")
}

func startMetricsReporter(nodeID, tenantID, customerID, hubMetricsURL, authToken string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// mTLS Setup wie im Original...
	certPath := os.Getenv("AGENT_CERT_PATH")
	if certPath == "" {
		certPath = "/etc/sentinel/certs/agent.crt"
	}
	keyPath := os.Getenv("AGENT_KEY_PATH")
	if keyPath == "" {
		keyPath = "/etc/sentinel/certs/agent.key"
	}
	caPath := os.Getenv("CA_CERT_PATH")
	if caPath == "" {
		caPath = "/etc/sentinel/certs/ca.crt"
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		slog.Error("Konnte mTLS-Schlüsselpaar nicht laden", "error", err)
		return
	}
	caCert, _ := os.ReadFile(caPath)
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      caCertPool,
				MinVersion:   tls.VersionTLS13,
			},
		},
		Timeout: 5 * time.Second,
	}

	for range ticker.C {
		cpuPercentages, _ := cpu.Percent(0, false)
		var cpuUsage float64 = 0.0
		if len(cpuPercentages) > 0 {
			cpuUsage = cpuPercentages[0]
		}

		vmStat, _ := mem.VirtualMemory()
		var ramUsage float64 = 0.0
		if vmStat != nil {
			ramUsage = vmStat.UsedPercent
		}

		payload := MetricsPayload{
			NodeID:     nodeID,
			TenantID:   tenantID,
			CustomerID: customerID,
			CPUUsage:   cpuUsage,
			RAMUsage:   ramUsage,
			Timestamp:  time.Now().Format(time.RFC3339),
		}

		data, err := json.Marshal(payload)
		if err != nil {
			continue
		}

		req, err := http.NewRequest(http.MethodPost, hubMetricsURL, bytes.NewBuffer(data))
		if err != nil {
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+authToken)
		req.Header.Set("X-Tenant-ID", tenantID) // Übermittelt das Systemhaus direkt im Header

		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}
}
