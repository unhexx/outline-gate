package routing

import (
	"net"
	"testing"

	"github.com/unhex/outline-gate/internal/config"
)

func mustCIDR(s string) net.IPNet {
	n, err := config.ParseCIDROrIP(s)
	if err != nil {
		panic(err)
	}
	return n
}

func TestExcludeMode(t *testing.T) {
	cfg := &config.Config{
		RoutingMode:  config.ModeExclude,
		DirectPolicy: config.DirectAllow,
		BypassCIDRs:  []net.IPNet{mustCIDR("10.0.0.0/8"), mustCIDR("192.168.0.0/16")},
	}
	e := New(cfg, []net.IP{net.ParseIP("203.0.113.50")})

	cases := []struct {
		ip   string
		want Path
	}{
		{"8.8.8.8", PathTunnel},
		{"10.0.0.1", PathDirect},
		{"192.168.1.1", PathDirect},
		{"203.0.113.50", PathDirect}, // outline server
		{"1.1.1.1", PathTunnel},
	}
	for _, tc := range cases {
		got := e.Decide(net.ParseIP(tc.ip))
		if got != tc.want {
			t.Errorf("%s: got %s want %s", tc.ip, got, tc.want)
		}
	}
}

func TestIncludeModeDirect(t *testing.T) {
	cfg := &config.Config{
		RoutingMode:  config.ModeInclude,
		DirectPolicy: config.DirectAllow,
		BypassCIDRs:  config.DefaultBypassCIDRs(),
		TunnelCIDRs:  []net.IPNet{mustCIDR("8.8.8.8/32"), mustCIDR("1.1.1.0/24")},
	}
	e := New(cfg, []net.IP{net.ParseIP("9.9.9.9")})

	cases := []struct {
		ip   string
		want Path
	}{
		{"8.8.8.8", PathTunnel},
		{"1.1.1.5", PathTunnel},
		{"8.8.4.4", PathDirect},
		{"10.1.1.1", PathDirect},
		{"9.9.9.9", PathDirect},
	}
	for _, tc := range cases {
		got := e.Decide(net.ParseIP(tc.ip))
		if got != tc.want {
			t.Errorf("%s: got %s want %s", tc.ip, got, tc.want)
		}
	}
}

func TestIncludeModeDrop(t *testing.T) {
	cfg := &config.Config{
		RoutingMode:  config.ModeInclude,
		DirectPolicy: config.DirectDrop,
		BypassCIDRs:  config.DefaultBypassCIDRs(),
		TunnelCIDRs:  []net.IPNet{mustCIDR("8.8.8.8/32")},
	}
	e := New(cfg, nil)
	if e.Decide(net.ParseIP("8.8.4.4")) != PathDrop {
		t.Fatal("expected drop")
	}
	if e.Decide(net.ParseIP("8.8.8.8")) != PathTunnel {
		t.Fatal("expected tunnel")
	}
}

func TestNilIP(t *testing.T) {
	e := New(&config.Config{RoutingMode: config.ModeExclude}, nil)
	if e.Decide(nil) != PathDrop {
		t.Fatal("nil ip")
	}
}
