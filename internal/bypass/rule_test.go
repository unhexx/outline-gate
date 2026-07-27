package bypass

import (
	"net"
	"testing"
)

func TestParseRule(t *testing.T) {
	tests := []struct {
		in   string
		kind Kind
		raw  string
	}{
		{"8.8.8.8", KindIP, "8.8.8.8"},
		{"203.0.113.0/24", KindCIDR, "203.0.113.0/24"},
		{"Example.COM", KindDomain, "example.com"},
		{"*.CDN.Example.NET", KindSuffix, "*.cdn.example.net"},
		{"example.com.", KindDomain, "example.com"},
	}
	for _, tc := range tests {
		r, err := ParseRule(tc.in)
		if err != nil {
			t.Fatalf("ParseRule(%q): %v", tc.in, err)
		}
		if r.Kind != tc.kind || r.Raw != tc.raw {
			t.Fatalf("ParseRule(%q) = kind=%s raw=%s, want %s %s", tc.in, r.Kind, r.Raw, tc.kind, tc.raw)
		}
	}
}

func TestParseRuleErrors(t *testing.T) {
	bad := []string{"", "   ", "not a host", "*.", "*..com", "http://x", "10.0.0.0/99"}
	for _, s := range bad {
		if _, err := ParseRule(s); err == nil {
			t.Fatalf("expected error for %q", s)
		}
	}
}

func TestMatcherDomainAndSuffix(t *testing.T) {
	rules := mustRules(t, "example.com", "*.cdn.example.net", "10.0.0.0/8", "1.2.3.4")
	m := NewMatcher(rules)

	if !m.MatchHost("example.com") {
		t.Fatal("exact domain")
	}
	if m.MatchHost("sub.example.com") {
		t.Fatal("exact must not match subdomain")
	}
	if !m.MatchHost("cdn.example.net") {
		t.Fatal("suffix apex")
	}
	if !m.MatchHost("a.b.cdn.example.net") {
		t.Fatal("suffix nested")
	}
	if m.MatchHost("notcdn.example.net") {
		t.Fatal("false suffix")
	}
	if !m.MatchIP(net.ParseIP("10.1.2.3")) {
		t.Fatal("cidr")
	}
	if !m.MatchIP(net.ParseIP("1.2.3.4")) {
		t.Fatal("ip")
	}
	if m.MatchIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("non-match ip")
	}
}

func TestDomainsToResolve(t *testing.T) {
	rules := mustRules(t, "example.com", "*.cdn.example.net", "1.2.3.4")
	hosts := DomainsToResolve(rules)
	if len(hosts) != 2 {
		t.Fatalf("got %v", hosts)
	}
}

func mustRules(t *testing.T, raws ...string) []Rule {
	t.Helper()
	var out []Rule
	for _, r := range raws {
		rule, err := ParseRule(r)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, rule)
	}
	return out
}
