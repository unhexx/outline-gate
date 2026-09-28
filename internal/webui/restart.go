package webui

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	scope := strings.ToLower(strings.TrimSpace(body.Scope))
	switch scope {
	case "services", "":
		if s.RestartServices == nil {
			writeErr(w, http.StatusNotImplemented, "restart is not available")
			return
		}
		msg, err := s.RestartServices(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		if msg == "" {
			msg = "службы перезапущены"
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "scope": "services", "message": msg})
	case "container":
		if s.RestartContainer == nil {
			writeErr(w, http.StatusNotImplemented, "container restart is not available")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"scope":   "container",
			"message": "контейнер перезапускается",
		})
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		go func() {
			time.Sleep(300 * time.Millisecond)
			s.RestartContainer()
		}()
	default:
		writeErr(w, http.StatusBadRequest, "scope must be services or container")
	}
}
