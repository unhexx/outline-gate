// Package metrics provides lightweight Prometheus text exposition without
// external dependencies.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// Registry holds process counters for outline-gate.
type Registry struct {
	TunnelOK     atomic.Int64
	TunnelFail   atomic.Int64
	DirectOK     atomic.Int64
	DirectFail   atomic.Int64
	Drop         atomic.Int64
	SOCKSAccept  atomic.Int64
	L3Accept     atomic.Int64
	DNSRefreshOK atomic.Int64
	DNSRefreshEr atomic.Int64
	started      time.Time
}

// New creates a registry with start time set.
func New() *Registry {
	return &Registry{started: time.Now().UTC()}
}

// ObserveConnection increments counters from a proxy connection event.
func (r *Registry) ObserveConnection(proto, via string, ok bool) {
	if r == nil {
		return
	}
	switch proto {
	case "socks":
		r.SOCKSAccept.Add(1)
	case "l3":
		r.L3Accept.Add(1)
	}
	switch via {
	case "direct":
		if ok {
			r.DirectOK.Add(1)
		} else {
			r.DirectFail.Add(1)
		}
	case "drop":
		r.Drop.Add(1)
	default: // tunnel
		if ok {
			r.TunnelOK.Add(1)
		} else {
			r.TunnelFail.Add(1)
		}
	}
}

// WritePrometheus writes Prometheus text format to w.
func (r *Registry) WritePrometheus(w io.Writer) {
	if r == nil {
		return
	}
	uptime := time.Since(r.started).Seconds()
	_, _ = fmt.Fprintf(w, "# HELP outline_gate_up Process up (1).\n")
	_, _ = fmt.Fprintf(w, "# TYPE outline_gate_up gauge\n")
	_, _ = fmt.Fprintf(w, "outline_gate_up 1\n")
	_, _ = fmt.Fprintf(w, "# HELP outline_gate_uptime_seconds Seconds since process start.\n")
	_, _ = fmt.Fprintf(w, "# TYPE outline_gate_uptime_seconds gauge\n")
	_, _ = fmt.Fprintf(w, "outline_gate_uptime_seconds %.0f\n", uptime)

	writeCounter(w, "outline_gate_connections_total", "Proxy connection attempts", map[string]int64{
		`via="tunnel",result="ok"`:     r.TunnelOK.Load(),
		`via="tunnel",result="fail"`:   r.TunnelFail.Load(),
		`via="direct",result="ok"`:     r.DirectOK.Load(),
		`via="direct",result="fail"`:   r.DirectFail.Load(),
		`via="drop",result="policy"`:   r.Drop.Load(),
	})
	writeCounter(w, "outline_gate_accepts_total", "Accepted client connections by proto", map[string]int64{
		`proto="socks"`: r.SOCKSAccept.Load(),
		`proto="l3"`:    r.L3Accept.Load(),
	})
	writeCounter(w, "outline_gate_dns_refresh_total", "Bypass domain DNS refresh outcomes", map[string]int64{
		`result="ok"`:    r.DNSRefreshOK.Load(),
		`result="error"`: r.DNSRefreshEr.Load(),
	})
}

func writeCounter(w io.Writer, name, help string, series map[string]int64) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	_, _ = fmt.Fprintf(w, "# TYPE %s counter\n", name)
	for labels, v := range series {
		_, _ = fmt.Fprintf(w, "%s{%s} %d\n", name, labels, v)
	}
}

// Handler returns an HTTP handler for GET /metrics.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.WritePrometheus(w)
	})
}
