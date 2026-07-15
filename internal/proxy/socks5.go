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

// SOCKS5 is a minimal SOCKS5 (no-auth, CONNECT only) server.
type SOCKS5 struct {
	ListenAddr string
	Dialer     Dialer
	Logger     *slog.Logger
	Timeout    time.Duration

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
	_ = conn.SetDeadline(time.Now().Add(s.Timeout))

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
	case 0x04: // IPv6
		if _, err := io.ReadFull(conn, buf[:16]); err != nil {
			return
		}
		host = net.IP(buf[:16]).String()
	default:
		_, _ = conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(buf[:2])
	target := net.JoinHostPort(host, strconv.Itoa(int(port)))

	dctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	remote, err := s.Dialer.DialContext(dctx, "tcp", target)
	if err != nil {
		s.Logger.Debug("SOCKS dial failed", "target", target, "err", err)
		_, _ = conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()

	// success
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	relay(conn, remote)
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
