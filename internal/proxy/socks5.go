// Package proxy provides local SOCKS5 and transparent TCP proxy servers
// that forward via an Outline dialer.
package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"
)

// Dialer is the subset of net dialing used by proxies.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// BypassChecker reports whether a destination host (name or IP literal)
// should skip the tunnel and use a direct dial.
type BypassChecker interface {
	ShouldBypassHost(host string) bool
}

// BypassMatcher is an optional richer bypass interface that returns the
// matched rule string for the connection log.
type BypassMatcher interface {
	MatchBypass(host string) (bypass bool, rule string)
}

// ConnRecorder records connection routing events for the management UI.
type ConnRecorder interface {
	RecordConnect(e ConnEvent)
}

// ConnEvent is a lightweight proxy-side connection record.
type ConnEvent struct {
	Proto      string // "socks" | "l3"
	ClientIP   string
	Target     string
	Host       string
	Port       int
	Via        string // "tunnel" | "direct"
	Rule       string
	OK         bool
	Error      string
	DurationMs int64
}

// SOCKS5 is a minimal SOCKS5 (no-auth, CONNECT only) server.
type SOCKS5 struct {
	ListenAddr string
	// Dialer is used for tunnelled connections (Outline).
	Dialer Dialer
	// DirectDialer is used when Bypass matches; defaults to net.Dialer.
	DirectDialer Dialer
	// Bypass optionally selects direct path for excluded hosts/IPs.
	Bypass BypassChecker
	// ConnLog optionally records each CONNECT for the live UI log.
	ConnLog ConnRecorder
	Logger  *slog.Logger
	Timeout time.Duration

	ln net.Listener
}

// ListenAndServe starts serving until the listener is closed or ctx cancelled.
func (s *SOCKS5) ListenAndServe(ctx context.Context) error {
	if s.Dialer == nil {
		return fmt.Errorf("socks5: dialer is required")
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	if s.Timeout <= 0 {
		s.Timeout = 30 * time.Second
	}
	ln, err := net.Listen("tcp", s.ListenAddr)
	if err != nil {
		return err
	}
	s.ln = ln
	s.Logger.Info("SOCKS5 listening", "addr", ln.Addr().String())

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			wg.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handle(ctx, conn)
		}()
	}
}

// Close stops the listener.
func (s *SOCKS5) Close() error {
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

func (s *SOCKS5) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	log := s.Logger
	if log == nil {
		log = slog.Default()
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))

	// greeting
	buf := make([]byte, 258)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	if buf[0] != 0x05 {
		return
	}
	nmethods := int(buf[1])
	if _, err := io.ReadFull(conn, buf[:nmethods]); err != nil {
		return
	}
	// no auth
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// request
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return
	}
	if buf[0] != 0x05 || buf[1] != 0x01 { // CONNECT
		_, _ = conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	var host string
	switch buf[3] {
	case 0x01: // IPv4
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return
		}
		host = net.IP(buf[:4]).String()
	case 0x03: // domain
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return
		}
		l := int(buf[0])
		if _, err := io.ReadFull(conn, buf[:l]); err != nil {
			return
		}
		host = string(buf[:l])
	case 0x04: // IPv6 — not supported in v1 (IPv4-only)
		if _, err := io.ReadFull(conn, buf[:16]); err != nil {
			return
		}
		log.Debug("SOCKS IPv6 rejected (v1 is IPv4-only)")
		// 0x08 = Address type not supported
		_, _ = conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	default:
		_, _ = conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(buf[:2])
	portInt := int(port)
	target := net.JoinHostPort(host, strconv.Itoa(portInt))
	clientIP := clientIPOf(conn)

	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dialer := s.Dialer
	via := "tunnel"
	rule := ""
	if s.Bypass != nil {
		if bm, ok := s.Bypass.(BypassMatcher); ok {
			if bypass, r := bm.MatchBypass(host); bypass {
				via = "direct"
				rule = r
			}
		} else if s.Bypass.ShouldBypassHost(host) {
			via = "direct"
		}
		if via == "direct" {
			if s.DirectDialer != nil {
				dialer = s.DirectDialer
			} else {
				dialer = &net.Dialer{}
			}
		}
	}
	start := time.Now()
	remote, err := dialer.DialContext(dctx, "tcp", target)
	dur := time.Since(start).Milliseconds()
	if err != nil {
		log.Debug("SOCKS dial failed", "target", target, "via", via, "err", err)
		s.record(ConnEvent{
			Proto: "socks", ClientIP: clientIP, Target: target, Host: host, Port: portInt,
			Via: via, Rule: rule, OK: false, Error: err.Error(), DurationMs: dur,
		})
		_, _ = conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	log.Debug("SOCKS connect", "target", target, "via", via)
	s.record(ConnEvent{
		Proto: "socks", ClientIP: clientIP, Target: target, Host: host, Port: portInt,
		Via: via, Rule: rule, OK: true, DurationMs: dur,
	})
	defer remote.Close()

	// success
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	relay(conn, remote)
}

func (s *SOCKS5) record(e ConnEvent) {
	if s.ConnLog != nil {
		s.ConnLog.RecordConnect(e)
	}
}

func clientIPOf(conn net.Conn) string {
	if conn == nil || conn.RemoteAddr() == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

func relay(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	copyFn := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		type closeWriter interface{ CloseWrite() error }
		if cw, ok := dst.(closeWriter); ok {
			_ = cw.CloseWrite()
		}
	}
	go copyFn(a, b)
	go copyFn(b, a)
	wg.Wait()
}
