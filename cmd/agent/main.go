package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"sentinel-agent/internal/buffer"
	"sentinel-agent/internal/collector"
	"sentinel-agent/internal/config"
	"sentinel-agent/internal/diagnostics"
	"sentinel-agent/internal/executor"
	"sentinel-agent/internal/identity"
	"sentinel-agent/internal/updater"

	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

func main() {
	level := new(slog.LevelVar)
	level.Set(slog.LevelInfo)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	nodeID := os.Getenv("NODE_ID")
	tenantID := os.Getenv("TENANT_ID")
	agentID := os.Getenv("AGENT_ID")
	customerID := os.Getenv("CUSTOMER_ID")
	hubMetricsURL := os.Getenv("HUB_METRICS_URL")
	hubBaseURL := os.Getenv("HUB_BASE_URL")
	enrollToken := os.Getenv("ENROLL_TOKEN")
	authToken := os.Getenv("ENTERPRISE_AUTH_TOKEN")

	if nodeID == "" || customerID == "" || hubMetricsURL == "" {
		slog.Error("CRITICAL: Erforderliche Umgebungsvariablen für Enterprise-Betrieb fehlen.")
		os.Exit(1)
	}
	identityPath := getEnvOrDefault("AGENT_IDENTITY_PATH", "/etc/sentinel-agent/identity.json")
	loadedIdentity, identityErr := identity.LoadFromEnvironmentOrFile(identityPath)
	if identityErr == nil {
		agentID = loadedIdentity.AgentID
		tenantID = loadedIdentity.TenantID
	} else if agentID == "" {
		agentID = nodeID
		slog.Warn("Lokale Identity fehlt; verwende NODE_ID als Agent-ID", "component", "identity", "error", identityErr)
	}
	if agentID == "" {
		slog.Error("Keine Agent-ID verfügbar", "component", "identity", "error", identityErr)
		os.Exit(1)
	}
	if tenantID == "" {
		slog.Error("Keine Tenant-ID verfügbar", "component", "identity")
		os.Exit(1)
	}
	requestHeaders := identityHeaders(identity.Identity{AgentID: agentID, TenantID: tenantID})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Lokalen Disk-Buffer initialisieren (Sicherheit Nr. 1 & Skalierung Nr. 2)
	bufferPath := filepath.Join(getEnvOrDefault("AGENT_STATE_DIR", "/var/lib/sentinel"), "buffer.dat")
	encryptionKey := []byte(getEnvOrDefault("AGENT_ENCRYPTION_KEY", "sentinel-agent-fallback-key-32!!"))
	diskBuffer, err := buffer.NewDiskBuffer(bufferPath, 10000, encryptionKey)
	if err != nil {
		slog.Error("Konnte lokalen Disk-Buffer nicht initialisieren", "error", err)
		os.Exit(1)
	}
	runtimeConfig := config.NewStore(config.RuntimeConfig{
		CollectorInterval: time.Duration(getEnvInt("AGENT_REPORT_INTERVAL_SECONDS", 30)) * time.Second,
		LogLevel:          slog.LevelInfo,
	})
	if runtimeConfig.Load().CollectorInterval <= 0 {
		_ = runtimeConfig.Update(config.RuntimeConfig{CollectorInterval: 30 * time.Second, LogLevel: slog.LevelInfo})
	}

	// 2. Automatisches Self-Enrollment beim Start
	if hubBaseURL != "" && enrollToken != "" {
		if custInt, err := strconv.Atoi(customerID); err == nil {
			performAutoEnrollment(ctx, hubBaseURL, enrollToken, nodeID, agentID, custInt, requestHeaders)
		}
	}
	_, err = diagnostics.Start(ctx, getEnvOrDefault("AGENT_DEBUG_SOCKET", "/run/sentinel-agent-debug.sock"))
	if err != nil {
		slog.Warn("Lokaler Diagnose-Socket konnte nicht gestartet werden", "component", "diagnostics", "error", err)
	}

	// 3. Watchdog für Hochverfügbarkeit starten
	watchdog := executor.NewWatchdog("sentinel-agent", 30*time.Second)
	go watchdog.MonitorAndHeal(ctx)

	// 4. Resilienten Collector & Reporter im Hintergrund starten
	engineDone := make(chan struct{})
	go func() {
		defer close(engineDone)
		startResilientEngine(ctx, diskBuffer, runtimeConfig, nodeID, agentID, tenantID, customerID, requestHeaders, hubMetricsURL, authToken)
	}()
	startRuntimeConfigPoller(ctx, runtimeConfig, level, requestHeaders)
	startMemoryWatchdog(ctx, diskBuffer, int64(getEnvInt("AGENT_MEMORY_LIMIT_BYTES", 50*1024*1024)))
	startSystemdWatchdog(ctx)
	if _, err := daemon.SdNotify(false, "READY=1"); err != nil {
		slog.Warn("systemd READY-Nachricht konnte nicht gesendet werden", "component", "systemd", "error", err)
	}

	slog.Info("Sentinel Agent Enterprise Edition erfolgreich gestartet", "node_id", nodeID, "tenant_id", tenantID)
	<-ctx.Done()
	slog.Info("Agent wird geordnet heruntergefahren...", "component", "lifecycle")
	_, _ = daemon.SdNotify(false, "STOPPING=1")

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	select {
	case <-engineDone:
	case <-shutdownCtx.Done():
		slog.Warn("Telemetry-Engine wurde beim Shutdown nicht rechtzeitig beendet", "component", "lifecycle")
	}
	if err := diskBuffer.Sync(); err != nil {
		slog.Error("Disk-Buffer konnte beim Shutdown nicht synchronisiert werden", "component", "buffer", "error", err)
	}
}

func startRuntimeConfigPoller(ctx context.Context, store *config.Store, level *slog.LevelVar, headers http.Header) {
	endpoint := os.Getenv("HUB_CONFIG_URL")
	if endpoint == "" {
		return
	}
	updateHandler := func(ctx context.Context, signal config.UpdateSignal) error {
		publicKeyBytes, err := hex.DecodeString(os.Getenv("AGENT_UPDATE_PUBLIC_KEY"))
		if err != nil || len(publicKeyBytes) != ed25519.PublicKeySize {
			return fmt.Errorf("AGENT_UPDATE_PUBLIC_KEY is missing or invalid")
		}
		client, err := createHTTPClient(signal.ManifestURL)
		if err != nil {
			return err
		}
		binaryPath := getEnvOrDefault("AGENT_BINARY_PATH", "/opt/sentinel-agent/sentinel-agent")
		agentUpdater, err := updater.NewWithHeaders(client, ed25519.PublicKey(publicKeyBytes), binaryPath, headers)
		if err != nil {
			return err
		}
		if err := agentUpdater.Apply(ctx, signal.ManifestURL); err != nil {
			return err
		}
		_, _ = daemon.SdNotify(false, "STOPPING=1")
		os.Exit(0)
		return nil
	}
	poller, err := config.NewPollerWithOptions(endpoint, 5*time.Minute, store, level, headers, updateHandler, func() (*http.Client, error) {
		return createHTTPClient(endpoint)
	})
	if err != nil {
		slog.Error("Runtime-Konfigurations-Poller konnte nicht gestartet werden", "component", "config", "error", err)
		return
	}
	go poller.Run(ctx)
}

func startMemoryWatchdog(ctx context.Context, buf *buffer.DiskBuffer, threshold int64) {
	if threshold <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				var stats runtime.MemStats
				runtime.ReadMemStats(&stats)
				if int64(stats.HeapAlloc) < threshold {
					continue
				}
				if err := buf.ReleaseMemoryToDisk(); err != nil {
					slog.Error("Buffer konnte bei Speicherdruck nicht auf Disk ausgelagert werden", "component", "memory", "heap_alloc", stats.HeapAlloc, "error", err)
					continue
				}
				slog.Warn("Speicherschutz ausgelöst; Buffer auf Disk ausgelagert", "component", "memory", "heap_alloc", stats.HeapAlloc, "threshold", threshold)
				debug.FreeOSMemory()
			}
		}
	}()
}

func startSystemdWatchdog(ctx context.Context) {
	usec, err := daemon.SdWatchdogEnabled(false)
	if err != nil || usec == 0 {
		return
	}
	interval := time.Duration(usec) * time.Microsecond / 2
	if interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := daemon.SdNotify(false, "WATCHDOG=1"); err != nil {
					slog.Warn("systemd WATCHDOG-Nachricht konnte nicht gesendet werden", "component", "systemd", "error", err)
				}
			}
		}
	}()
}

func performAutoEnrollment(ctx context.Context, hubBaseURL, enrollToken, nodeID, agentID string, customerID int, headers http.Header) {
	enrollPayload := map[string]interface{}{
		"enroll_token": enrollToken,
		"node_id":      nodeID,
		"agent_id":     agentID,
		"customer_id":  customerID,
	}

	data, _ := json.Marshal(enrollPayload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(hubBaseURL, "/")+"/api/v1/agent/enroll", bytes.NewBuffer(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	copyHeaders(req.Header, headers)

	client, err := createHTTPClient(hubBaseURL)
	if err != nil {
		slog.Error("Enrollment-Client konnte nicht initialisiert werden", "error", err)
		return
	}
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

func startResilientEngine(ctx context.Context, buf *buffer.DiskBuffer, runtimeConfig *config.Store, nodeID, agentID, tenantID, customerID string, headers http.Header, hubMetricsURL, authToken string) {
	timer := time.NewTimer(runtimeConfig.Load().CollectorInterval)
	defer timer.Stop()
	breaker, err := collector.NewCircuitBreaker(5, 60*time.Second)
	if err != nil {
		slog.Error("Circuit-Breaker konnte nicht initialisiert werden", "component", "reporter", "error", err)
		return
	}

	sendBatch := func() {
		if err := buf.FlushCompress(ctx, func(ctx context.Context, compressedGzip []byte) error {
			if err := breaker.Allow(); err != nil {
				return err
			}
			client, err := createHTTPClient(hubMetricsURL)
			if err != nil {
				breaker.RecordFailure(err)
				return err
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubMetricsURL, bytes.NewBuffer(compressedGzip))
			if err != nil {
				breaker.RecordFailure(err)
				return err
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Encoding", "gzip")
			if authToken != "" {
				req.Header.Set("Authorization", "Bearer "+authToken)
			}
			req.Header.Set("X-Tenant-ID", tenantID)
			req.Header.Set("X-Agent-ID", agentID)
			copyHeaders(req.Header, headers)

			resp, err := client.Do(req)
			if err != nil {
				breaker.RecordFailure(err)
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode >= 400 {
				err := fmt.Errorf("hub rejected batch payload with status: %d", resp.StatusCode)
				if resp.StatusCode >= 500 {
					breaker.RecordFailure(err)
				}
				return err
			}
			breaker.RecordSuccess()
			return nil
		}); err != nil {
			slog.Warn("Metriken konnten nicht übertragen werden; verbleiben im Disk-Buffer", "error", err)
		}
	}

	// Direkt nach dem Start berichten, damit lokale Demos und Healthchecks nicht 30 Sekunden warten.
	collectAndBufferMetrics(ctx, buf, nodeID, agentID, tenantID, customerID)
	sendBatch()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			collectAndBufferMetrics(ctx, buf, nodeID, agentID, tenantID, customerID)
			sendBatch()
			interval := runtimeConfig.Load().CollectorInterval
			if interval <= 0 {
				interval = 30 * time.Second
			}
			timer.Reset(interval)
		}
	}
}

func identityHeaders(agentIdentity identity.Identity) http.Header {
	headers := make(http.Header)
	headers.Set("X-Tenant-ID", agentIdentity.TenantID)
	headers.Set("X-Agent-ID", agentIdentity.AgentID)
	return headers
}

func copyHeaders(destination, source http.Header) {
	for key, values := range source {
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

func collectAndBufferMetrics(ctx context.Context, buf *buffer.DiskBuffer, nodeID, agentID, tenantID, customerID string) {
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
		"agent_id":       agentID,
		"tenant_id":      tenantID,
		"customer_id":    customerID,
		"cpu_usage_pct":  cpuUsage,
		"ram_usage_pct":  ramUsage,
		"disk_usage_pct": diskUsage,
		"timestamp":      time.Now().Format(time.RFC3339),
	}

	if err := buf.Enqueue("system_metric", payload); err != nil {
		slog.Error("Metrik konnte nicht im Disk-Buffer persistiert werden", "error", err)
	}
}

func createHTTPClient(endpoint string) (*http.Client, error) {
	if strings.HasPrefix(endpoint, "http://") {
		if os.Getenv("AGENT_DEMO_MODE") != "true" {
			return nil, fmt.Errorf("unverschlüsseltes HTTP ist nur mit AGENT_DEMO_MODE=true erlaubt")
		}
		return &http.Client{Timeout: 15 * time.Second}, nil
	}
	return createMTLSClient()
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

func getEnvInt(key string, defaultValue int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return defaultValue
	}
	return value
}
