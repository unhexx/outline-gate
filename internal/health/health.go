// Package health exposes HTTP liveness and readiness endpoints.
package health

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
)

// StatusFunc returns true when the component is ready.
type StatusFunc func() bool

// Server serves /healthz and /readyz.
type Server struct {
	TunnelReady  StatusFunc
	GatewayReady StatusFunc
	// GatewayRequired if true, /readyz also requires GatewayReady.
	GatewayRequired bool
	started         atomic.Bool
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleLive)
	mux.HandleFunc("/readyz", s.handleReady)
	mux.HandleFunc("/", s.handleRoot)
	return mux
}

// MarkStarted marks the process as alive.
func (s *Server) MarkStarted() {
	s.started.Store(true)
}

func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	if !s.started.Load() {
		http.Error(w, "starting", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	tunnelOK := s.TunnelReady == nil || s.TunnelReady()
	gwOK := !s.GatewayRequired || s.GatewayReady == nil || s.GatewayReady()
	if !tunnelOK || !gwOK {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ready":   false,
			"tunnel":  tunnelOK,
			"gateway": gwOK,
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ready":   true,
		"tunnel":  true,
		"gateway": !s.GatewayRequired || gwOK,
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write([]byte("outline-gate\n"))
}
