package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sentinel-core/internal/collector"
	"sentinel-core/internal/executor"
	"sentinel-core/internal/network"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

type MetricsPayload struct {
	NodeID    string  `json:"node_id"`
	CPUUsage  float64 `json:"cpu_usage_pct"`
	RAMUsage  float64 `json:"ram_usage_pct"`
	Timestamp string  `json:"timestamp"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	addr := os.Getenv("AGENT_LISTEN_ADDR")
	if addr == "" {
		addr = "10.0.0.15:9443"
	}

	nodeID := os.Getenv("NODE_ID")
	hubMetricsURL := os.Getenv("HUB_METRICS_URL")
	authToken := os.Getenv("ENTERPRISE_AUTH_TOKEN")

	// Fail-Fast: Zero-Trust Prinzip (Agent bricht ab, wenn Auth-Daten fehlen)
	if nodeID == "" || hubMetricsURL == "" || authToken == "" {
		slog.Error("CRITICAL: NODE_ID, HUB_METRICS_URL oder ENTERPRISE_AUTH_TOKEN fehlen. Beende Agent.")
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.Handle("POST /trigger-ansible", enforceVPN(http.HandlerFunc(executor.RunAnsiblePlaybook)))
	mux.Handle("GET /backup-status", enforceVPN(http.HandlerFunc(collector.CheckResticStatus)))

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	// Hintergrund-Worker mit mTLS und Token starten
	go startMetricsReporter(nodeID, hubMetricsURL, authToken)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("Sentinel Agent gestartet", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Agent Server abgestürzt", "error", err)
			os.Exit(1)
		}
	}()

	<-stop
	slog.Info("Fahre Agent sicher herunter...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}

// Authentifizierter Metrics Reporter mit mTLS, Exponential-Backoff und Fail-Safe
func startMetricsReporter(nodeID, hubMetricsURL, authToken string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// --- mTLS Konfiguration (Enterprise Security) ---
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
		slog.Error("CRITICAL: Konnte mTLS-Schlüsselpaar nicht laden", "path_cert", certPath, "error", err)
		return
	}

	caCert, err := os.ReadFile(caPath)
	if err != nil {
		slog.Error("CRITICAL: Konnte CA-Zertifikat nicht lesen", "path_ca", caPath, "error", err)
		return
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		MinVersion:   tls.VersionTLS13, // Erzwingt modernes, sicheres TLS 1.3
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
		Timeout: 5 * time.Second,
	}
	// -----------------------------------------------

	for range ticker.C {
		cpuPercentages, err := cpu.Percent(0, false)
		var cpuUsage float64 = 0.0
		if err == nil && len(cpuPercentages) > 0 {
			cpuUsage = cpuPercentages[0]
		}

		vmStat, err := mem.VirtualMemory()
		var ramUsage float64 = 0.0
		if err == nil {
			ramUsage = vmStat.UsedPercent
		}

		payload := MetricsPayload{
			NodeID:    nodeID,
			CPUUsage:  cpuUsage,
			RAMUsage:  ramUsage,
			Timestamp: time.Now().Format(time.RFC3339),
		}

		data, err := json.Marshal(payload)
		if err != nil {
			slog.Error("Fehler beim JSON-Marshalling der Metriken", "error", err)
			continue
		}

		backoff := 2 * time.Second
		maxRetries := 3
		var success bool

		for i := 1; i <= maxRetries; i++ {
			req, err := http.NewRequest(http.MethodPost, hubMetricsURL, bytes.NewBuffer(data))
			if err != nil {
				slog.Error("Fehler beim Erstellen des Requests", "error", err)
				break
			}

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+authToken)

			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
					success = true
					break
				}
			}
			slog.Warn("Metrik-Übertragung fehlgeschlagen, neuer Versuch...", "attempt", i, "error", err)
			time.Sleep(backoff)
			backoff *= 2
		}

		if !success {
			slog.Error("Metrik-Übertragung nach max. Retries endgültig fehlgeschlagen", "node_id", nodeID)
		}
	}
}

func enforceVPN(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := network.ValidateVPNConnection(r.RemoteAddr); err != nil {
			slog.Warn("Unautorisierter Zugriffserfassungsversuch blockiert", "remote", r.RemoteAddr)
			http.Error(w, "Forbidden: Invalid network path", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
