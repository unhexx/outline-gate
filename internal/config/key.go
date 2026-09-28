package config

import (
	"fmt"
	"strings"
)

// ValidateAccessKey rejects empty, redacted, and status-line copies.
// A key copied from the UI ("ss://***@host · ready: …") must never be stored:
// it survives container restarts and the tunnel cannot come back.
func ValidateAccessKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("access key is empty")
	}
	if strings.ContainsAny(key, " \t\r\n") {
		return fmt.Errorf("access key must be a single token without spaces")
	}
	lower := strings.ToLower(key)
	if strings.Contains(key, "***") || strings.Contains(lower, "%2a%2a%2a") ||
		strings.Contains(lower, "ready:") || strings.Contains(lower, "persist:") {
		return fmt.Errorf("access key looks redacted or copied from the status line")
	}
	if !strings.HasPrefix(key, "ss://") && !strings.HasPrefix(key, "ssconf://") {
		return fmt.Errorf("access key must start with ss:// or ssconf://")
	}
	return nil
}
