// Package gateway applies Linux nftables rules for L3 split-tunnel forwarding.
package gateway

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/unhex/outline-gate/internal/config"
	"github.com/unhex/outline-gate/internal/routing"
)

const tableName = "outline_gate"

// Gateway manages nftables rules for transparent redirect + masquerade.
type Gateway struct {
	cfg    *config.Config
	engine *routing.Engine
	logger *slog.Logger
	mu     sync.Mutex
	active bool
}

// New creates a Gateway controller (rules not applied until Apply).
func New(cfg *config.Config, engine *routing.Engine, logger *slog.Logger) *Gateway {
	if logger == nil {
		logger = slog.Default()
	}
	return &Gateway{cfg: cfg, engine: engine, logger: logger}
}

// Active reports whether rules are currently applied.
func (g *Gateway) Active() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active
}

// Apply (re)creates the outline_gate nftables table with current routing sets.
func (g *Gateway) Apply() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := ensureIPForward(); err != nil {
		g.logger.Warn("ip_forward", "err", err)
	}

	script, err := g.buildNFTScript()
	if err != nil {
		return err
	}
	g.logger.Debug("nft script", "script", script)

	// Replace table atomically: delete if exists, then add.
	_ = runNFT(fmt.Sprintf("delete table inet %s", tableName))
	if err := runNFT(script); err != nil {
		g.active = false
		return fmt.Errorf("nft apply: %w", err)
	}
	g.active = true
	g.logger.Info("gateway rules applied",
		"mode", g.cfg.RoutingMode,
		"transproxy_port", g.cfg.TransproxyPort,
	)
	return nil
}

// Flush removes the outline_gate table.
func (g *Gateway) Flush() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	err := runNFT(fmt.Sprintf("delete table inet %s", tableName))
	// ignore "does not exist"
	if err != nil && !strings.Contains(err.Error(), "No such file") && !strings.Contains(strings.ToLower(err.Error()), "does not exist") {
		// nft returns non-zero if missing — treat as ok if table gone
		if !strings.Contains(err.Error(), "not found") {
			g.logger.Debug("flush", "err", err)
		}
	}
	g.active = false
	g.logger.Info("gateway rules flushed")
	return nil
}

// UpdateEngine swaps the routing engine and re-applies if active.
func (g *Gateway) UpdateEngine(engine *routing.Engine) error {
	g.mu.Lock()
	was := g.active
	g.engine = engine
	g.mu.Unlock()
	if was {
		return g.Apply()
	}
	return nil
}

func (g *Gateway) buildNFTScript() (string, error) {
	if g.engine == nil {
		return "", fmt.Errorf("routing engine is nil")
	}
	port := g.cfg.TransproxyPort
	if port <= 0 {
		return "", fmt.Errorf("invalid transproxy port")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\n", tableName)

	// sets
	fmt.Fprintf(&b, "add set inet %s bypass { type ipv4_addr; flags interval; }\n", tableName)
	fmt.Fprintf(&b, "add set inet %s tunnel { type ipv4_addr; flags interval; }\n", tableName)

	for _, n := range g.engine.BypassNets() {
		if n.IP.To4() == nil {
			continue
		}
		fmt.Fprintf(&b, "add element inet %s bypass { %s }\n", tableName, n.String())
	}
	for _, n := range g.engine.TunnelNets() {
		if n.IP.To4() == nil {
			continue
		}
		fmt.Fprintf(&b, "add element inet %s tunnel { %s }\n", tableName, n.String())
	}

	// prerouting: redirect matching TCP to local transparent proxy
	fmt.Fprintf(&b, "add chain inet %s prerouting { type nat hook prerouting priority dstnat; policy accept; }\n", tableName)
	// also handle locally generated traffic if needed — for LAN clients PREROUTING is enough
	fmt.Fprintf(&b, "add chain inet %s output { type nat hook output priority dstnat; policy accept; }\n", tableName)

	// Skip redirect of traffic already aimed at the local transparent port.
	fmt.Fprintf(&b, "add rule inet %s prerouting tcp dport %d return\n", tableName, port)

	switch g.cfg.RoutingMode {
	case config.ModeExclude:
		// redirect TCP if NOT in bypass
		fmt.Fprintf(&b, "add rule inet %s prerouting ip daddr != @bypass meta l4proto tcp redirect to :%d\n", tableName, port)
	case config.ModeInclude:
		fmt.Fprintf(&b, "add rule inet %s prerouting ip daddr @tunnel meta l4proto tcp redirect to :%d\n", tableName, port)
		if g.cfg.DirectPolicy == config.DirectDrop {
			fmt.Fprintf(&b, "add chain inet %s forward { type filter hook forward priority filter; policy accept; }\n", tableName)
			// drop forwarded traffic that is neither bypass nor tunnel (best-effort)
			fmt.Fprintf(&b, "add rule inet %s forward ip daddr != @bypass ip daddr != @tunnel drop\n", tableName)
		}
	}

	// masquerade for forwarded traffic leaving LAN interface (or any)
	fmt.Fprintf(&b, "add chain inet %s postrouting { type nat hook postrouting priority srcnat; policy accept; }\n", tableName)
	if ifc := strings.TrimSpace(g.cfg.LANInterface); ifc != "" {
		fmt.Fprintf(&b, "add rule inet %s postrouting oifname %q masquerade\n", tableName, ifc)
	} else {
		fmt.Fprintf(&b, "add rule inet %s postrouting masquerade\n", tableName)
	}

	return b.String(), nil
}

func runNFT(script string) error {
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ensureIPForward() error {
	const path = "/proc/sys/net/ipv4/ip_forward"
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(b)) == "1" {
		return nil
	}
	return os.WriteFile(path, []byte("1\n"), 0o644)
}

// DryRunScript returns the nft script without applying (for tests).
func (g *Gateway) DryRunScript() (string, error) {
	return g.buildNFTScript()
}
