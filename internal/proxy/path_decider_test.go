package proxy

import (
	"net"
	"sync"
	"testing"

	"github.com/unhexx/outline-gate/internal/config"
	"github.com/unhexx/outline-gate/internal/routing"
)

func TestEnginePathDeciderDirectAndDrop(t *testing.T) {
	cfg := &config.Config{
		RoutingMode:  config.ModeInclude,
		DirectPolicy: config.DirectDrop,
		TunnelCIDRs: []net.IPNet{
			{IP: net.ParseIP("8.8.8.8").To4(), Mask: net.CIDRMask(32, 32)},
		},
		BypassCIDRs: config.DefaultBypassCIDRs(),
	}
	eng := routing.New(cfg, []net.IP{net.ParseIP("203.0.113.1")})
	var mu sync.Mutex
	d := &EnginePathDecider{
		Mu:     &mu,
		Engine: func() *routing.Engine { return eng },
	}

	via, rule := d.DecidePath(net.ParseIP("203.0.113.1"))
	if via != PathDirect || rule != "outline-server" {
		t.Fatalf("server: via=%s rule=%s", via, rule)
	}
	via, rule = d.DecidePath(net.ParseIP("8.8.8.8"))
	if via != PathTunnel {
		t.Fatalf("tunnel: via=%s rule=%s", via, rule)
	}
	via, rule = d.DecidePath(net.ParseIP("1.1.1.1"))
	if via != PathDrop || rule != "policy:drop" {
		t.Fatalf("drop: via=%s rule=%s", via, rule)
	}
}
