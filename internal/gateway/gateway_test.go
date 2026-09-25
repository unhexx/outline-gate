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
	if !strings.Contains(script, "ip protocol tcp redirect to :12345") {
		t.Fatal("IPv4 TCP redirect missing")
	}
	if strings.Contains(script, "meta l4proto tcp redirect") {
		t.Fatal("inet l4proto redirect would also match IPv6")
	}
	// prerouting still sends public bypass to userspace (live log)
	if strings.Contains(script, "ip daddr != @bypass") {
		t.Fatal("legacy exclude bypass skip should be gone")
	}
	if !strings.Contains(script, "10.0.0.0/8") {
		t.Fatal("default private CIDR should be in private set")
	}
	if !strings.Contains(script, `oifname "eth0"`) {
		t.Fatal("masquerade iface")
	}
	if !strings.Contains(script, `ip saddr 127.0.0.0/8 return`) {
		t.Fatal("loopback SNAT skip missing")
	}
	if !strings.Contains(script, `oifname "lo" return`) {
		t.Fatal("oif lo SNAT skip missing")
	}
}

func TestMasqueradeSkipsLoopbackWhenUnscoped(t *testing.T) {
	cfg := &config.Config{
		RoutingMode:    config.ModeExclude,
		DirectPolicy:   config.DirectAllow,
		BypassCIDRs:    config.DefaultBypassCIDRs(),
		TransproxyPort: 12345,
	}
	eng := routing.New(cfg, nil)
	g := New(cfg, eng, nil)
	script, err := g.DryRunScript()
	if err != nil {
		t.Fatal(err)
	}
	ret := strings.Index(script, `ip saddr 127.0.0.0/8 return`)
	lo := strings.Index(script, `oifname "lo" return`)
	masq := strings.Index(script, "masquerade")
	if ret < 0 || lo < 0 {
		t.Fatal("unscoped masquerade must skip 127.0.0.0/8 and oif lo")
	}
	if masq < 0 || ret > masq || lo > masq {
		t.Fatal("loopback return rules must precede masquerade")
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
	if !strings.Contains(script, "ip protocol tcp redirect to :12345") {
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

func TestOutputExcludeRedirectsAfterBypassSkip(t *testing.T) {
	cfg := &config.Config{
		RoutingMode:         config.ModeExclude,
		DirectPolicy:        config.DirectAllow,
		BypassCIDRs:         config.DefaultBypassCIDRs(),
		TransproxyPort:      12345,
		GatewayOutputEnable: true,
	}
	eng := routing.New(cfg, []net.IP{net.ParseIP("203.0.113.1")})
	g := New(cfg, eng, nil)
	script, err := g.DryRunScript()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "add set inet outline_gate bypass") {
		t.Fatal("bypass set missing")
	}
	if !strings.Contains(script, "203.0.113.1/32") {
		t.Fatal("outline server must be in bypass set")
	}
	priv := strings.Index(script, "add rule inet outline_gate output ip daddr @private return")
	byp := strings.Index(script, "add rule inet outline_gate output ip daddr @bypass return")
	redir := strings.Index(script, "add rule inet outline_gate output ip protocol tcp redirect to :12345")
	if priv < 0 || byp < 0 || redir < 0 {
		t.Fatal("OUTPUT exclude rules missing")
	}
	if !(priv < byp && byp < redir) {
		t.Fatal("OUTPUT skip rules must precede redirect")
	}
	// prerouting must still blanket-redirect (userspace logs Direct)
	pre := strings.Index(script, "add rule inet outline_gate prerouting ip protocol tcp redirect to :12345")
	if pre < 0 {
		t.Fatal("prerouting IPv4 redirect missing")
	}
}

func TestOutputDisabledLeavesOutputChainEmpty(t *testing.T) {
	cfg := &config.Config{
		RoutingMode:         config.ModeExclude,
		DirectPolicy:        config.DirectAllow,
		BypassCIDRs:         config.DefaultBypassCIDRs(),
		TransproxyPort:      12345,
		GatewayOutputEnable: false,
	}
	g := New(cfg, routing.New(cfg, nil), nil)
	script, err := g.DryRunScript()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(script, "add rule inet outline_gate output ip protocol tcp redirect") {
		t.Fatal("OUTPUT redirect must not be installed when GATEWAY_OUTPUT_ENABLE=false")
	}
}

func TestOutputIncludeRedirectsOnlyTunnelSet(t *testing.T) {
	cfg := &config.Config{
		RoutingMode:         config.ModeInclude,
		DirectPolicy:        config.DirectAllow,
		BypassCIDRs:         config.DefaultBypassCIDRs(),
		TunnelCIDRs:         []net.IPNet{{IP: net.ParseIP("8.8.8.8").To4(), Mask: net.CIDRMask(32, 32)}},
		TransproxyPort:      12345,
		GatewayOutputEnable: true,
	}
	g := New(cfg, routing.New(cfg, nil), nil)
	script, err := g.DryRunScript()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "add rule inet outline_gate output ip daddr @tunnel ip protocol tcp redirect to :12345") {
		t.Fatal("include OUTPUT must redirect only @tunnel")
	}
	if strings.Contains(script, "add rule inet outline_gate output ip protocol tcp redirect to :12345") {
		t.Fatal("include OUTPUT must not blanket-redirect (Direct residual would loop)")
	}
	if !strings.Contains(script, "8.8.8.8/32") {
		t.Fatal("tunnel CIDR missing from set")
	}
}
