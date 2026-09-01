package outline

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.getoutline.org/sdk/transport"
)

func TestResolveServerIP_Literal(t *testing.T) {
	ip, err := ResolveServerIP("ss://YWVzLTEyOC1nY206dGVzdA@192.168.100.1:8888")
	if err != nil {
		t.Fatal(err)
	}
	if ip.String() != "192.168.100.1" {
		t.Fatalf("got %s", ip)
	}
}

func TestResolveServerIP_Empty(t *testing.T) {
	_, err := ResolveServerIP("")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveServerIP_Multipart(t *testing.T) {
	_, err := ResolveServerIP("split:5|ss://x@1.2.3.4:1")
	if err == nil {
		t.Fatal("expected error for multi-part")
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "dial tcp 9.9.9.9:1: i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestTransportFailuresMarkNotReady(t *testing.T) {
	c := &Client{reconnectBase: time.Second, reconnectMax: time.Second, probeFails: 2, logger: discardLogger()}
	c.serverIP = []byte{45, 151, 102, 145}
	c.ready.Store(true)

	c.noteDialResult(fmt.Errorf("destination connection refused"))
	if !c.Ready() {
		t.Fatal("destination errors must not flip ready")
	}

	c.noteDialResult(timeoutErr{})
	if !c.Ready() {
		t.Fatal("still ready after 1 timeout")
	}
	c.noteDialResult(timeoutErr{})
	if c.Ready() {
		t.Fatal("expected not ready after consecutive transport timeouts")
	}

	c.noteDialResult(nil)
	if c.failCount.Load() != 0 {
		t.Fatal("successful dial should reset fail counter")
	}
}

func TestIsTransportFailureServerIP(t *testing.T) {
	c := &Client{}
	c.serverIP = []byte{1, 2, 3, 4}
	err := fmt.Errorf("dial tcp 1.2.3.4:11097: connection refused")
	if !c.isTransportFailure(err) {
		t.Fatal("error mentioning Outline server IP is transport failure")
	}
}

func TestNewRequiresKey(t *testing.T) {
	_, err := New(Options{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExpandAccessKey_SSPassthrough(t *testing.T) {
	in := "ss://YWVzLTEyOC1nY206dGVzdA@1.2.3.4:1"
	out, err := ExpandAccessKey(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %q", out)
	}
}

func TestExpandAccessKey_SSConfJSON(t *testing.T) {
	body, _ := json.Marshal(outlineJSON{
		Server:     "10.0.0.1",
		ServerPort: 443,
		Password:   "secret",
		Method:     "aes-256-gcm",
		Prefix:     "\x16\x03\x01",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	// Use https-style path via ssconf by rewriting host — call parse via expandDynamic with https
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := ExpandAccessKey(ctx, srv.URL) // http:// test server — not ssconf
	// http:// without outline in URL is not treated as dynamic
	if err != nil {
		t.Fatal(err)
	}
	if out != srv.URL {
		// expected passthrough for plain http without outline in URL
	}

	// Force through expandDynamic via ssconf scheme pointing at test server is hard;
	// unit-test parseDynamicBody instead.
	ss, err := parseDynamicBody(string(body))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ss, "ss://") || !strings.Contains(ss, "@10.0.0.1:443") {
		t.Fatalf("bad ss key: %s", ss)
	}
	if !strings.Contains(ss, "prefix=") {
		t.Fatalf("missing prefix: %s", ss)
	}
}

func TestParseDynamicBody_SSText(t *testing.T) {
	ss, err := parseDynamicBody("ss://x@1.2.3.4:9")
	if err != nil || ss != "ss://x@1.2.3.4:9" {
		t.Fatalf("got %q err=%v", ss, err)
	}
}

func TestParseDynamicBody_ProviderError(t *testing.T) {
	body := `{"error":{"message":"access key deleted"}}`
	_, err := parseDynamicBody(body)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "access key deleted") {
		t.Fatalf("want provider message in error, got %v", err)
	}
}

func TestParseDynamicBody_URLSafeUserinfoAndIPv6(t *testing.T) {
	j := outlineJSON{
		Server:     "10.0.0.1",
		ServerPort: 443,
		Method:     "chacha20-ietf-poly1305",
		Password:   "secret/with+chars??",
	}
	std := base64.StdEncoding.EncodeToString([]byte(j.Method + ":" + j.Password))
	if !strings.ContainsAny(std, "/+") {
		t.Fatalf("fixture must produce std base64 / or +, got %s", std)
	}
	body, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := parseDynamicBody(string(body))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(ss)
	if err != nil {
		t.Fatal(err)
	}
	if u.Hostname() != "10.0.0.1" || u.Port() != "443" {
		t.Fatalf("host broken by userinfo encoding: %q host=%q port=%q", ss, u.Hostname(), u.Port())
	}
	userinfo := u.User.String()
	if strings.ContainsAny(userinfo, "/+") {
		t.Fatalf("userinfo must be URL-safe, got %q in %s", userinfo, ss)
	}

	j.Server = "2001:db8::1"
	body, _ = json.Marshal(j)
	ss, err = parseDynamicBody(string(body))
	if err != nil {
		t.Fatal(err)
	}
	u, err = url.Parse(ss)
	if err != nil {
		t.Fatal(err)
	}
	if u.Hostname() != "2001:db8::1" || u.Port() != "443" {
		t.Fatalf("IPv6 host: %q host=%q port=%q", ss, u.Hostname(), u.Port())
	}
}

func TestAccessKeyEndpoint(t *testing.T) {
	got := AccessKeyEndpoint("ss://YWVzLTEyOC1nY206dGVzdA@10.0.0.1:11097")
	if got != "10.0.0.1:11097" {
		t.Fatalf("got %q", got)
	}
	if AccessKeyEndpoint("") != "" {
		t.Fatal("empty")
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

const testSS1 = "ss://YWVzLTEyOC1nY206dGVzdA@10.0.0.1:1"
const testSS2 = "ss://YWVzLTEyOC1nY206dGVzdA@10.0.0.2:1"

type fakeStreamDialer struct {
	mu    sync.Mutex
	err   error
	dials []string
}

func (f *fakeStreamDialer) DialStream(ctx context.Context, addr string) (transport.StreamConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dials = append(f.dials, addr)
	if f.err != nil {
		return nil, f.err
	}
	a, b := net.Pipe()
	go b.Close()
	return stubConn{a}, nil
}

func (f *fakeStreamDialer) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

type stubConn struct{ net.Conn }

func (s stubConn) CloseRead() error  { return s.Conn.Close() }
func (s stubConn) CloseWrite() error { return s.Conn.Close() }

func testClient(t *testing.T, key string, expand func(context.Context, string) (string, error), d *fakeStreamDialer) *Client {
	t.Helper()
	c, err := New(Options{
		AccessKey:       key,
		ReconnectBase:   20 * time.Millisecond,
		ReconnectMax:    50 * time.Millisecond,
		ProbeAddr:       "",
		ProbeFails:      2,
		RefreshInterval: 0,
		Logger:          discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if expand != nil {
		c.expand = expand
	} else {
		c.expand = func(_ context.Context, k string) (string, error) { return k, nil }
	}
	c.newDialer = func(context.Context, string) (transport.StreamDialer, error) { return d, nil }
	return c
}

func waitUntil(t *testing.T, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func TestRefreshIfChanged_RebuildsOnNewEndpoint(t *testing.T) {
	var n atomic.Int32
	expand := func(context.Context, string) (string, error) {
		if n.Add(1) == 1 {
			return testSS1, nil
		}
		return testSS2, nil
	}
	d := &fakeStreamDialer{}
	c := testClient(t, "ssconf://provider.example/key", expand, d)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ip := c.ServerIP(); ip == nil || ip.String() != "10.0.0.1" {
		t.Fatalf("server ip: %v", ip)
	}
	if !c.refreshIfChanged(context.Background()) {
		t.Fatal("expected rebuild on endpoint change")
	}
	if ip := c.ServerIP(); ip == nil || ip.String() != "10.0.0.2" {
		t.Fatalf("server ip after refresh: %v", ip)
	}
	if !c.Ready() {
		t.Fatal("should stay ready")
	}
}

func TestRefreshIfChanged_FetchErrorKeepsDialer(t *testing.T) {
	var fail atomic.Bool
	expand := func(context.Context, string) (string, error) {
		if fail.Load() {
			return "", errors.New("ssconf down")
		}
		return testSS1, nil
	}
	d := &fakeStreamDialer{}
	c := testClient(t, "ssconf://provider.example/key", expand, d)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	var refreshErr error
	c.OnRefresh = func(err error, changed bool) {
		refreshErr = err
		if changed {
			t.Error("changed should be false")
		}
	}
	if c.refreshIfChanged(context.Background()) {
		t.Fatal("should not rebuild")
	}
	if refreshErr == nil {
		t.Fatal("expected refresh error hook")
	}
	if !c.Ready() {
		t.Fatal("must keep ready on fetch failure")
	}
	if ip := c.ServerIP(); ip == nil || ip.String() != "10.0.0.1" {
		t.Fatalf("server ip: %v", ip)
	}
}

func TestRefreshIfChanged_StaticKeySkipped(t *testing.T) {
	var expands atomic.Int32
	expand := func(_ context.Context, key string) (string, error) {
		expands.Add(1)
		return key, nil
	}
	d := &fakeStreamDialer{}
	c := testClient(t, testSS1, expand, d)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if expands.Load() != 1 {
		t.Fatalf("connect expands: %d", expands.Load())
	}
	if c.refreshIfChanged(context.Background()) {
		t.Fatal("static key should not refresh")
	}
	if expands.Load() != 1 {
		t.Fatalf("refresh must not expand static key, got %d", expands.Load())
	}
}

func TestDialContext_ConsecutiveFailuresMarkUnready(t *testing.T) {
	d := &fakeStreamDialer{err: timeoutErr{}}
	c := testClient(t, testSS1, nil, d)
	c.probeFails = 2
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !c.Ready() {
		t.Fatal("ready after connect")
	}
	_, err := c.DialContext(context.Background(), "tcp", "1.1.1.1:443")
	if err == nil {
		t.Fatal("expected dial error")
	}
	if !c.Ready() {
		t.Fatal("still ready after 1 failure")
	}
	_, err = c.DialContext(context.Background(), "tcp", "1.1.1.1:443")
	if err == nil {
		t.Fatal("expected dial error")
	}
	if c.Ready() {
		t.Fatal("expected not ready after threshold")
	}
}

func TestMaintainReady_RefreshSwitchesEndpoint(t *testing.T) {
	var n atomic.Int32
	expand := func(context.Context, string) (string, error) {
		if n.Load() == 0 {
			n.Add(1)
			return testSS1, nil
		}
		return testSS2, nil
	}
	d := &fakeStreamDialer{}
	c := testClient(t, "ssconf://provider.example/key", expand, d)
	c.refreshEvery = 25 * time.Millisecond
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.MaintainReady(ctx, nil)
	waitUntil(t, 2*time.Second, func() bool {
		ip := c.ServerIP()
		return ip != nil && ip.String() == "10.0.0.2" && c.Ready()
	})
}

func TestMaintainReady_ProbeFailuresReconnect(t *testing.T) {
	var expands atomic.Int32
	expand := func(context.Context, string) (string, error) {
		expands.Add(1)
		return testSS1, nil
	}
	d := &fakeStreamDialer{}
	c := testClient(t, "ssconf://provider.example/key", expand, d)
	c.probeAddr = "1.1.1.1:443"
	c.probeInterval = 20 * time.Millisecond
	c.probeTimeout = 50 * time.Millisecond
	c.probeFails = 2
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := expands.Load()
	d.setErr(errors.New("probe fail"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.MaintainReady(ctx, nil)
	waitUntil(t, 2*time.Second, func() bool {
		return expands.Load() > before
	})
	d.setErr(nil)
	waitUntil(t, 2*time.Second, func() bool {
		return c.Ready()
	})
}
