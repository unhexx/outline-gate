package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestHTTPConnect(t *testing.T) {
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
	_, _ = fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", bln.Addr().String(), bln.Addr().String())
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(br, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != "ping" {
		t.Fatalf("echo %q", got)
	}
}
