package proxy

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type recordingDialer struct {
	name string
	hits *atomic.Int32
	addr string
}

func (d *recordingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.hits.Add(1)
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, d.addr)
}

type hostBypass map[string]bool

func (h hostBypass) ShouldBypassHost(host string) bool {
	return h[host]
}

func TestSOCKS5BypassUsesDirectDialer(t *testing.T) {
	bln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer bln.Close()
	go func() {
		c, err := bln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.Copy(c, c)
	}()
	backend := bln.Addr().String()

	var tunnelHits, directHits atomic.Int32
	tunnel := &recordingDialer{name: "tunnel", hits: &tunnelHits, addr: backend}
	direct := &recordingDialer{name: "direct", hits: &directHits, addr: backend}

	s := &SOCKS5{
		ListenAddr:   "127.0.0.1:0",
		Dialer:       tunnel,
		DirectDialer: direct,
		Bypass:       hostBypass{"example.com": true},
		Timeout:      5 * time.Second,
	}
	ln, err := net.Listen("tcp", s.ListenAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	s.ln = ln
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(ctx, conn)
		}
	}()

	// CONNECT domain example.com -> should use direct
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(c, resp); err != nil {
		t.Fatal(err)
	}
	host := "example.com"
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, host...)
	req = append(req, 0x00, 0x50) // port 80
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(c, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != 0x00 {
		t.Fatalf("socks status %d", reply[1])
	}
	if directHits.Load() != 1 {
		t.Fatalf("direct hits=%d tunnel=%d", directHits.Load(), tunnelHits.Load())
	}
	if tunnelHits.Load() != 0 {
		t.Fatalf("tunnel should not be used")
	}
}
