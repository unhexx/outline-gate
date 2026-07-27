package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/unhex/outline-gate/internal/connlog"
)

// ConnLog is the read side of the connection event store for the UI.
type ConnLog interface {
	Snapshot(limit int) []connlog.Event
	Subscribe() (<-chan connlog.Event, func())
	Capacity() int
	Len() int
	Stats(since time.Time) (total, vpn, direct, ok, fail int)
}

func (s *Server) routeConnectionsAPI(w http.ResponseWriter, r *http.Request) {
	if s.ConnLog == nil {
		writeErr(w, http.StatusNotFound, "connections API disabled")
		return
	}
	path := r.URL.Path
	switch {
	case path == "/api/v1/connections" || path == "/api/v1/connections/":
		s.handleConnections(w, r)
	case path == "/api/v1/connections/stream" || path == "/api/v1/connections/stream/":
		s.handleConnectionsStream(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := 0
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	events := s.ConnLog.Snapshot(limit)
	if events == nil {
		events = []connlog.Event{}
	}
	since := time.Now().Add(-time.Minute)
	total, vpn, direct, okN, fail := s.ConnLog.Stats(since)
	writeJSON(w, http.StatusOK, map[string]any{
		"events":   events,
		"capacity": s.ConnLog.Capacity(),
		"count":    s.ConnLog.Len(),
		"stats_1m": map[string]int{
			"total":  total,
			"vpn":    vpn,
			"direct": direct,
			"ok":     okN,
			"fail":   fail,
		},
	})
}

func (s *Server) handleConnectionsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Initial snapshot
	events := s.ConnLog.Snapshot(0)
	if events == nil {
		events = []connlog.Event{}
	}
	if err := writeSSE(w, "snapshot", events); err != nil {
		return
	}
	flusher.Flush()

	ch, unsub := s.ConnLog.Subscribe()
	defer unsub()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			if err := writeSSE(w, "ping", map[string]string{"t": time.Now().UTC().Format(time.RFC3339)}); err != nil {
				return
			}
			flusher.Flush()
		case e, ok := <-ch:
			if !ok {
				return
			}
			if err := writeSSE(w, "conn", e); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	return err
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	out := map[string]any{}
	if s.Outline != nil {
		out["outline"] = s.Outline.Status()
	}
	if s.Status != nil {
		st := s.Status()
		out["socks"] = st.SOCKSListen
		out["gateway"] = st.GatewayEnable
		out["health"] = st.HealthListen
	}
	if s.ConnLog != nil {
		since := time.Now().Add(-time.Minute)
		total, vpn, direct, okN, fail := s.ConnLog.Stats(since)
		out["connlog"] = map[string]any{
			"capacity": s.ConnLog.Capacity(),
			"count":    s.ConnLog.Len(),
			"vpn_1m":   vpn,
			"direct_1m": direct,
			"total_1m": total,
			"ok_1m":    okN,
			"fail_1m":  fail,
		}
	}
	writeJSON(w, http.StatusOK, out)
}
