// Package outline wraps outline-sdk configurl dialers and server endpoint resolution.
package outline

import (
	"context"
	"fmt"
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

	mu       sync.RWMutex
	dialer   transport.StreamDialer
	serverIP net.IP
	ready    atomic.Bool

	providers *configurl.ProviderContainer
}

// Options configures Client construction.
type Options struct {
	AccessKey     string
	ReconnectBase time.Duration
	ReconnectMax  time.Duration
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
	ip, err := ResolveServerIP(opts.AccessKey)
	if err != nil {
		// Non-fatal for multi-part configs: leave nil and continue.
		ip = nil
	}
	return &Client{
		accessKey:     opts.AccessKey,
		reconnectBase: opts.ReconnectBase,
		reconnectMax:  opts.ReconnectMax,
		serverIP:      ip,
		providers:     configurl.NewDefaultProviders(),
	}, nil
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

// Ready reports whether a dialer is available.
func (c *Client) Ready() bool {
	return c.ready.Load()
}

// Connect builds the StreamDialer. Safe to call multiple times (replaces dialer).
// ssconf:// keys are fetched and expanded to ss:// on each Connect (fresh static key).
func (c *Client) Connect(ctx context.Context) error {
	key, err := ExpandAccessKey(ctx, c.accessKey)
	if err != nil {
		c.ready.Store(false)
		return fmt.Errorf("outline expand key: %w", err)
	}
	d, err := c.providers.NewStreamDialer(ctx, key)
	if err != nil {
		c.ready.Store(false)
		return fmt.Errorf("outline dialer: %w", err)
	}
	// Best-effort re-resolve server IP from expanded key.
	if ip, err := ResolveServerIP(key); err == nil {
		c.mu.Lock()
		c.serverIP = ip
		c.mu.Unlock()
	}
	c.mu.Lock()
	c.dialer = d
	c.mu.Unlock()
	c.ready.Store(true)
	return nil
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
	if err != nil {
		return nil, err
	}
	return sc, nil
}

// MaintainReady periodically ensures the dialer exists. On failure, backs off and retries.
// Blocks until ctx is cancelled.
func (c *Client) MaintainReady(ctx context.Context, onChange func(ready bool)) {
	delay := c.reconnectBase
	for {
		if ctx.Err() != nil {
			return
		}
		if !c.Ready() {
			if err := c.Connect(ctx); err != nil {
				if onChange != nil {
					onChange(false)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
				delay *= 2
				if delay > c.reconnectMax {
					delay = c.reconnectMax
				}
				continue
			}
			delay = c.reconnectBase
			if onChange != nil {
				onChange(true)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			// Keepalive probe: lightweight status only (no forced reconnect if ready).
		}
	}
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
