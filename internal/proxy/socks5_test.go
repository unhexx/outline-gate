package proxy

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

type echoDialer struct{}

func (echoDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	// Dial a local echo server started by the test via address.
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

func TestSOCKS5Connect(t *testing.T) {
	// backend echo
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

	s := &SOCKS5{
		ListenAddr: "127.0.0.1:0",
		Dialer:     echoDialer{},
		Timeout:    5 * time.Second,
	}
	// custom listen
	ln, err := net.Listen("tcp", s.ListenAddr)
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	s.ListenAddr = ln.Addr().String()
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

	// raw SOCKS5 client
	c, err := net.Dial("tcp", s.ListenAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// greeting
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(c, resp); err != nil {
		t.Fatal(err)
	}
	if resp[0] != 0x05 || resp[1] != 0x00 {
		t.Fatalf("auth resp %v", resp)
	}
	// CONNECT to backend
	host, portStr, _ := net.SplitHostPort(bln.Addr().String())
	ip := net.ParseIP(host).To4()
	var port int
	_, _ = net.ParseIP(host), port
	var p uint16
	_, err = net.ResolveTCPAddr("tcp", bln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	ta, _ := net.ResolveTCPAddr("tcp", bln.Addr().String())
	p = uint16(ta.Port)
	req := []byte{0x05, 0x01, 0x00, 0x01, ip[0], ip[1], ip[2], ip[3], byte(p >> 8), byte(p)}
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	rep := make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil {
		t.Fatal(err)
	}
	if rep[1] != 0x00 {
		t.Fatalf("connect failed: %v", rep)
	}
	msg := []byte("hello")
	if _, err := c.Write(msg); err != nil {
		t.Fatal(err)
	}
	out := make([]byte, 5)
	if _, err := io.ReadFull(c, out); err != nil {
		t.Fatal(err)
	}
	if string(out) != "hello" {
		t.Fatalf("echo: %q", out)
	}
	_ = portStr
}
