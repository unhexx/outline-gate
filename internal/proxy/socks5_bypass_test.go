package proxy

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/unhexx/outline-gate/internal/bypass"
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

type captureLog struct {
	events []ConnEvent
}

func (c *captureLog) RecordConnect(e ConnEvent) {
	c.events = append(c.events, e)
}

type detailBypass struct{}

func (detailBypass) ShouldBypassHost(host string) bool {
	ok, _ := detailBypass{}.MatchBypass(host)
	return ok
}

func (detailBypass) MatchBypass(host string) (bool, string) {
	if host == "bank.example" {
		return true, "bank.example"
	}
	return false, ""
}

func TestSOCKS5RecordsConnLogWithRule(t *testing.T) {
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

	var hits atomic.Int32
	tunnel := &recordingDialer{hits: &hits, addr: backend}
	log := &captureLog{}

	s := &SOCKS5{
		ListenAddr:   "127.0.0.1:0",
		Dialer:       tunnel,
		DirectDialer: tunnel,
		Bypass:       detailBypass{},
		ConnLog:      log,
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
	host := "bank.example"
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, host...)
	req = append(req, 0x00, 0x50)
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
	if len(log.events) != 1 {
		t.Fatalf("events=%+v", log.events)
	}
	e := log.events[0]
	if e.Via != "direct" || e.Rule != "bank.example" || !e.OK || e.Proto != "socks" {
		t.Fatalf("%+v", e)
	}
}

func TestSOCKS5BlockRejectsAndLogs(t *testing.T) {
	var tunnelHits atomic.Int32
	tunnel := &recordingDialer{hits: &tunnelHits, addr: "127.0.0.1:9"}
	log := &captureLog{}
	s := &SOCKS5{
		ListenAddr: "127.0.0.1:0",
		Dialer:     tunnel,
		Block:      hostBypass{"ads.example": true},
		ConnLog:    log,
		Timeout:    5 * time.Second,
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
	host := "ads.example"
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, host...)
	req = append(req, 0x00, 0x50)
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(c, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != 0x02 {
		t.Fatalf("socks status %d want 0x02 (not allowed)", reply[1])
	}
	if tunnelHits.Load() != 0 {
		t.Fatalf("blocked dest must not dial, hits=%d", tunnelHits.Load())
	}
	if len(log.events) != 1 {
		t.Fatalf("events=%+v", log.events)
	}
	e := log.events[0]
	if e.Via != "drop" || e.OK || e.Error != "blocked" || e.Proto != "socks" || e.Host != "ads.example" {
		t.Fatalf("%+v", e)
	}
}

func TestSOCKS5BlockWinsOverBypass(t *testing.T) {
	var tunnelHits, directHits atomic.Int32
	tunnel := &recordingDialer{hits: &tunnelHits, addr: "127.0.0.1:9"}
	direct := &recordingDialer{hits: &directHits, addr: "127.0.0.1:9"}
	log := &captureLog{}
	s := &SOCKS5{
		ListenAddr:   "127.0.0.1:0",
		Dialer:       tunnel,
		DirectDialer: direct,
		Bypass:       hostBypass{"ads.example": true},
		Block:        hostBypass{"ads.example": true},
		ConnLog:      log,
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
	host := "ads.example"
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, host...)
	req = append(req, 0x00, 0x50)
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(c, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != 0x02 {
		t.Fatalf("status %d", reply[1])
	}
	if tunnelHits.Load() != 0 || directHits.Load() != 0 {
		t.Fatalf("block must win over bypass")
	}
	if len(log.events) != 1 || log.events[0].Via != "drop" {
		t.Fatalf("%+v", log.events)
	}
}

func TestSOCKS5StaticCIDRBypassUsesDirect(t *testing.T) {
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

	static := []net.IPNet{
		{IP: net.ParseIP("192.168.0.0").To4(), Mask: net.CIDRMask(16, 32)},
	}
	mgr := bypass.NewManager(bypass.Options{StaticBypass: static})

	var tunnelHits, directHits atomic.Int32
	tunnel := &recordingDialer{name: "tunnel", hits: &tunnelHits, addr: backend}
	direct := &recordingDialer{name: "direct", hits: &directHits, addr: backend}
	s := &SOCKS5{
		ListenAddr:   "127.0.0.1:0",
		Dialer:       tunnel,
		DirectDialer: direct,
		Bypass:       mgr,
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
	ip := net.ParseIP("192.168.1.50").To4()
	req := []byte{0x05, 0x01, 0x00, 0x01, ip[0], ip[1], ip[2], ip[3], 0x00, 0x50}
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
	if directHits.Load() != 1 || tunnelHits.Load() != 0 {
		t.Fatalf("direct=%d tunnel=%d (RFC1918 must be direct)", directHits.Load(), tunnelHits.Load())
	}
}
