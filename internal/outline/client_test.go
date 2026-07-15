package outline

import (
	"testing"
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

func TestNewRequiresKey(t *testing.T) {
	_, err := New(Options{})
	if err == nil {
		t.Fatal("expected error")
	}
}
