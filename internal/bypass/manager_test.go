package bypass

import (
	"context"
	"net"
	"path/filepath"
	"testing"
)

func TestManagerEffectiveAndMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.txt")
	store := NewStore(path)
	static, _ := parseStatic("192.168.0.0/16")

	var notify int
	m := NewManager(Options{
		Store:        store,
		StaticBypass: static,
		LookupIP: func(ctx context.Context, host string) ([]net.IP, error) {
			if host == "example.com" {
				return []net.IP{net.ParseIP("93.184.216.34")}, nil
			}
			return nil, nil
		},
		OnChange: func() { notify++ },
	})

	ctx := context.Background()
	if _, err := m.AddRule(ctx, "example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddRule(ctx, "10.0.0.0/8"); err != nil {
		t.Fatal(err)
	}

	if !m.ShouldBypassHost("example.com") {
		t.Fatal("domain match")
	}
	if !m.ShouldBypassHost("10.1.2.3") {
		t.Fatal("cidr match")
	}
	if !m.MatchIP(net.ParseIP("93.184.216.34")) {
		t.Fatal("resolved IP should match for SOCKS")
	}

	eff := m.EffectiveBypassNets()
	if !containsNet(eff, "192.168.0.0/16") || !containsNet(eff, "10.0.0.0/8") || !containsNet(eff, "93.184.216.34/32") {
		t.Fatalf("effective=%v", netsStrings(eff))
	}
	if notify < 1 {
		t.Fatal("expected OnChange")
	}
}

func parseStatic(cidrs ...string) ([]net.IPNet, error) {
	var out []net.IPNet
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, nil
}

func containsNet(nets []net.IPNet, s string) bool {
	for _, n := range nets {
		if n.String() == s {
			return true
		}
	}
	return false
}

func netsStrings(nets []net.IPNet) []string {
	out := make([]string, len(nets))
	for i, n := range nets {
		out[i] = n.String()
	}
	return out
}
