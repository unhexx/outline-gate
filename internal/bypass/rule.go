// Package bypass implements user-managed VPN exclusion rules (IP, CIDR, domains).
package bypass

import (
	"fmt"
	"net"
	"strings"
	"unicode"
)

// Kind classifies a bypass rule.
type Kind string

const (
	KindIP     Kind = "ip"
	KindCIDR   Kind = "cidr"
	KindDomain Kind = "domain"
	KindSuffix Kind = "suffix"
)

// Rule is a single bypass entry.
type Rule struct {
	// Raw is the canonical string form (lowercased hostnames).
	Raw  string `json:"rule"`
	Kind Kind   `json:"kind"`
	// Net is set for KindIP and KindCIDR.
	Net *net.IPNet `json:"-"`
	// Host is set for KindDomain (exact) and KindSuffix (apex without "*.").
	Host string `json:"-"`
}

// ParseRule parses a single rule line (no comments).
func ParseRule(s string) (Rule, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Rule{}, fmt.Errorf("empty rule")
	}
	if strings.ContainsAny(s, " \t") {
		return Rule{}, fmt.Errorf("rule must not contain whitespace")
	}

	// CIDR
	if strings.Contains(s, "/") {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return Rule{}, fmt.Errorf("invalid CIDR: %w", err)
		}
		return Rule{Raw: n.String(), Kind: KindCIDR, Net: n}, nil
	}

	// IP
	if ip := net.ParseIP(s); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			n := &net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}
			return Rule{Raw: v4.String(), Kind: KindIP, Net: n}, nil
		}
		n := &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
		return Rule{Raw: ip.String(), Kind: KindIP, Net: n}, nil
	}

	// Suffix *.example.com
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "*.") {
		apex := strings.TrimPrefix(lower, "*.")
		apex = strings.TrimSuffix(apex, ".")
		if err := validateHostname(apex); err != nil {
			return Rule{}, fmt.Errorf("invalid suffix domain: %w", err)
		}
		return Rule{Raw: "*." + apex, Kind: KindSuffix, Host: apex}, nil
	}

	// Exact domain
	host := strings.ToLower(strings.TrimSuffix(lower, "."))
	if err := validateHostname(host); err != nil {
		return Rule{}, fmt.Errorf("invalid domain: %w", err)
	}
	return Rule{Raw: host, Kind: KindDomain, Host: host}, nil
}

func validateHostname(host string) error {
	if host == "" {
		return fmt.Errorf("empty hostname")
	}
	if len(host) > 253 {
		return fmt.Errorf("hostname too long")
	}
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return fmt.Errorf("invalid hostname form")
	}
	labels := strings.Split(host, ".")
	if len(labels) < 1 {
		return fmt.Errorf("invalid hostname")
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return fmt.Errorf("invalid label %q", label)
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("label must not start/end with hyphen: %q", label)
		}
		for _, r := range label {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
				continue
			}
			return fmt.Errorf("invalid character in hostname label %q", label)
		}
	}
	return nil
}

// NormalizeHost lowercases and trims trailing dots for matching.
func NormalizeHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}
