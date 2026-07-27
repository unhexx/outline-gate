package bypass

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Store is a thread-safe, file-backed set of user bypass rules.
type Store struct {
	path string
	mu   sync.RWMutex
	rules []Rule
}

// NewStore creates a store. path may be empty (memory-only).
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Load reads rules from path. Missing file yields empty list.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		s.rules = nil
		return nil
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.rules = nil
			return nil
		}
		return err
	}
	rules, err := ParseRulesFile(string(b))
	if err != nil {
		return err
	}
	s.rules = rules
	return nil
}

// ParseRulesFile parses a full rules file (comments and blanks allowed).
func ParseRulesFile(content string) ([]Rule, error) {
	var rules []Rule
	seen := make(map[string]struct{})
	for i, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r, err := ParseRule(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if _, ok := seen[r.Raw]; ok {
			continue
		}
		seen[r.Raw] = struct{}{}
		rules = append(rules, r)
	}
	return rules, nil
}

// Rules returns a copy of current rules.
func (s *Store) Rules() []Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Rule, len(s.rules))
	copy(out, s.rules)
	return out
}

// Set replaces all rules and persists.
func (s *Store) Set(rules []Rule) error {
	// normalize / dedupe
	seen := make(map[string]struct{}, len(rules))
	clean := make([]Rule, 0, len(rules))
	for _, r := range rules {
		parsed, err := ParseRule(r.Raw)
		if err != nil {
			return err
		}
		if _, ok := seen[parsed.Raw]; ok {
			continue
		}
		seen[parsed.Raw] = struct{}{}
		clean = append(clean, parsed)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.persistLocked(clean); err != nil {
		return err
	}
	s.rules = clean
	return nil
}

// Add appends a rule if not already present.
func (s *Store) Add(raw string) (Rule, error) {
	r, err := ParseRule(raw)
	if err != nil {
		return Rule{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.rules {
		if existing.Raw == r.Raw {
			return existing, nil
		}
	}
	next := append(append([]Rule(nil), s.rules...), r)
	if err := s.persistLocked(next); err != nil {
		return Rule{}, err
	}
	s.rules = next
	return r, nil
}

// Remove deletes a rule by raw string (parsed for canonical form).
func (s *Store) Remove(raw string) (bool, error) {
	r, err := ParseRule(raw)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make([]Rule, 0, len(s.rules))
	found := false
	for _, existing := range s.rules {
		if existing.Raw == r.Raw {
			found = true
			continue
		}
		next = append(next, existing)
	}
	if !found {
		return false, nil
	}
	if err := s.persistLocked(next); err != nil {
		return false, err
	}
	s.rules = next
	return true, nil
}

func (s *Store) persistLocked(rules []Rule) error {
	if s.path == "" {
		return nil
	}
	var b strings.Builder
	b.WriteString("# outline-gate user bypass rules (managed by UI/API)\n")
	for _, r := range rules {
		b.WriteString(r.Raw)
		b.WriteByte('\n')
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
