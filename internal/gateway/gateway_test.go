package gateway

import (
	"net"
	"strings"
	"testing"

	"github.com/unhex/outline-gate/internal/config"
	"github.com/unhex/outline-gate/internal/routing"
)

func TestDryRunExclude(t *testing.T) {
	cfg := &config.Config{
		RoutingMode:    config.ModeExclude,
		DirectPolicy:   config.DirectAllow,
		BypassCIDRs:    []net.IPNet{{IP: net.ParseIP("10.0.0.0").To4(), Mask: net.CIDRMask(8, 32)}},
		TransproxyPort: 12345,
		LANInterface:   "eth0",
	}
	eng := routing.New(cfg, []net.IP{net.ParseIP("203.0.113.1")})
	g := New(cfg, eng, nil)
	script, err := g.DryRunScript()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "add table inet outline_gate") {
		t.Fatal("missing table")
	}
	if !strings.Contains(script, "ip daddr != @bypass") || !strings.Contains(script, "redirect to :12345") {
		t.Fatal("exclude rule missing")
	}
	if !strings.Contains(script, "203.0.113.1") {
		t.Fatal("server IP should be in bypass elements")
	}
	if !strings.Contains(script, `oifname "eth0"`) {
		t.Fatal("masquerade iface")
	}
}

func TestDryRunInclude(t *testing.T) {
	cfg := &config.Config{
		RoutingMode:    config.ModeInclude,
		DirectPolicy:   config.DirectDrop,
		BypassCIDRs:    config.DefaultBypassCIDRs(),
		TunnelCIDRs:    []net.IPNet{{IP: net.ParseIP("8.8.8.8").To4(), Mask: net.CIDRMask(32, 32)}},
		TransproxyPort: 12345,
	}
	eng := routing.New(cfg, nil)
	g := New(cfg, eng, nil)
	script, err := g.DryRunScript()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "ip daddr @tunnel") {
		t.Fatal("include rule missing")
	}
	if !strings.Contains(script, "drop") {
		t.Fatal("drop policy missing")
	}
}
