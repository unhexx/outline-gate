// Package config loads and validates outline-gate configuration from
// environment variables and optional list files.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RoutingMode selects split-tunnel policy.
type RoutingMode string

const (
	ModeExclude RoutingMode = "exclude"
	ModeInclude RoutingMode = "include"
)

// DirectPolicy is applied to non-matching destinations in include mode.
type DirectPolicy string

const (
	DirectAllow DirectPolicy = "direct"
	DirectDrop  DirectPolicy = "drop"
)

// DNSMode controls DNS handling (v1: mostly informational / future hooks).
type DNSMode string

const (
	DNSSystem DNSMode = "system"
	DNSTunnel DNSMode = "tunnel"
	DNSStatic DNSMode = "static"
)

// Config is the full runtime configuration for outline-gate.
type Config struct {
	AccessKey     string
	RoutingMode   RoutingMode
	BypassCIDRs   []net.IPNet
	TunnelCIDRs   []net.IPNet
	DirectPolicy  DirectPolicy
	LANInterface  string
	GatewayEnable bool
	SOCKSListen   string
	HealthListen  string
	LogLevel      string
	LogFormat     string
	ReconnectBase time.Duration
	ReconnectMax  time.Duration
	DNSMode       DNSMode
	DNSServers    []string
	// TransproxyListen is the local address for TCP REDIRECT/TPROXY.
	TransproxyListen string
	// TransproxyPort is the port component of TransproxyListen (for nftables).
	TransproxyPort int

	// UIEnable turns on the embedded bypass management UI and API.
	UIEnable bool
	// UIToken is required when UIEnable is true (Bearer or Basic password).
	UIToken string
	// BypassRulesFile is the path to user-managed bypass rules (IP/CIDR/domain).
	BypassRulesFile string
	// BypassDNSRefresh is how often domain rules are re-resolved for L3.
	BypassDNSRefresh time.Duration
	// AccessKeyPersistFile is where the UI writes a replaced Outline key (survives restart).
	// Loaded with highest priority when non-empty.
	AccessKeyPersistFile string
}

// Load reads configuration from the process environment.
func Load() (*Config, error) {
	return LoadFromEnv(os.Getenv)
}

// LoadFromEnv is like Load but uses getenv for testability.
func LoadFromEnv(getenv func(string) string) (*Config, error) {
	cfg := &Config{
		RoutingMode:      ModeExclude,
		DirectPolicy:     DirectAllow,
		LANInterface:     "",
		GatewayEnable:    false,
		SOCKSListen:      "0.0.0.0:1080",
		HealthListen:     "0.0.0.0:8080",
		LogLevel:         "info",
		LogFormat:        "text",
		ReconnectBase:    time.Second,
		ReconnectMax:     60 * time.Second,
		DNSMode:          DNSSystem,
		TransproxyListen: "127.0.0.1:12345",
		TransproxyPort:   12345,
		UIEnable:             false,
		BypassRulesFile:      "/config/bypass.rules.txt",
		BypassDNSRefresh:     60 * time.Second,
		AccessKeyPersistFile: "/config/outline_key.runtime.txt",
	}

	key, err := loadAccessKey(getenv)
	if err != nil {
		return nil, err
	}
	cfg.AccessKey = key

	if v := strings.TrimSpace(getenv("ROUTING_MODE")); v != "" {
		cfg.RoutingMode = RoutingMode(strings.ToLower(v))
	}
	if v := strings.TrimSpace(getenv("DIRECT_POLICY")); v != "" {
		cfg.DirectPolicy = DirectPolicy(strings.ToLower(v))
	}
	if v := strings.TrimSpace(getenv("LAN_INTERFACE")); v != "" {
		cfg.LANInterface = v
	}
	if v := strings.TrimSpace(getenv("GATEWAY_ENABLE")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("GATEWAY_ENABLE: %w", err)
		}
		cfg.GatewayEnable = b
	}
	if v := strings.TrimSpace(getenv("SOCKS_LISTEN")); v != "" {
		cfg.SOCKSListen = v
	}
	if v := strings.TrimSpace(getenv("HEALTH_LISTEN")); v != "" {
		cfg.HealthListen = v
	}
	if v := strings.TrimSpace(getenv("TRANSPROXY_LISTEN")); v != "" {
		cfg.TransproxyListen = v
	}
	if v := strings.TrimSpace(getenv("LOG_LEVEL")); v != "" {
		cfg.LogLevel = strings.ToLower(v)
	}
	if v := strings.TrimSpace(getenv("LOG_FORMAT")); v != "" {
		cfg.LogFormat = strings.ToLower(v)
	}
	if v := strings.TrimSpace(getenv("DNS_MODE")); v != "" {
		cfg.DNSMode = DNSMode(strings.ToLower(v))
	}
	if v := strings.TrimSpace(getenv("DNS_SERVERS")); v != "" {
		cfg.DNSServers = splitCSV(v)
	}
	if v := strings.TrimSpace(getenv("RECONNECT_BASE_DELAY")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("RECONNECT_BASE_DELAY: %w", err)
		}
		cfg.ReconnectBase = d
	}
	if v := strings.TrimSpace(getenv("RECONNECT_MAX_DELAY")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("RECONNECT_MAX_DELAY: %w", err)
		}
		cfg.ReconnectMax = d
	}
	if v := strings.TrimSpace(getenv("UI_ENABLE")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("UI_ENABLE: %w", err)
		}
		cfg.UIEnable = b
	}
	if v := strings.TrimSpace(getenv("UI_TOKEN")); v != "" {
		cfg.UIToken = v
	}
	if v := strings.TrimSpace(getenv("BYPASS_RULES_FILE")); v != "" {
		cfg.BypassRulesFile = v
	}
	if v := strings.TrimSpace(getenv("BYPASS_DNS_REFRESH")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("BYPASS_DNS_REFRESH: %w", err)
		}
		cfg.BypassDNSRefresh = d
	}
	if v := strings.TrimSpace(getenv("OUTLINE_KEY_PERSIST_FILE")); v != "" {
		cfg.AccessKeyPersistFile = v
	}

	bypass, err := loadCIDRs(getenv, "BYPASS_CIDRS", "BYPASS_CIDRS_FILE")
	if err != nil {
		return nil, fmt.Errorf("bypass CIDRs: %w", err)
	}
	cfg.BypassCIDRs = mergeIPNets(DefaultBypassCIDRs(), bypass)

	tunnel, err := loadCIDRs(getenv, "TUNNEL_CIDRS", "TUNNEL_CIDRS_FILE")
	if err != nil {
		return nil, fmt.Errorf("tunnel CIDRs: %w", err)
	}
	cfg.TunnelCIDRs = tunnel

	if host, portStr, err := net.SplitHostPort(cfg.TransproxyListen); err == nil {
		p, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("TRANSPROXY_LISTEN port: %w", err)
		}
		cfg.TransproxyPort = p
		_ = host
	} else {
		return nil, fmt.Errorf("TRANSPROXY_LISTEN: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadAccessKey(getenv func(string) string) (string, error) {
	// UI-persisted key wins so runtime key changes survive container restarts.
	persistPath := strings.TrimSpace(getenv("OUTLINE_KEY_PERSIST_FILE"))
	if persistPath == "" {
		persistPath = "/config/outline_key.runtime.txt"
	}
	if b, err := os.ReadFile(persistPath); err == nil {
		if key := firstNonCommentLine(string(b)); key != "" {
			return key, nil
		}
	}

	if v := strings.TrimSpace(getenv("OUTLINE_ACCESS_KEY")); v != "" {
		return v, nil
	}
	if path := strings.TrimSpace(getenv("OUTLINE_ACCESS_KEY_FILE")); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("OUTLINE_ACCESS_KEY_FILE: %w", err)
		}
		key := firstNonCommentLine(string(b))
		if key == "" {
			return "", fmt.Errorf("OUTLINE_ACCESS_KEY_FILE is empty or only comments")
		}
		return key, nil
	}
	return "", fmt.Errorf("OUTLINE_ACCESS_KEY or OUTLINE_ACCESS_KEY_FILE is required")
}

// PersistAccessKey writes the access key to path atomically (for UI key replace).
func PersistAccessKey(path, key string) error {
	path = strings.TrimSpace(path)
	key = strings.TrimSpace(key)
	if path == "" {
		return fmt.Errorf("persist path is empty")
	}
	if key == "" {
		return fmt.Errorf("access key is empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	content := "# outline-gate runtime access key (managed by UI/API)\n" + key + "\n"
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func loadCIDRs(getenv func(string) string, envKey, fileKey string) ([]net.IPNet, error) {
	var raw []string
	if v := strings.TrimSpace(getenv(envKey)); v != "" {
		raw = append(raw, splitCSV(v)...)
	}
	if path := strings.TrimSpace(getenv(fileKey)); path != "" {
		lines, err := readListFile(path)
		if err != nil {
			return nil, err
		}
		raw = append(raw, lines...)
	}
	return parseCIDRs(raw)
}

func firstNonCommentLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

func readListFile(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseCIDRs accepts "1.2.3.4", "1.2.3.4/32", or "10.0.0.0/8".
func parseCIDRs(raw []string) ([]net.IPNet, error) {
	var out []net.IPNet
	for _, s := range raw {
		n, err := ParseCIDROrIP(s)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", s, err)
		}
		out = append(out, n)
	}
	return out, nil
}

// ParseCIDROrIP parses a CIDR or bare IPv4 address (as /32).
func ParseCIDROrIP(s string) (net.IPNet, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return net.IPNet{}, fmt.Errorf("empty CIDR")
	}
	if strings.Contains(s, "/") {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return net.IPNet{}, err
		}
		return *n, nil
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return net.IPNet{}, fmt.Errorf("invalid IP or CIDR")
	}
	if v4 := ip.To4(); v4 != nil {
		return net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}, nil
	}
	return net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}, nil
}

// DefaultBypassCIDRs returns private/reserved ranges that should not go via tunnel.
func DefaultBypassCIDRs() []net.IPNet {
	defaults := []string{
		"0.0.0.0/8",
		"10.0.0.0/8",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"224.0.0.0/4",
		"240.0.0.0/4",
		"100.64.0.0/10", // CGNAT
	}
	out, err := parseCIDRs(defaults)
	if err != nil {
		panic(err)
	}
	return out
}

func mergeIPNets(a, b []net.IPNet) []net.IPNet {
	seen := make(map[string]struct{}, len(a)+len(b))
	var out []net.IPNet
	for _, list := range [][]net.IPNet{a, b} {
		for _, n := range list {
			k := n.String()
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, n)
		}
	}
	return out
}

// Validate checks semantic constraints.
func (c *Config) Validate() error {
	if c.AccessKey == "" {
		return fmt.Errorf("access key is required")
	}
	switch c.RoutingMode {
	case ModeExclude, ModeInclude:
	default:
		return fmt.Errorf("ROUTING_MODE must be exclude or include, got %q", c.RoutingMode)
	}
	switch c.DirectPolicy {
	case DirectAllow, DirectDrop:
	default:
		return fmt.Errorf("DIRECT_POLICY must be direct or drop, got %q", c.DirectPolicy)
	}
	switch c.DNSMode {
	case DNSSystem, DNSTunnel, DNSStatic:
	default:
		return fmt.Errorf("DNS_MODE must be system, tunnel, or static, got %q", c.DNSMode)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error")
	}
	if c.RoutingMode == ModeInclude && len(c.TunnelCIDRs) == 0 {
		return fmt.Errorf("include mode requires TUNNEL_CIDRS or TUNNEL_CIDRS_FILE")
	}
	if c.ReconnectBase <= 0 {
		return fmt.Errorf("RECONNECT_BASE_DELAY must be positive")
	}
	if c.ReconnectMax < c.ReconnectBase {
		return fmt.Errorf("RECONNECT_MAX_DELAY must be >= RECONNECT_BASE_DELAY")
	}
	if _, _, err := net.SplitHostPort(c.SOCKSListen); err != nil {
		return fmt.Errorf("SOCKS_LISTEN: %w", err)
	}
	if _, _, err := net.SplitHostPort(c.HealthListen); err != nil {
		return fmt.Errorf("HEALTH_LISTEN: %w", err)
	}
	if c.UIEnable && strings.TrimSpace(c.UIToken) == "" {
		return fmt.Errorf("UI_TOKEN is required when UI_ENABLE=true")
	}
	if c.BypassDNSRefresh <= 0 {
		return fmt.Errorf("BYPASS_DNS_REFRESH must be positive")
	}
	return nil
}

// RedactAccessKey returns a safe-to-log form of the access key.
func RedactAccessKey(key string) string {
	if key == "" {
		return ""
	}
	// ss://userinfo@host:port — hide userinfo
	if i := strings.Index(key, "://"); i >= 0 {
		rest := key[i+3:]
		if at := strings.LastIndex(rest, "@"); at >= 0 {
			return key[:i+3] + "***@" + rest[at+1:]
		}
	}
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "***" + key[len(key)-2:]
}
