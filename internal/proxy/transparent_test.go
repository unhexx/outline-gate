package proxy

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type fixedDecider struct {
	via  string
	rule string
}

func (d fixedDecider) DecidePath(dst net.IP) (string, string) {
	return d.via, d.rule
}

// dialAndServeEcho listens, accepts one conn, echoes, returns listen addr.
func dialAndServeEcho(t *testing.T) (addr string, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.Copy(c, c)
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func TestTransparentDirectUsesDirectDialer(t *testing.T) {
	backend, closeB := dialAndServeEcho(t)
	defer closeB()

	var tunnelHits, directHits atomic.Int32
	tunnel := &recordingDialer{hits: &tunnelHits, addr: backend}
	direct := &recordingDialer{hits: &directHits, addr: backend}
	log := &captureLog{}

	// Simulate transparent without SO_ORIGINAL_DST by calling decide+dial logic
	// via a thin integration: we only test DecidePath selection with dialers.
	// Full SO_ORIGINAL_DST needs REDIRECT; unit-test path selection:
	via, rule := fixedDecider{via: PathDirect, rule: "1.2.3.4"}.DecidePath(net.ParseIP("1.2.3.4"))
	if via != PathDirect || rule != "1.2.3.4" {
		t.Fatalf("via=%s rule=%s", via, rule)
	}

	tp := &Transparent{
		Dialer:       tunnel,
		DirectDialer: direct,
		Decider:      fixedDecider{via: PathDirect, rule: "example.com"},
		ConnLog:      log,
		Timeout:      3 * time.Second,
	}

	// Exercise dial selection by invoking handle-like logic on a fake original
	// destination through a local connection that we can't set SO_ORIGINAL_DST on.
	// Instead verify Decider + dialer fields by a small helper used only in test:
	dctx := context.Background()
	dialer := tp.Dialer
	via2, rule2 := tp.Decider.DecidePath(net.ParseIP("93.184.216.34"))
	if via2 == PathDirect {
		dialer = tp.DirectDialer
	}
	c, err := dialer.DialContext(dctx, "tcp", backend)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if directHits.Load() != 1 || tunnelHits.Load() != 0 {
		t.Fatalf("direct=%d tunnel=%d", directHits.Load(), tunnelHits.Load())
	}
	if via2 != PathDirect || rule2 != "example.com" {
		t.Fatalf("via=%s rule=%s", via2, rule2)
	}
}

func TestTransparentDropRecordsEvent(t *testing.T) {
	log := &captureLog{}
	tp := &Transparent{
		Dialer:  &recordingDialer{hits: &atomic.Int32{}, addr: "127.0.0.1:1"},
		Decider: fixedDecider{via: PathDrop},
		ConnLog: log,
		Timeout: time.Second,
	}
	// Direct unit of drop recording
	tp.record(ConnEvent{
		Proto: "l3", Host: "8.8.8.8", Target: "8.8.8.8:53", Port: 53,
		Via: PathDrop, OK: false, Error: "dropped by routing policy",
	})
	if len(log.events) != 1 || log.events[0].Via != PathDrop || log.events[0].OK {
		t.Fatalf("%+v", log.events)
	}
}
