package config

import (
	"os"
	"path/filepath"
	"testing"
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

func TestRedactAccessKey(t *testing.T) {
	got := RedactAccessKey("ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz@1.2.3.4:8388")
	if got != "ss://***@1.2.3.4:8388" {
		t.Fatalf("got %q", got)
	}
	if RedactAccessKey("short") != "***" {
		t.Fatal("short key")
	}
}
