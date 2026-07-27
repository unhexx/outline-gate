// Package routing implements pure split-tunnel decision logic.
package routing

import (
	"net"

	"github.com/unhex/outline-gate/internal/config"
)

// Path is the selected forwarding path for a destination.
type Path int

const (
	PathDirect Path = iota
	PathTunnel
	PathDrop
)

func (p Path) String() string {
	switch p {
	case PathDirect:
		return "direct"
	case PathTunnel:
		return "tunnel"
	case PathDrop:
		return "drop"
	default:
		return "unknown"
	}
}

// Engine decides whether a destination should go via tunnel.
type Engine struct {
	Mode         config.RoutingMode
	DirectPolicy config.DirectPolicy
	Bypass       []net.IPNet
	Tunnel       []net.IPNet
	ServerIPs    []net.IP
}

// New builds an engine from config and optional Outline server IPs (always bypassed).
func New(cfg *config.Config, serverIPs []net.IP) *Engine {
	return &Engine{
		Mode:         cfg.RoutingMode,
		DirectPolicy: cfg.DirectPolicy,
		Bypass:       append([]net.IPNet(nil), cfg.BypassCIDRs...),
		Tunnel:       append([]net.IPNet(nil), cfg.TunnelCIDRs...),
		ServerIPs:    append([]net.IP(nil), serverIPs...),
	}
}

// NewWithBypass is like New but uses an explicit bypass list (e.g. static + user + resolved).
func NewWithBypass(cfg *config.Config, bypass []net.IPNet, serverIPs []net.IP) *Engine {
	return &Engine{
		Mode:         cfg.RoutingMode,
		DirectPolicy: cfg.DirectPolicy,
		Bypass:       append([]net.IPNet(nil), bypass...),
		Tunnel:       append([]net.IPNet(nil), cfg.TunnelCIDRs...),
		ServerIPs:    append([]net.IP(nil), serverIPs...),
	}
}

// WithBypass returns a shallow copy with a new bypass list.
func (e *Engine) WithBypass(bypass []net.IPNet) *Engine {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Bypass = append([]net.IPNet(nil), bypass...)
	cp.ServerIPs = append([]net.IP(nil), e.ServerIPs...)
	cp.Tunnel = append([]net.IPNet(nil), e.Tunnel...)
	return &cp
}

// WithServerIPs returns a shallow copy with updated Outline server IPs.
func (e *Engine) WithServerIPs(serverIPs []net.IP) *Engine {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Bypass = append([]net.IPNet(nil), e.Bypass...)
	cp.ServerIPs = append([]net.IP(nil), serverIPs...)
	cp.Tunnel = append([]net.IPNet(nil), e.Tunnel...)
	return &cp
}

// Decide returns the path for dst (IPv4 preferred).
func (e *Engine) Decide(dst net.IP) Path {
	if dst == nil {
		return PathDrop
	}
	if v4 := dst.To4(); v4 != nil {
		dst = v4
	}

	if e.isServer(dst) || containsIP(e.Bypass, dst) {
		return PathDirect
	}

	switch e.Mode {
	case config.ModeExclude:
		return PathTunnel
	case config.ModeInclude:
		if containsIP(e.Tunnel, dst) {
			return PathTunnel
		}
		if e.DirectPolicy == config.DirectDrop {
			return PathDrop
		}
		return PathDirect
	default:
		return PathDrop
	}
}

// ShouldTunnel is a convenience for boolean checks.
func (e *Engine) ShouldTunnel(dst net.IP) bool {
	return e.Decide(dst) == PathTunnel
}

// IsServerIP reports whether dst is a configured Outline server address.
func (e *Engine) IsServerIP(dst net.IP) bool {
	return e != nil && e.isServer(dst)
}

// BypassNets returns the effective bypass set (for nftables).
func (e *Engine) BypassNets() []net.IPNet {
	out := append([]net.IPNet(nil), e.Bypass...)
	for _, ip := range e.ServerIPs {
		if ip == nil {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			out = append(out, net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)})
		}
	}
	return out
}

// TunnelNets returns tunnel targets (include mode).
func (e *Engine) TunnelNets() []net.IPNet {
	return append([]net.IPNet(nil), e.Tunnel...)
}

func (e *Engine) isServer(dst net.IP) bool {
	for _, ip := range e.ServerIPs {
		if ip == nil {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		if ip.Equal(dst) {
			return true
		}
	}
	return false
}

func containsIP(nets []net.IPNet, ip net.IP) bool {
	for i := range nets {
		if nets[i].Contains(ip) {
			return true
		}
	}
	return false
}
