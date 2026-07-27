package bypass

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"
)

// LookupIPFunc resolves a hostname to IPs (injectable for tests).
type LookupIPFunc func(ctx context.Context, host string) ([]net.IP, error)

// OnChangeFunc is called after rules or resolved IPs change.
type OnChangeFunc func()

// Manager owns user rules, DNS refresh for L3, and matchers for SOCKS.
type Manager struct {
	store    *Store
	static   []net.IPNet // defaults + BYPASS_CIDRS (not user domain rules)
	lookup   LookupIPFunc
	logger   *slog.Logger
	interval time.Duration
	onChange OnChangeFunc

	mu        sync.RWMutex
	matcher   *Matcher
	resolved  []net.IPNet // from domain DNS
	lastError string
}

// Options configures a Manager.
type Options struct {
	Store         *Store
	StaticBypass  []net.IPNet
	LookupIP      LookupIPFunc
	Logger        *slog.Logger
	RefreshEvery  time.Duration
	OnChange      OnChangeFunc
}

// NewManager creates a Manager. Call Refresh after Load.
func NewManager(opts Options) *Manager {
	if opts.Store == nil {
		opts.Store = NewStore("")
	}
	if opts.LookupIP == nil {
		opts.LookupIP = defaultLookupIP
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.RefreshEvery <= 0 {
		opts.RefreshEvery = 60 * time.Second
	}
	m := &Manager{
		store:    opts.Store,
		static:   append([]net.IPNet(nil), opts.StaticBypass...),
		lookup:   opts.LookupIP,
		logger:   opts.Logger,
		interval: opts.RefreshEvery,
		onChange: opts.OnChange,
	}
	m.rebuildMatcher()
	return m
}

func defaultLookupIP(ctx context.Context, host string) ([]net.IP, error) {
	// Prefer Go resolver; returns IPv4 and IPv6.
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	return ips, err
}

// Load reloads rules from disk and refreshes DNS.
func (m *Manager) Load(ctx context.Context) error {
	if err := m.store.Load(); err != nil {
		return err
	}
	m.rebuildMatcher()
	return m.Refresh(ctx)
}

// SetStatic updates static CIDR bypass (defaults + env file) without touching user rules.
func (m *Manager) SetStatic(nets []net.IPNet) {
	m.mu.Lock()
	m.static = append([]net.IPNet(nil), nets...)
	m.mu.Unlock()
	m.notify()
}

// Rules returns user rules.
func (m *Manager) Rules() []Rule {
	return m.store.Rules()
}

// AddRule adds a user rule, persists, refreshes DNS.
func (m *Manager) AddRule(ctx context.Context, raw string) (Rule, error) {
	r, err := m.store.Add(raw)
	if err != nil {
		return Rule{}, err
	}
	m.rebuildMatcher()
	_ = m.Refresh(ctx)
	return r, nil
}

// RemoveRule removes a user rule.
func (m *Manager) RemoveRule(ctx context.Context, raw string) (bool, error) {
	ok, err := m.store.Remove(raw)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	m.rebuildMatcher()
	_ = m.Refresh(ctx)
	return true, nil
}

// SetRules replaces all user rules.
func (m *Manager) SetRules(ctx context.Context, raws []string) error {
	rules := make([]Rule, 0, len(raws))
	for _, raw := range raws {
		r, err := ParseRule(raw)
		if err != nil {
			return err
		}
		rules = append(rules, r)
	}
	if err := m.store.Set(rules); err != nil {
		return err
	}
	m.rebuildMatcher()
	return m.Refresh(ctx)
}

// MatchHost reports SOCKS/domain bypass.
func (m *Manager) MatchHost(host string) bool {
	m.mu.RLock()
	matcher := m.matcher
	m.mu.RUnlock()
	if matcher == nil {
		return false
	}
	return matcher.MatchHost(host)
}

// MatchIP reports whether IP matches user IP/CIDR rules (not resolved domain IPs).
// For SOCKS with IP target, also check EffectiveBypassNets via routing engine.
func (m *Manager) MatchIP(ip net.IP) bool {
	m.mu.RLock()
	matcher := m.matcher
	m.mu.RUnlock()
	if matcher == nil {
		return false
	}
	if matcher.MatchIP(ip) {
		return true
	}
	// Also treat resolved domain IPs as bypass for SOCKS IP connect.
	m.mu.RLock()
	resolved := m.resolved
	m.mu.RUnlock()
	for i := range resolved {
		if resolved[i].Contains(ip) {
			return true
		}
	}
	return false
}

// ShouldBypassHost is true if host (name or IP literal) should skip the tunnel.
func (m *Manager) ShouldBypassHost(host string) bool {
	host = NormalizeHost(host)
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return m.MatchIP(ip)
	}
	return m.MatchHost(host)
}

// EffectiveBypassNets returns static + user IP/CIDR + resolved domain IPs.
func (m *Manager) EffectiveBypassNets() []net.IPNet {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]net.IPNet, 0, len(m.static)+len(m.resolved)+8)
	seen := make(map[string]struct{})
	add := func(n net.IPNet) {
		k := n.String()
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		out = append(out, n)
	}
	for _, n := range m.static {
		add(n)
	}
	for _, n := range StaticNets(m.store.Rules()) {
		add(n)
	}
	// store.Rules under RLock of manager — store has own lock; OK.
	for _, n := range m.resolved {
		add(n)
	}
	return out
}

// LastError returns the last DNS refresh error string (empty if ok).
func (m *Manager) LastError() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastError
}

// ResolvedNets returns a copy of DNS-resolved bypass nets.
func (m *Manager) ResolvedNets() []net.IPNet {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]net.IPNet, len(m.resolved))
	copy(out, m.resolved)
	return out
}

// Refresh re-resolves domain rules and notifies OnChange.
func (m *Manager) Refresh(ctx context.Context) error {
	hosts := DomainsToResolve(m.store.Rules())
	var nets []net.IPNet
	var firstErr error
	for _, h := range hosts {
		ips, err := m.lookup(ctx, h)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			m.logger.Debug("bypass DNS lookup failed", "host", h, "err", err)
			continue
		}
		for _, ip := range ips {
			if v4 := ip.To4(); v4 != nil {
				nets = append(nets, net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)})
			}
			// skip v6 for nft v1 sets (ipv4 only)
		}
	}
	m.mu.Lock()
	m.resolved = nets
	if firstErr != nil {
		m.lastError = firstErr.Error()
	} else {
		m.lastError = ""
	}
	m.mu.Unlock()
	m.notify()
	return firstErr
}

// RunRefreshLoop periodically refreshes DNS until ctx is done.
func (m *Manager) RunRefreshLoop(ctx context.Context) {
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = m.Refresh(ctx)
		}
	}
}

func (m *Manager) rebuildMatcher() {
	rules := m.store.Rules()
	m.mu.Lock()
	m.matcher = NewMatcher(rules)
	m.mu.Unlock()
}

func (m *Manager) notify() {
	if m.onChange != nil {
		m.onChange()
	}
}
