package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sentinel-agent/internal/collector"
	"sentinel-agent/internal/executor"
	"sentinel-agent/internal/network"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	mux := http.NewServeMux()

	// Endpunkte mit eingebauter Zero-Trust-Netzwerkvalidierung wrapfen
	mux.Handle("POST /trigger-ansible", enforceVPN(http.HandlerFunc(executor.RunAnsiblePlaybook)))
	mux.Handle("GET /backup-status", enforceVPN(http.HandlerFunc(collector.CheckResticStatus)))

	// Bindung erfolgt strikt an die lokale WireGuard-IP (z.B. 10.0.0.15)
	server := &http.Server{
		Addr:         "10.0.0.15:9443",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("Sentinel Agent gestartet auf WireGuard Interface", "addr", server.Addr)
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

// Middleware: Erzwingt, dass Requests nur aus dem WireGuard-Netzwerk kommen
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
