// Package tunnel switches the SOCKS/L3 dialer between Outline and AmneziaWG.
package tunnel

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	BackendOutline = "outline"
	BackendAmnezia = "amnezia"
)

// Dialer is the subset used by SOCKS, HTTP CONNECT, transproxy and DoH.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Mux forwards DialContext to the selected backend.
type Mux struct {
	outline Dialer
	amnezia Dialer

	outlineReady func() bool
	amneziaReady func() bool
	outlineIP    func() net.IP
	amneziaIP    func() net.IP

	mu      sync.RWMutex
	backend atomic.Value // string
}

// NewMux starts on Outline.
func NewMux(outline, amnezia Dialer, outlineReady, amneziaReady func() bool, outlineIP, amneziaIP func() net.IP) *Mux {
	m := &Mux{
		outline:      outline,
		amnezia:      amnezia,
		outlineReady: outlineReady,
		amneziaReady: amneziaReady,
		outlineIP:    outlineIP,
		amneziaIP:    amneziaIP,
	}
	m.backend.Store(BackendOutline)
	return m
}

// Backend returns outline or amnezia.
func (m *Mux) Backend() string {
	if m == nil {
		return BackendOutline
	}
	v, _ := m.backend.Load().(string)
	if v == "" {
		return BackendOutline
	}
	return v
}

// SetBackend only switches the dialer pointer. The caller brings the
// Amnezia interface up or down around this call.
func (m *Mux) SetBackend(name string) error {
	name = Normalize(name)
	if name != BackendOutline && name != BackendAmnezia {
		return fmt.Errorf("unknown tunnel backend %q", name)
	}
	if name == BackendAmnezia && m.amnezia == nil {
		return fmt.Errorf("amnezia backend is not configured")
	}
	m.mu.Lock()
	m.backend.Store(name)
	m.mu.Unlock()
	return nil
}

// DialContext implements Dialer.
func (m *Mux) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d := m.active()
	if d == nil {
		return nil, fmt.Errorf("tunnel dialer not ready")
	}
	return d.DialContext(ctx, network, address)
}

// Ready reports the selected backend.
func (m *Mux) Ready() bool {
	switch m.Backend() {
	case BackendAmnezia:
		return m.amneziaReady != nil && m.amneziaReady()
	default:
		return m.outlineReady != nil && m.outlineReady()
	}
}

// ServerIP is the current transport server (Outline or Amnezia endpoint).
func (m *Mux) ServerIP() net.IP {
	switch m.Backend() {
	case BackendAmnezia:
		if m.amneziaIP != nil {
			return m.amneziaIP()
		}
	default:
		if m.outlineIP != nil {
			return m.outlineIP()
		}
	}
	return nil
}

func (m *Mux) active() Dialer {
	if m.Backend() == BackendAmnezia {
		return m.amnezia
	}
	return m.outline
}

// Normalize maps empty/alias names to outline|amnezia.
func Normalize(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", BackendOutline, "ss", "ss://", "outline-ss":
		return BackendOutline
	case BackendAmnezia, "awg", "amneziawg", "amneziavpn", "vpn":
		return BackendAmnezia
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}
