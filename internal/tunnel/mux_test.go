package tunnel

import (
	"context"
	"net"
	"testing"
)

type recDialer struct {
	name string
	n    *int
}

func (d recDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	if d.n != nil {
		*d.n++
	}
	return nil, context.Canceled
}

func TestMuxSwitch(t *testing.T) {
	var o, a int
	readyO, readyA := true, false
	m := NewMux(
		recDialer{name: "o", n: &o},
		recDialer{name: "a", n: &a},
		func() bool { return readyO },
		func() bool { return readyA },
		func() net.IP { return net.ParseIP("1.1.1.1") },
		func() net.IP { return net.ParseIP("9.9.9.9") },
	)
	if m.Backend() != BackendOutline || !m.Ready() || m.ServerIP().String() != "1.1.1.1" {
		t.Fatalf("outline start: backend=%s ready=%v ip=%v", m.Backend(), m.Ready(), m.ServerIP())
	}
	_, _ = m.DialContext(context.Background(), "tcp", "example.com:443")
	if o != 1 || a != 0 {
		t.Fatalf("outline dials o=%d a=%d", o, a)
	}
	if err := m.SetBackend(BackendAmnezia); err != nil {
		t.Fatal(err)
	}
	readyA = true
	if m.Backend() != BackendAmnezia || !m.Ready() || m.ServerIP().String() != "9.9.9.9" {
		t.Fatalf("amnezia: backend=%s ready=%v ip=%v", m.Backend(), m.Ready(), m.ServerIP())
	}
	_, _ = m.DialContext(context.Background(), "tcp", "example.com:443")
	if o != 1 || a != 1 {
		t.Fatalf("amnezia dials o=%d a=%d", o, a)
	}
	if Normalize("AWG") != BackendAmnezia || Normalize("") != BackendOutline {
		t.Fatal("normalize")
	}
}
