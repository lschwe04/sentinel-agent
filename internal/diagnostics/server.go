package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"time"
)

type Server struct {
	server *http.Server
	path   string
}

func Start(ctx context.Context, path string) (*Server, error) {
	return StartWithHandlers(ctx, path, nil)
}

func StartWithHandlers(ctx context.Context, path string, handlers map[string]http.HandlerFunc) (*Server, error) {
	if path == "" {
		return nil, fmt.Errorf("diagnostic socket path is empty")
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale diagnostic socket: %w", err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on diagnostic socket: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("restrict diagnostic socket permissions: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.Handle("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
	mux.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
	mux.Handle("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
	mux.Handle("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))
	for route, handler := range handlers {
		if route != "" && handler != nil {
			mux.HandleFunc(route, handler)
		}
	}
	server := &Server{server: &http.Server{Handler: mux}, path: path}
	go func() {
		if err := server.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// The diagnostic endpoint is best-effort; the agent itself must remain alive.
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.server.Shutdown(shutdownCtx)
		_ = os.Remove(path)
	}()
	return server, nil
}
