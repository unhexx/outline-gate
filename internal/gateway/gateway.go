// Package gateway applies Linux nftables rules for L3 split-tunnel forwarding.
package gateway

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/unhexx/outline-gate/internal/config"
	"github.com/unhexx/outline-gate/internal/routing"
)

const tableName = "outline_gate"

// Gateway manages nftables rules for transparent redirect + masquerade.
type Gateway struct {
	cfg    *config.Config
	engine *routing.Engine
	logger *slog.Logger
	mu     sync.Mutex
	active bool
	// nftBin is resolved once (LookPath or common absolute paths).
	nftBin string
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
	return g.applyLocked()
}

// applyLocked assumes g.mu is held.
func (g *Gateway) applyLocked() error {
	if err := ensureIPForward(); err != nil {
		// Non-fatal for Apply: rules may still work if host already forwards.
		g.logger.Error("ip_forward not enabled",
			"err", err,
			"hint", "run with CAP_NET_ADMIN / --privileged / sysctl net.ipv4.ip_forward=1",
		)
	}

	script, err := g.buildNFTScript()
	if err != nil {
		return err
	}
	g.logger.Debug("nft script", "script", script)

	// Replace table atomically: delete if exists, then add.
	_ = g.runNFTLocked(fmt.Sprintf("delete table inet %s", tableName))
	if err := g.runNFTLocked(script); err != nil {
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
	err := g.runNFTLocked(fmt.Sprintf("delete table inet %s", tableName))
	g.active = false
	if err != nil && !isNFTNotFound(err) {
		g.logger.Error("gateway flush failed", "err", err)
		return fmt.Errorf("nft flush: %w", err)
	}
	g.logger.Info("gateway rules flushed")
	return nil
}

// UpdateEngine swaps the routing engine and re-applies if active.
// Uses a single lock path so Flush cannot race between active check and Apply.
func (g *Gateway) UpdateEngine(engine *routing.Engine) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.engine = engine
	if !g.active {
		return nil
	}
	return g.applyLocked()
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

	// @private: RFC1918/reserved — kernel path only (no log flood).
	// All other TCP is REDIRECTed to transparent proxy; tunnel vs Direct
	// is decided in userspace so both appear in the connection log.
	// Note: IPv4-only sets; IPv6 is not redirected (see docs).
	fmt.Fprintf(&b, "add set inet %s private { type ipv4_addr; flags interval; }\n", tableName)
	for _, n := range config.DefaultBypassCIDRs() {
		if n.IP.To4() == nil {
			continue
		}
		fmt.Fprintf(&b, "add element inet %s private { %s }\n", tableName, n.String())
	}

	// prerouting: redirect non-private TCP to local transparent proxy
	fmt.Fprintf(&b, "add chain inet %s prerouting { type nat hook prerouting priority dstnat; policy accept; }\n", tableName)
	fmt.Fprintf(&b, "add chain inet %s output { type nat hook output priority dstnat; policy accept; }\n", tableName)

	// Skip redirect of traffic already aimed at the local transparent port.
	fmt.Fprintf(&b, "add rule inet %s prerouting tcp dport %d return\n", tableName, port)
	// Private/reserved destinations stay on the kernel forward path.
	fmt.Fprintf(&b, "add rule inet %s prerouting ip daddr @private return\n", tableName)
	// Everything else (tunnel + user Direct + include residual) → userspace.
	fmt.Fprintf(&b, "add rule inet %s prerouting meta l4proto tcp redirect to :%d\n", tableName, port)

	// masquerade for forwarded traffic leaving LAN interface (or any)
	fmt.Fprintf(&b, "add chain inet %s postrouting { type nat hook postrouting priority srcnat; policy accept; }\n", tableName)
	if ifc := strings.TrimSpace(g.cfg.LANInterface); ifc != "" {
		fmt.Fprintf(&b, "add rule inet %s postrouting oifname %q masquerade\n", tableName, ifc)
	} else {
		fmt.Fprintf(&b, "add rule inet %s postrouting masquerade\n", tableName)
	}

	return b.String(), nil
}

func (g *Gateway) runNFTLocked(script string) error {
	bin, err := g.resolveNFT()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (g *Gateway) resolveNFT() (string, error) {
	if g.nftBin != "" {
		return g.nftBin, nil
	}
	if p, err := exec.LookPath("nft"); err == nil {
		g.nftBin = p
		return p, nil
	}
	for _, cand := range []string{"/usr/sbin/nft", "/sbin/nft", "/usr/bin/nft"} {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			g.nftBin = cand
			return cand, nil
		}
	}
	return "", fmt.Errorf("nft binary not found in PATH or /usr/sbin/nft (install nftables)")
}

func isNFTNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no such file or directory")
}

func ensureIPForward() error {
	const path = "/proc/sys/net/ipv4/ip_forward"
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w (need host network / proc mount?)", path, err)
	}
	if strings.TrimSpace(string(b)) == "1" {
		return nil
	}
	if err := os.WriteFile(path, []byte("1\n"), 0o644); err != nil {
		return fmt.Errorf("write %s: %w (need CAP_NET_ADMIN or set sysctl net.ipv4.ip_forward=1 on the host)", path, err)
	}
	return nil
}

// DryRunScript returns the nft script without applying (for tests).
func (g *Gateway) DryRunScript() (string, error) {
	return g.buildNFTScript()
}
