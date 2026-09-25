// Package dnsdoh is a local DNS stub that resolves via DNS-over-HTTPS
// (RFC 8484) over a provided TCP dialer (typically the Outline tunnel).
package dnsdoh

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

const maxDNSMessage = 4096

// Dialer is the TCP dialer used for the HTTPS connection to the DoH server.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Server answers DNS on UDP+TCP listen addresses and forwards queries as
// application/dns-message POSTs through Dialer.
type Server struct {
	Listen  []string
	URL     string
	Addr    string // ip:port of the DoH origin (bootstrap; no extra DNS)
	Dialer  Dialer
	Logger  *slog.Logger
	Timeout time.Duration

	mu     sync.Mutex
	httpCl *http.Client
}

// ListenAndServe binds UDP and TCP on each Listen address until ctx is done.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.Dialer == nil {
		return fmt.Errorf("dnsdoh: dialer is required")
	}
	if len(s.Listen) == 0 {
		return fmt.Errorf("dnsdoh: listen address is required")
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	if s.Timeout <= 0 {
		s.Timeout = 8 * time.Second
	}
	if err := s.initHTTP(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	bound := 0
	var skipErr error
	for _, addr := range s.Listen {
		addr := addr
		udp, err := net.ListenPacket("udp4", addr)
		if err != nil {
			if isUnassignedAddr(err) {
				s.Logger.Warn("dns-over-https skip listen (address not local)",
					"addr", addr, "proto", "udp", "err", err)
				skipErr = err
				continue
			}
			return fmt.Errorf("dnsdoh udp %s: %w", addr, err)
		}
		tcp, err := net.Listen("tcp4", addr)
		if err != nil {
			_ = udp.Close()
			if isUnassignedAddr(err) {
				s.Logger.Warn("dns-over-https skip listen (address not local)",
					"addr", addr, "proto", "tcp", "err", err)
				skipErr = err
				continue
			}
			return fmt.Errorf("dnsdoh tcp %s: %w", addr, err)
		}
		s.Logger.Info("dns-over-https stub listening",
			"addr", addr, "upstream", s.URL, "via", s.Addr)
		bound++

		wg.Add(3)
		go func() {
			defer wg.Done()
			<-ctx.Done()
			_ = udp.Close()
			_ = tcp.Close()
		}()
		go func() {
			defer wg.Done()
			s.serveUDP(ctx, udp)
		}()
		go func() {
			defer wg.Done()
			s.serveTCP(ctx, tcp)
		}()
	}
	if bound == 0 {
		if skipErr != nil {
			return fmt.Errorf("dnsdoh: no listen address available: %w", skipErr)
		}
		return fmt.Errorf("dnsdoh: no listen address available")
	}

	<-ctx.Done()
	wg.Wait()
	return nil
}

// isUnassignedAddr reports bind failures for IPs that are not assigned in
// this netns (typical: host docker0 172.17.0.1 listed in DNS_LISTEN while
// the process runs on a bridge network).
func isUnassignedAddr(err error) bool {
	return errors.Is(err, syscall.EADDRNOTAVAIL)
}

func (s *Server) initHTTP() error {
	u, err := url.Parse(s.URL)
	if err != nil {
		return fmt.Errorf("dnsdoh URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("dnsdoh URL must be https")
	}
	serverName := u.Hostname()
	addr := strings.TrimSpace(s.Addr)
	if addr == "" {
		addr = bootstrapAddr(u)
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return fmt.Errorf("dnsdoh bootstrap addr: %w", err)
	}
	s.Addr = addr

	d := s.Dialer
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return d.DialContext(ctx, "tcp", addr)
		},
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: serverName,
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: s.Timeout,
		DisableCompression:    true,
	}
	s.httpCl = &http.Client{
		Transport: tr,
		Timeout:   s.Timeout,
	}
	return nil
}

func bootstrapAddr(u *url.URL) string {
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "443"
	}
	if ip := net.ParseIP(host); ip != nil {
		return net.JoinHostPort(host, port)
	}
	switch strings.ToLower(host) {
	case "cloudflare-dns.com", "one.one.one.one":
		return net.JoinHostPort("1.1.1.1", port)
	case "dns.google":
		return net.JoinHostPort("8.8.8.8", port)
	case "dns.quad9.net", "dns11.quad9.net":
		return net.JoinHostPort("9.9.9.9", port)
	default:
		return net.JoinHostPort(host, port)
	}
}

func (s *Server) serveUDP(ctx context.Context, pc net.PacketConn) {
	buf := make([]byte, maxDNSMessage)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.Logger.Debug("dnsdoh udp read", "err", err)
			continue
		}
		q := append([]byte(nil), buf[:n]...)
		go func() {
			resp, err := s.resolve(ctx, q)
			if err != nil {
				s.Logger.Debug("dnsdoh query failed", "err", err, "peer", from.String())
				resp = servfail(q)
			}
			if _, werr := pc.WriteTo(resp, from); werr != nil {
				s.Logger.Debug("dnsdoh udp write", "err", werr)
			}
		}()
	}
}

func (s *Server) serveTCP(ctx context.Context, ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.Logger.Debug("dnsdoh tcp accept", "err", err)
			continue
		}
		go s.handleTCP(ctx, c)
	}
}

func (s *Server) handleTCP(ctx context.Context, c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(s.Timeout))
	var hdr [2]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return
	}
	n := int(binary.BigEndian.Uint16(hdr[:]))
	if n <= 0 || n > maxDNSMessage {
		return
	}
	q := make([]byte, n)
	if _, err := io.ReadFull(c, q); err != nil {
		return
	}
	resp, err := s.resolve(ctx, q)
	if err != nil {
		resp = servfail(q)
	}
	var outHdr [2]byte
	binary.BigEndian.PutUint16(outHdr[:], uint16(len(resp)))
	_, _ = c.Write(outHdr[:])
	_, _ = c.Write(resp)
}

func (s *Server) resolve(ctx context.Context, q []byte) ([]byte, error) {
	if len(q) < 12 {
		return nil, fmt.Errorf("dns query too short")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(q))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	req.Host = req.URL.Host

	s.mu.Lock()
	cl := s.httpCl
	s.mu.Unlock()
	res, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxDNSMessage))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("doh status %d", res.StatusCode)
	}
	if len(body) < 12 {
		return nil, fmt.Errorf("doh response too short")
	}
	return body, nil
}

// servfail copies the query ID and sets QR+RCODE=2 if the header is usable.
func servfail(q []byte) []byte {
	out := make([]byte, 12)
	if len(q) >= 2 {
		copy(out[:2], q[:2])
	}
	// QR=1, OPCODE=0, AA=0, TC=0, RD copied if present, RA=0, RCODE=SERVFAIL
	rd := byte(0)
	if len(q) >= 3 {
		rd = q[2] & 0x01
	}
	out[2] = 0x80 | rd
	out[3] = 0x02
	return out
}
