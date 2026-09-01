package metrics

import (
	"bytes"
	"strings"
	"testing"
)

func TestWritePrometheus(t *testing.T) {
	r := New()
	r.ObserveConnection("socks", "tunnel", true)
	r.ObserveConnection("l3", "direct", false)
	r.ObserveConnection("l3", "drop", false)
	r.ObserveProbe(true)
	r.ObserveProbe(false)
	r.ObserveRefresh(true, true)
	r.ObserveRefresh(false, false)
	var buf bytes.Buffer
	r.WritePrometheus(&buf)
	out := buf.String()
	for _, want := range []string{
		"outline_gate_up 1",
		`outline_gate_connections_total{via="tunnel",result="ok"} 1`,
		`outline_gate_connections_total{via="direct",result="fail"} 1`,
		`outline_gate_accepts_total{proto="socks"} 1`,
		`outline_gate_tunnel_probe_total{result="ok"} 1`,
		`outline_gate_tunnel_probe_total{result="fail"} 1`,
		`outline_gate_ssconf_refresh_total{result="changed"} 1`,
		`outline_gate_ssconf_refresh_total{result="fail"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
