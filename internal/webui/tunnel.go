package webui

import (
	"context"
	"encoding/json"
	"net/http"
)

// TunnelController switches Outline vs AmneziaWG behind SOCKS/L3.
type TunnelController interface {
	Status() TunnelStatus
	SetBackend(ctx context.Context, backend string) (TunnelStatus, error)
}

// TunnelStatus is safe JSON for the UI.
type TunnelStatus struct {
	Backend string        `json:"backend"`
	Ready   bool          `json:"ready"`
	Outline OutlineStatus `json:"outline"`
	Amnezia AmneziaStatus `json:"amnezia"`
	Message string        `json:"message,omitempty"`
}

// AmneziaStatus is the AmneziaWG side of the switcher.
type AmneziaStatus struct {
	Configured bool   `json:"configured"`
	Ready      bool   `json:"ready"`
	Endpoint   string `json:"endpoint,omitempty"`
	Iface      string `json:"iface,omitempty"`
	Label      string `json:"label,omitempty"`
}

func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request) {
	if s.Tunnel == nil {
		writeErr(w, http.StatusNotFound, "tunnel API disabled")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.Tunnel.Status())
	case http.MethodPut, http.MethodPost:
		var body struct {
			Backend string `json:"backend"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		st, err := s.Tunnel.SetBackend(r.Context(), body.Backend)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": st})
	default:
		w.Header().Set("Allow", "GET, PUT, POST")
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
