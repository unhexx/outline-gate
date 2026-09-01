package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadFromEnv_MinimalExclude(t *testing.T) {
	env := map[string]string{
		"OUTLINE_ACCESS_KEY": "ss://secret@1.2.3.4:8388",
		"ROUTING_MODE":       "exclude",
	}
	cfg, err := LoadFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RoutingMode != ModeExclude {
		t.Fatalf("mode: %s", cfg.RoutingMode)
	}
	if len(cfg.BypassCIDRs) < 5 {
		t.Fatalf("expected default bypass CIDRs, got %d", len(cfg.BypassCIDRs))
	}
	if cfg.GatewayEnable {
		t.Fatal("gateway should default false (SOCKS-only until explicitly enabled)")
	}
	if cfg.BlockRulesFile != "/config/block.rules.txt" {
		t.Fatalf("BlockRulesFile default: %s", cfg.BlockRulesFile)
	}
}

func TestLoadFromEnv_IncludeRequiresTunnelList(t *testing.T) {
	env := map[string]string{
		"OUTLINE_ACCESS_KEY": "ss://secret@1.2.3.4:8388",
		"ROUTING_MODE":       "include",
	}
	_, err := LoadFromEnv(func(k string) string { return env[k] })
	if err == nil {
		t.Fatal("expected error for empty tunnel list")
	}
}

func TestLoadFromEnv_IncludeWithCIDRs(t *testing.T) {
	env := map[string]string{
		"OUTLINE_ACCESS_KEY": "ss://secret@1.2.3.4:8388",
		"ROUTING_MODE":       "include",
		"TUNNEL_CIDRS":       "8.8.8.8,1.1.1.0/24",
	}
	cfg, err := LoadFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TunnelCIDRs) != 2 {
		t.Fatalf("tunnel cidrs: %d", len(cfg.TunnelCIDRs))
	}
}

func TestLoadFromEnv_KeyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	if err := os.WriteFile(path, []byte("ss://filekey@9.9.9.9:443\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"OUTLINE_ACCESS_KEY_FILE": path,
		"ROUTING_MODE":            "exclude",
	}
	cfg, err := LoadFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AccessKey != "ss://filekey@9.9.9.9:443" {
		t.Fatalf("key: %q", cfg.AccessKey)
	}
}

func TestLoadFromEnv_BypassFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bypass.txt")
	content := "# comment\n203.0.113.0/24\n\n198.51.100.1\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"OUTLINE_ACCESS_KEY": "ss://x@1.1.1.1:1",
		"BYPASS_CIDRS_FILE":  path,
	}
	cfg, err := LoadFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range cfg.BypassCIDRs {
		if n.String() == "203.0.113.0/24" {
			found = true
		}
	}
	if !found {
		t.Fatal("bypass file CIDR not loaded")
	}
}

func TestLoadFromEnv_MissingKey(t *testing.T) {
	_, err := LoadFromEnv(func(string) string { return "" })
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFromEnv_InvalidMode(t *testing.T) {
	env := map[string]string{
		"OUTLINE_ACCESS_KEY": "ss://x@1.1.1.1:1",
		"ROUTING_MODE":       "weird",
	}
	_, err := LoadFromEnv(func(k string) string { return env[k] })
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFromEnv_InvalidDirectPolicy(t *testing.T) {
	env := map[string]string{
		"OUTLINE_ACCESS_KEY": "ss://x@1.1.1.1:1",
		"ROUTING_MODE":       "exclude",
		"DIRECT_POLICY":      "typo",
	}
	_, err := LoadFromEnv(func(k string) string { return env[k] })
	if err == nil {
		t.Fatal("expected error for invalid DIRECT_POLICY")
	}
}

func TestLoadFromEnv_SOCKSAllowCIDRs(t *testing.T) {
	env := map[string]string{
		"OUTLINE_ACCESS_KEY": "ss://x@1.1.1.1:1",
		"SOCKS_ALLOW_CIDRS":  "127.0.0.0/8,10.0.0.0/8",
	}
	cfg, err := LoadFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.SOCKSAllowCIDRs) != 2 {
		t.Fatalf("SOCKSAllowCIDRs: %d", len(cfg.SOCKSAllowCIDRs))
	}
}

func TestLoadFromEnv_UIRequiresToken(t *testing.T) {
	env := map[string]string{
		"OUTLINE_ACCESS_KEY": "ss://x@1.1.1.1:1",
		"UI_ENABLE":          "true",
	}
	_, err := LoadFromEnv(func(k string) string { return env[k] })
	if err == nil {
		t.Fatal("expected UI_TOKEN required")
	}
	env["UI_TOKEN"] = "s3cret"
	cfg, err := LoadFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UIEnable || cfg.UIToken != "s3cret" {
		t.Fatalf("%+v", cfg)
	}
}

func TestLoadFromEnv_ProbeAndRefreshDefaults(t *testing.T) {
	env := map[string]string{
		"OUTLINE_ACCESS_KEY": "ss://x@1.1.1.1:1",
	}
	cfg, err := LoadFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SSConfRefresh != 2*60*1e9 && cfg.SSConfRefresh != 2*time.Minute {
		t.Fatalf("SSConfRefresh default: %s", cfg.SSConfRefresh)
	}
	if cfg.ProbeAddr != "1.1.1.1:443" {
		t.Fatalf("ProbeAddr default: %q", cfg.ProbeAddr)
	}
	if cfg.ProbeInterval != 30*time.Second || cfg.ProbeTimeout != 8*time.Second || cfg.ProbeFails != 2 {
		t.Fatalf("probe defaults: interval=%s timeout=%s fails=%d", cfg.ProbeInterval, cfg.ProbeTimeout, cfg.ProbeFails)
	}
}

func TestLoadFromEnv_ProbeOffAndRefreshZero(t *testing.T) {
	env := map[string]string{
		"OUTLINE_ACCESS_KEY":      "ss://x@1.1.1.1:1",
		"TUNNEL_PROBE_ADDR":       "off",
		"SSCONF_REFRESH_INTERVAL": "0s",
		"TUNNEL_PROBE_INTERVAL":   "15s",
		"TUNNEL_PROBE_TIMEOUT":    "3s",
		"TUNNEL_PROBE_FAILS":      "4",
	}
	cfg, err := LoadFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProbeAddr != "" {
		t.Fatalf("probe should be off, got %q", cfg.ProbeAddr)
	}
	if cfg.SSConfRefresh != 0 {
		t.Fatalf("refresh: %s", cfg.SSConfRefresh)
	}
	if cfg.ProbeFails != 4 || cfg.ProbeInterval != 15*time.Second || cfg.ProbeTimeout != 3*time.Second {
		t.Fatalf("overrides: %+v", cfg)
	}
}

func TestLoadFromEnv_InvalidProbeAddr(t *testing.T) {
	env := map[string]string{
		"OUTLINE_ACCESS_KEY": "ss://x@1.1.1.1:1",
		"TUNNEL_PROBE_ADDR":  "not-a-host-port",
	}
	_, err := LoadFromEnv(func(k string) string { return env[k] })
	if err == nil {
		t.Fatal("expected TUNNEL_PROBE_ADDR error")
	}
}

func TestLoadFromEnv_InvalidProbeFails(t *testing.T) {
	env := map[string]string{
		"OUTLINE_ACCESS_KEY": "ss://x@1.1.1.1:1",
		"TUNNEL_PROBE_FAILS": "0",
	}
	_, err := LoadFromEnv(func(k string) string { return env[k] })
	if err == nil {
		t.Fatal("expected TUNNEL_PROBE_FAILS error")
	}
}

func TestParseCIDROrIP(t *testing.T) {
	n, err := ParseCIDROrIP("10.1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if n.String() != "10.1.2.3/32" {
		t.Fatalf("got %s", n.String())
	}
	_, err = ParseCIDROrIP("not-an-ip")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPersistAccessKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key.runtime.txt")
	if err := PersistAccessKey(path, "ss://secret@1.2.3.4:8388"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "ss://secret@1.2.3.4:8388") {
		t.Fatalf("content: %q", b)
	}
	if err := PersistAccessKey(path, ""); err == nil {
		t.Fatal("empty key should fail")
	}
}

func TestRedactAccessKey(t *testing.T) {
	got := RedactAccessKey("ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz@1.2.3.4:8388")
	if got != "ss://***@1.2.3.4:8388" {
		t.Fatalf("got %q", got)
	}
	if RedactAccessKey("short") != "***" {
		t.Fatal("short key")
	}
}
