package dnsdoh

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

type staticDialer struct {
	addr string
}

func (d staticDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	var nd net.Dialer
	return nd.DialContext(ctx, network, d.addr)
}

func TestResolveRFC8484(t *testing.T) {
	const echo = "abcdefghijklmnopqrstuvwx" // 12+ bytes, fake DNS
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/dns-message" {
			t.Errorf("ct %s", r.Header.Get("Content-Type"))
		}
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(b)
	}))
	defer ts.Close()

	hostPort := ts.Listener.Addr().String()
	s := &Server{
		Listen:  []string{"127.0.0.1:0"},
		URL:     "https://cloudflare-dns.com/dns-query",
		Addr:    hostPort,
		Dialer:  staticDialer{addr: hostPort},
		Timeout: 2 * time.Second,
	}
	if err := s.initHTTP(); err != nil {
		t.Fatal(err)
	}
	s.httpCl.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	got, err := s.resolve(context.Background(), []byte(echo))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != echo {
		t.Fatalf("got %q", got)
	}
}

func TestServfailPreservesID(t *testing.T) {
	q := []byte{0xab, 0xcd, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0}
	r := servfail(q)
	if r[0] != 0xab || r[1] != 0xcd {
		t.Fatalf("id %x", r[:2])
	}
	if r[3]&0x0f != 2 {
		t.Fatalf("rcode %d", r[3]&0x0f)
	}
}

func TestBootstrapAddr(t *testing.T) {
	u, err := url.Parse("https://cloudflare-dns.com/dns-query")
	if err != nil {
		t.Fatal(err)
	}
	if got := bootstrapAddr(u); got != "1.1.1.1:443" {
		t.Fatalf("got %s", got)
	}
}

func TestListenAndServeSkipsUnassignedAddr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Server{
		Listen: []string{"203.0.113.1:18153", "127.0.0.1:0"},
		URL:    "https://cloudflare-dns.com/dns-query",
		Addr:   "1.1.1.1:443",
		Dialer: staticDialer{addr: "127.0.0.1:9"},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	errCh := make(chan error, 1)
	go func() { errCh <- s.ListenAndServe(ctx) }()
	select {
	case err := <-errCh:
		t.Fatalf("ListenAndServe returned early: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for shutdown")
	}
}

func TestListenAndServeAllUnassignedFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s := &Server{
		Listen: []string{"203.0.113.1:18153"},
		URL:    "https://cloudflare-dns.com/dns-query",
		Addr:   "1.1.1.1:443",
		Dialer: staticDialer{addr: "127.0.0.1:9"},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if err := s.ListenAndServe(ctx); err == nil {
		t.Fatal("expected error when no address can be bound")
	}
}
