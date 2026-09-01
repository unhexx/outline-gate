// Package outline wraps outline-sdk configurl dialers and server endpoint resolution.
package outline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.getoutline.org/sdk/transport"
	"golang.getoutline.org/sdk/x/configurl"
)

// Dialer dials remote TCP endpoints through the Outline transport.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Client manages an Outline StreamDialer with readiness tracking.
type Client struct {
	accessKey     string
	reconnectBase time.Duration
	reconnectMax  time.Duration
	probeAddr     string
	probeInterval time.Duration
	probeTimeout  time.Duration
	probeFails    int
	refreshEvery  time.Duration
	logger        *slog.Logger

	mu          sync.RWMutex
	dialer      transport.StreamDialer
	serverIP    net.IP
	expandedKey string
	ready       atomic.Bool
	failCount   atomic.Int32

	wake chan struct{}

	providers *configurl.ProviderContainer
	expand    func(ctx context.Context, key string) (string, error)
	newDialer func(ctx context.Context, key string) (transport.StreamDialer, error)

	// OnProbe is invoked after each tunnel probe (ok=false on dial error).
	OnProbe func(ok bool)
	// OnRefresh is invoked after an ssconf refresh attempt.
	// err != nil means the fetch/rebuild failed and the previous dialer was kept
	// (unless Ready was already false).
	OnRefresh func(err error, changed bool)
}

// Options configures Client construction.
type Options struct {
	AccessKey       string
	ReconnectBase   time.Duration
	ReconnectMax    time.Duration
	ProbeAddr       string
	ProbeInterval   time.Duration
	ProbeTimeout    time.Duration
	ProbeFails      int
	RefreshInterval time.Duration
	Logger          *slog.Logger
}

// New creates a Client. Call Connect to establish the dialer.
func New(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.AccessKey) == "" {
		return nil, fmt.Errorf("access key is required")
	}
	if opts.ReconnectBase <= 0 {
		opts.ReconnectBase = time.Second
	}
	if opts.ReconnectMax < opts.ReconnectBase {
		opts.ReconnectMax = 60 * time.Second
	}
	if opts.ProbeFails <= 0 {
		opts.ProbeFails = 2
	}
	if opts.ProbeAddr != "" {
		if opts.ProbeInterval <= 0 {
			opts.ProbeInterval = 30 * time.Second
		}
		if opts.ProbeTimeout <= 0 {
			opts.ProbeTimeout = 8 * time.Second
		}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	ip, err := ResolveServerIP(opts.AccessKey)
	if err != nil {
		// Non-fatal for multi-part configs: leave nil and continue.
		ip = nil
	}
	providers := configurl.NewDefaultProviders()
	c := &Client{
		accessKey:     opts.AccessKey,
		reconnectBase: opts.ReconnectBase,
		reconnectMax:  opts.ReconnectMax,
		probeAddr:     strings.TrimSpace(opts.ProbeAddr),
		probeInterval: opts.ProbeInterval,
		probeTimeout:  opts.ProbeTimeout,
		probeFails:    opts.ProbeFails,
		refreshEvery:  opts.RefreshInterval,
		logger:        opts.Logger,
		serverIP:      ip,
		providers:     providers,
		wake:          make(chan struct{}, 1),
		expand:        ExpandAccessKey,
	}
	c.newDialer = func(ctx context.Context, key string) (transport.StreamDialer, error) {
		return providers.NewStreamDialer(ctx, key)
	}
	return c, nil
}

// ServerIP returns the resolved Outline server IPv4 (may be nil).
func (c *Client) ServerIP() net.IP {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.serverIP == nil {
		return nil
	}
	return append(net.IP(nil), c.serverIP...)
}

// Ready reports whether a dialer is available and the failure threshold has not been hit.
func (c *Client) Ready() bool {
	return c.ready.Load()
}

// Connect builds the StreamDialer. Safe to call multiple times (replaces dialer).
// ssconf:// keys are fetched and expanded to ss:// on each Connect (fresh static key).
func (c *Client) Connect(ctx context.Context) error {
	c.mu.RLock()
	accessKey := c.accessKey
	c.mu.RUnlock()
	key, err := c.expand(ctx, accessKey)
	if err != nil {
		c.ready.Store(false)
		return fmt.Errorf("outline expand key: %w", err)
	}
	if err := c.applyExpanded(ctx, key); err != nil {
		c.ready.Store(false)
		return err
	}
	return nil
}

func (c *Client) applyExpanded(ctx context.Context, key string) error {
	d, err := c.newDialer(ctx, key)
	if err != nil {
		// Leave the previous dialer in place; caller decides whether to flip Ready.
		return fmt.Errorf("outline dialer: %w", err)
	}
	var ip net.IP
	if resolved, err := ResolveServerIP(key); err == nil {
		ip = resolved
	}
	c.mu.Lock()
	c.dialer = d
	c.expandedKey = key
	c.serverIP = ip
	c.mu.Unlock()
	c.failCount.Store(0)
	c.ready.Store(true)
	return nil
}

// AccessKey returns the current access key (sensitive — avoid logging raw).
func (c *Client) AccessKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.accessKey
}

// SetAccessKey replaces the Outline access key and reconnects.
// On failure the previous dialer/key remains until Connect succeeds with the new key;
// if Connect fails, ready is false and the new key is kept for retry.
func (c *Client) SetAccessKey(ctx context.Context, accessKey string) error {
	accessKey = strings.TrimSpace(accessKey)
	if accessKey == "" {
		return fmt.Errorf("access key is required")
	}
	if !strings.HasPrefix(accessKey, "ss://") && !strings.HasPrefix(accessKey, "ssconf://") {
		return fmt.Errorf("access key must start with ss:// or ssconf://")
	}
	c.mu.Lock()
	c.accessKey = accessKey
	c.mu.Unlock()
	c.ready.Store(false)
	err := c.Connect(ctx)
	if err != nil {
		c.kick()
	}
	return err
}

// DialContext dials address (host:port) over the Outline tunnel (TCP).
func (c *Client) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("unsupported network %q (tcp only in v1)", network)
	}
	c.mu.RLock()
	d := c.dialer
	c.mu.RUnlock()
	if d == nil {
		return nil, fmt.Errorf("outline dialer not ready")
	}
	sc, err := d.DialStream(ctx, address)
	c.noteDialResult(err)
	if err != nil {
		return nil, err
	}
	return sc, nil
}

func (c *Client) noteDialResult(err error) {
	if err == nil {
		c.failCount.Store(0)
		return
	}
	if !c.isTransportFailure(err) {
		return
	}
	c.noteTunnelFailure()
}

// noteTunnelFailure counts a probe or transport failure toward the reconnect threshold.
func (c *Client) noteTunnelFailure() {
	n := c.failCount.Add(1)
	if int(n) >= c.failThreshold() {
		if c.ready.Swap(false) {
			c.log().Warn("outline tunnel marked not ready after consecutive dial failures",
				"failures", n, "threshold", c.failThreshold(), "server", c.endpointLocked())
			c.kick()
		}
	}
}

func (c *Client) failThreshold() int {
	if c.probeFails > 0 {
		return c.probeFails
	}
	return 2
}

func (c *Client) isTransportFailure(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	msg := err.Error()
	c.mu.RLock()
	sip := ""
	if c.serverIP != nil {
		sip = c.serverIP.String()
	}
	c.mu.RUnlock()
	if sip != "" && strings.Contains(msg, sip) {
		return true
	}
	return strings.Contains(msg, "i/o timeout")
}

func (c *Client) kick() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Client) log() *slog.Logger {
	if c.logger != nil {
		return c.logger
	}
	return slog.Default()
}

func (c *Client) endpointLocked() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if ep := AccessKeyEndpoint(c.expandedKey); ep != "" {
		return ep
	}
	if c.serverIP != nil {
		return c.serverIP.String()
	}
	return ""
}

// MaintainReady periodically ensures the dialer exists and the tunnel still works.
// On ssconf:// keys it re-fetches the dynamic config and rebuilds the dialer when
// the expanded endpoint changes. Blocks until ctx is cancelled.
func (c *Client) MaintainReady(ctx context.Context, onChange func(ready bool)) {
	delay := c.reconnectBase
	var lastProbe, lastRefresh time.Time
	needImmediateProbe := c.probeAddr != ""

	for {
		if ctx.Err() != nil {
			return
		}
		if !c.Ready() {
			if err := c.Connect(ctx); err != nil {
				if onChange != nil {
					onChange(false)
				}
				c.log().Warn("outline connect failed; will retry", "err", err, "next_in", delay)
				if !c.wait(ctx, delay) {
					return
				}
				delay *= 2
				if delay > c.reconnectMax {
					delay = c.reconnectMax
				}
				continue
			}
			if c.probeAddr != "" {
				if err := c.probe(ctx); err != nil {
					// A freshly rebuilt dialer that cannot pass a probe is not ready.
					c.ready.Store(false)
					if onChange != nil {
						onChange(false)
					}
					c.log().Warn("outline probe failed after connect; will retry",
						"err", err, "target", c.probeAddr, "next_in", delay)
					if !c.wait(ctx, delay) {
						return
					}
					delay *= 2
					if delay > c.reconnectMax {
						delay = c.reconnectMax
					}
					continue
				}
			}
			delay = c.reconnectBase
			now := time.Now()
			lastProbe = now
			lastRefresh = now
			needImmediateProbe = false
			if onChange != nil {
				onChange(true)
			}
		} else if needImmediateProbe {
			needImmediateProbe = false
			lastProbe = time.Now()
			if err := c.probe(ctx); err != nil && !c.Ready() {
				if onChange != nil {
					onChange(false)
				}
				continue
			}
		}

		now := time.Now()
		if lastRefresh.IsZero() {
			lastRefresh = now
		}
		if lastProbe.IsZero() {
			lastProbe = now
		}

		if c.refreshEvery > 0 && now.Sub(lastRefresh) >= c.refreshEvery {
			lastRefresh = now
			if changed := c.refreshIfChanged(ctx); changed {
				if c.probeAddr != "" {
					if err := c.probe(ctx); err != nil && !c.Ready() {
						if onChange != nil {
							onChange(false)
						}
						continue
					}
				}
				if onChange != nil && c.Ready() {
					onChange(true)
				}
			}
		}
		if c.probeAddr != "" && c.probeInterval > 0 && now.Sub(lastProbe) >= c.probeInterval {
			lastProbe = now
			if err := c.probe(ctx); err != nil && !c.Ready() {
				if onChange != nil {
					onChange(false)
				}
				continue
			}
		}

		if !c.wait(ctx, c.nextWake(lastProbe, lastRefresh)) {
			return
		}
	}
}

func (c *Client) nextWake(lastProbe, lastRefresh time.Time) time.Duration {
	wake := 5 * time.Second
	now := time.Now()
	if c.probeAddr != "" && c.probeInterval > 0 {
		if d := c.probeInterval - now.Sub(lastProbe); d < wake {
			if d < 0 {
				d = 0
			}
			wake = d
		}
	}
	if c.refreshEvery > 0 {
		if d := c.refreshEvery - now.Sub(lastRefresh); d < wake {
			if d < 0 {
				d = 0
			}
			wake = d
		}
	}
	// Avoid a hot loop if both intervals are due at the same instant.
	if wake < 10*time.Millisecond {
		wake = 10 * time.Millisecond
	}
	return wake
}

func (c *Client) wait(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	case <-c.wake:
		return true
	}
}

func (c *Client) probe(ctx context.Context) error {
	if c.probeAddr == "" {
		return nil
	}
	timeout := c.probeTimeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := c.DialContext(pctx, "tcp", c.probeAddr)
	ok := err == nil
	if c.OnProbe != nil {
		c.OnProbe(ok)
	}
	if err != nil {
		// Probe target is chosen by us: dest-level errors still mean "tunnel unhealthy"
		// when they were not already counted as transport failures.
		if !c.isTransportFailure(err) {
			c.noteTunnelFailure()
		}
		c.log().Warn("outline tunnel probe failed",
			"target", c.probeAddr,
			"err", err,
			"failures", c.failCount.Load(),
			"threshold", c.failThreshold(),
			"server", c.endpointLocked(),
		)
		return err
	}
	_ = conn.Close()
	return nil
}

// refreshIfChanged re-expands a dynamic key and rebuilds the dialer when the
// static ss:// endpoint changes. A fetch failure leaves the current dialer in
// place. Returns true if the dialer was replaced.
func (c *Client) refreshIfChanged(ctx context.Context) bool {
	c.mu.RLock()
	key := c.accessKey
	prev := c.expandedKey
	c.mu.RUnlock()
	if !isDynamicKey(key) {
		return false
	}
	expanded, err := c.expand(ctx, key)
	if err != nil {
		if c.OnRefresh != nil {
			c.OnRefresh(err, false)
		}
		c.log().Warn("ssconf refresh failed; keeping current dialer", "err", err)
		return false
	}
	if expanded == prev {
		if c.OnRefresh != nil {
			c.OnRefresh(nil, false)
		}
		return false
	}
	oldEP := AccessKeyEndpoint(prev)
	newEP := AccessKeyEndpoint(expanded)
	if err := c.applyExpanded(ctx, expanded); err != nil {
		if c.OnRefresh != nil {
			c.OnRefresh(err, false)
		}
		c.log().Warn("ssconf endpoint changed but dialer rebuild failed; keeping current",
			"err", err, "old", oldEP, "new", newEP)
		return false
	}
	if c.OnRefresh != nil {
		c.OnRefresh(nil, true)
	}
	c.log().Info("ssconf endpoint changed", "old", oldEP, "new", newEP, "server_ip", c.ServerIP())
	return true
}

// ResolveServerIP extracts host from an ss:// key and resolves IPv4.
// Multi-part configs (containing "|") return an error.
func ResolveServerIP(accessKey string) (net.IP, error) {
	accessKey = strings.TrimSpace(accessKey)
	if accessKey == "" {
		return nil, fmt.Errorf("empty access key")
	}
	if strings.Contains(accessKey, "|") {
		return nil, fmt.Errorf("multi-part config not supported for server IP resolution")
	}
	// Take the last hop that looks like ss:// for composite keys without |.
	u, err := url.Parse(accessKey)
	if err != nil {
		return nil, fmt.Errorf("parse access key: %w", err)
	}
	if u.Scheme != "ss" && u.Scheme != "ssconf" {
		// Still try hostname if present.
		if u.Hostname() == "" {
			return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
		}
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("missing host in access key")
	}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4, nil
		}
		return ip, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("lookup %s: %w", host, err)
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4, nil
		}
	}
	if len(ips) > 0 {
		return ips[0], nil
	}
	return nil, fmt.Errorf("no addresses for %s", host)
}

// Ensure Client implements Dialer.
var _ Dialer = (*Client)(nil)
