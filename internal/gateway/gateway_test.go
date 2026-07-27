package gateway

import (
	"net"
	"strings"
	"testing"

	"github.com/unhexx/outline-gate/internal/config"
	"github.com/unhexx/outline-gate/internal/routing"
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
	if !strings.Contains(script, "add set inet outline_gate private") {
		t.Fatal("missing private set")
	}
	if !strings.Contains(script, "ip daddr @private return") {
		t.Fatal("private skip rule missing")
	}
	if !strings.Contains(script, "meta l4proto tcp redirect to :12345") {
		t.Fatal("blanket TCP redirect missing")
	}
	// userspace decides Direct; must not skip user/server via nft bypass set
	if strings.Contains(script, "ip daddr != @bypass") {
		t.Fatal("legacy exclude bypass skip should be gone")
	}
	if !strings.Contains(script, "10.0.0.0/8") {
		t.Fatal("default private CIDR should be in private set")
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
	// include mode also uses userspace path decision + blanket redirect
	if !strings.Contains(script, "meta l4proto tcp redirect to :12345") {
		t.Fatal("TCP redirect missing")
	}
	if !strings.Contains(script, "ip daddr @private return") {
		t.Fatal("private skip missing")
	}
	// drop is handled in transparent userspace, not nft forward
	if strings.Contains(script, " type filter hook forward") {
		t.Fatal("legacy nft forward drop should be gone")
	}
}

func TestUpdateEngineSwapsEngineWithoutApplyWhenInactive(t *testing.T) {
	cfg := &config.Config{
		RoutingMode:    config.ModeExclude,
		DirectPolicy:   config.DirectAllow,
		BypassCIDRs:    config.DefaultBypassCIDRs(),
		TransproxyPort: 12345,
	}
	eng1 := routing.New(cfg, nil)
	g := New(cfg, eng1, nil)
	if g.Active() {
		t.Fatal("new gateway should be inactive")
	}
	eng2 := routing.New(cfg, []net.IP{net.ParseIP("203.0.113.9")})
	if err := g.UpdateEngine(eng2); err != nil {
		t.Fatal(err)
	}
	// Still inactive: no Apply was performed (no nft).
	if g.Active() {
		t.Fatal("UpdateEngine without prior Apply must not mark active")
	}
	script, err := g.DryRunScript()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "add table inet outline_gate") {
		t.Fatal("script after UpdateEngine should still build")
	}
}
