package version

import "testing"

func TestString(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	Version = "0.4.0"
	if String() != "v0.4.0" {
		t.Fatalf("got %q", String())
	}
	Version = "v1.2.3"
	if String() != "v1.2.3" {
		t.Fatalf("got %q", String())
	}
	Version = ""
	if String() != "vdev" {
		t.Fatalf("got %q", String())
	}
}
