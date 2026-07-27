package bypass

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreCRUD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bypass.rules.txt")
	s := NewStore(path)

	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if len(s.Rules()) != 0 {
		t.Fatal("expected empty")
	}

	r, err := s.Add("Example.COM")
	if err != nil {
		t.Fatal(err)
	}
	if r.Raw != "example.com" {
		t.Fatalf("raw=%s", r.Raw)
	}
	if _, err := s.Add("10.0.0.0/8"); err != nil {
		t.Fatal(err)
	}
	// duplicate
	if _, err := s.Add("example.com"); err != nil {
		t.Fatal(err)
	}
	if len(s.Rules()) != 2 {
		t.Fatalf("rules=%d", len(s.Rules()))
	}

	ok, err := s.Remove("EXAMPLE.COM")
	if err != nil || !ok {
		t.Fatalf("remove: ok=%v err=%v", ok, err)
	}
	if len(s.Rules()) != 1 {
		t.Fatal("after remove")
	}

	// reload
	s2 := NewStore(path)
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	if len(s2.Rules()) != 1 || s2.Rules()[0].Raw != "10.0.0.0/8" {
		t.Fatalf("reload: %+v", s2.Rules())
	}

	b, _ := os.ReadFile(path)
	if len(b) == 0 {
		t.Fatal("file empty")
	}
}

func TestParseRulesFile(t *testing.T) {
	content := `
# comment
8.8.8.8

*.example.com
example.com
`
	rules, err := ParseRulesFile(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 3 {
		t.Fatalf("got %d", len(rules))
	}
}
