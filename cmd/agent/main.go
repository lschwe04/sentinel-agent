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
	"strconv"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// Erweitert um Tenant-, Customer- und Festplatten-Zuordnung für das Enterprise-Modell
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

	addr := os.Getenv("AGENT_LISTEN_ADDR")
	if addr == "" {
		addr = "10.0.0.15:9443"
	}

	nodeID := os.Getenv("NODE_ID")
	tenantID := os.Getenv("TENANT_ID")
	customerID := os.Getenv("CUSTOMER_ID")
	hubMetricsURL := os.Getenv("HUB_METRICS_URL")
	hubBaseURL := os.Getenv("HUB_BASE_URL") // Z.B. https://hub.systemhaus.de/api/v1
	enrollToken := os.Getenv("ENROLL_TOKEN")
	authToken := os.Getenv("ENTERPRISE_AUTH_TOKEN")

	if nodeID == "" || tenantID == "" || customerID == "" || hubMetricsURL == "" || authToken == "" {
		slog.Error("CRITICAL: Erforderliche Umgebungsvariablen (NODE_ID, TENANT_ID, CUSTOMER_ID, HUB_METRICS_URL, ENTERPRISE_AUTH_TOKEN) fehlen.")
		os.Exit(1)
	}

	// 1. Automatisches Self-Enrollment beim Start ausführen, falls Hub-Base und Token da sind
	if hubBaseURL != "" && enrollToken != "" {
		custInt, err := strconv.Atoi(customerID)
		if err == nil {
			performAutoEnrollment(hubBaseURL, enrollToken, nodeID, custInt)
		} else {
			slog.Warn("CUSTOMER_ID konnte nicht nach Integer geparst werden, überspringe Auto-Enrollment", "customer_id", customerID)
		}
	}

	// 2. Hintergrund-Worker mit Mandanten-Kontext starten
	go startMetricsReporter(nodeID, tenantID, customerID, hubMetricsURL, authToken)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	slog.Info("Agent wird heruntergefahren...")
}

// Führt die automatische Registrierung (Enrollment) beim Hub durch
func performAutoEnrollment(hubBaseURL, enrollToken, nodeID string, customerID int) {
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

	enrollURL := hubBaseURL + "/enroll"
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Post(enrollURL, "application/json", bytes.NewBuffer(data))
	if err != nil {
		slog.Warn("Auto-Enrollment Verbindung fehlgeschlagen (wird fortgesetzt)", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		slog.Info("Agent erfolgreich am Hub eingeschrieben (Enrolled)", "node_id", nodeID)
	} else {
		slog.Error("Enrollment vom Hub abgelehnt", "status_code", resp.StatusCode)
	}
}

func startMetricsReporter(nodeID, tenantID, customerID, hubMetricsURL, authToken string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// mTLS Setup
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
		// CPU Auslastung
		cpuPercentages, _ := cpu.Percent(0, false)
		var cpuUsage float64 = 0.0
		if len(cpuPercentages) > 0 {
			cpuUsage = cpuPercentages[0]
		}

		// RAM Auslastung
		vmStat, _ := mem.VirtualMemory()
		var ramUsage float64 = 0.0
		if vmStat != nil {
			ramUsage = vmStat.UsedPercent
		}

		// Festplatten-Auslastung (Root-Partition)
		diskStat, err := disk.Usage("/")
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
			continue
		}

		req, err := http.NewRequest(http.MethodPost, hubMetricsURL, bytes.NewBuffer(data))
		if err != nil {
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+authToken)
		req.Header.Set("X-Tenant-ID", tenantID)

		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}
}
