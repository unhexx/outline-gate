package bypass

import (
	"net"
	"strings"
)

// Matcher checks whether a destination should bypass the tunnel.
type Matcher struct {
	rules []Rule
}

// NewMatcher builds a matcher from rules (copied).
func NewMatcher(rules []Rule) *Matcher {
	cp := make([]Rule, len(rules))
	copy(cp, rules)
	return &Matcher{rules: cp}
}

// MatchHost reports whether host (domain or IP literal) matches a bypass rule.
func (m *Matcher) MatchHost(host string) bool {
	ok, _ := m.MatchHostDetail(host)
	return ok
}

// MatchHostDetail is like MatchHost but also returns the matched rule Raw (if any).
func (m *Matcher) MatchHostDetail(host string) (bool, string) {
	if m == nil {
		return false, ""
	}
	host = NormalizeHost(host)
	if host == "" {
		return false, ""
	}
	if ip := net.ParseIP(host); ip != nil {
		return m.MatchIPDetail(ip)
	}
	for _, r := range m.rules {
		switch r.Kind {
		case KindDomain:
			if host == r.Host {
				return true, r.Raw
			}
		case KindSuffix:
			if host == r.Host || strings.HasSuffix(host, "."+r.Host) {
				return true, r.Raw
			}
		}
	}
	return false, ""
}

// MatchIP reports whether ip is covered by an IP/CIDR rule.
func (m *Matcher) MatchIP(ip net.IP) bool {
	ok, _ := m.MatchIPDetail(ip)
	return ok
}

// MatchIPDetail is like MatchIP but also returns the matched rule Raw (if any).
func (m *Matcher) MatchIPDetail(ip net.IP) (bool, string) {
	if m == nil || ip == nil {
		return false, ""
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, r := range m.rules {
		if r.Net != nil && r.Net.Contains(ip) {
			return true, r.Raw
		}
	}
	return false, ""
}

// Rules returns a copy of the rules.
func (m *Matcher) Rules() []Rule {
	if m == nil {
		return nil
	}
	out := make([]Rule, len(m.rules))
	copy(out, m.rules)
	return out
}

// StaticNets returns IP/CIDR nets from rules (no DNS).
func StaticNets(rules []Rule) []net.IPNet {
	var out []net.IPNet
	for _, r := range rules {
		if r.Net != nil {
			out = append(out, *r.Net)
		}
	}
	return out
}

// DomainsToResolve returns hostnames that should be DNS-resolved for L3 bypass.
// For suffix rules, the apex host is returned.
func DomainsToResolve(rules []Rule) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, r := range rules {
		var h string
		switch r.Kind {
		case KindDomain, KindSuffix:
			h = r.Host
		default:
			continue
		}
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}
