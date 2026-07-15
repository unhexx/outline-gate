package outline

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// outlineJSON is the JSON body served by ssconf:// endpoints (Outline dynamic keys).
type outlineJSON struct {
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
	Password   string `json:"password"`
	Method     string `json:"method"`
	Prefix     string `json:"prefix"`
}

// ExpandAccessKey resolves ssconf:// (and https:// dynamic key) URLs into a
// static ss:// key that outline-sdk configurl can consume. Plain ss:// keys
// are returned unchanged (fragment stripped for the dialer only when needed
// by the caller — Expand keeps the key usable by configurl).
func ExpandAccessKey(ctx context.Context, accessKey string) (string, error) {
	accessKey = strings.TrimSpace(accessKey)
	if accessKey == "" {
		return "", fmt.Errorf("empty access key")
	}
	// Multi-part pipe stacks: only expand leading ssconf hop if present.
	if strings.Contains(accessKey, "|") {
		parts := strings.Split(accessKey, "|")
		for i, p := range parts {
			p = strings.TrimSpace(p)
			if isDynamicKey(p) {
				exp, err := expandDynamic(ctx, p)
				if err != nil {
					return "", err
				}
				parts[i] = exp
			}
		}
		return strings.Join(parts, "|"), nil
	}
	if isDynamicKey(accessKey) {
		return expandDynamic(ctx, accessKey)
	}
	return accessKey, nil
}

func isDynamicKey(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "ssconf://")
}

func expandDynamic(ctx context.Context, key string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(key))
	if err != nil {
		return "", fmt.Errorf("parse dynamic key: %w", err)
	}
	if u.Scheme != "ssconf" {
		return "", fmt.Errorf("unsupported dynamic scheme %q", u.Scheme)
	}
	// ssconf://host/path → https://host/path (fragment is display name only)
	u.Scheme = "https"
	u.Fragment = ""
	fetchURL := u.String()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "outline-gate/1.0")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ssconf fetch: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("ssconf read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ssconf HTTP %d", resp.StatusCode)
	}
	return parseDynamicBody(strings.TrimSpace(string(body)))
}

func parseDynamicBody(body string) (string, error) {
	if body == "" {
		return "", fmt.Errorf("ssconf empty body")
	}
	// Already an ss:// key (some providers return the static key as text).
	if strings.HasPrefix(body, "ss://") {
		return strings.TrimSpace(body), nil
	}
	// Outline Manager / outlineaccesskey JSON.
	var j outlineJSON
	if err := json.Unmarshal([]byte(body), &j); err != nil {
		return "", fmt.Errorf("ssconf json: %w", err)
	}
	if j.Server == "" || j.ServerPort <= 0 || j.Method == "" || j.Password == "" {
		return "", fmt.Errorf("ssconf json missing server/port/method/password")
	}
	userinfo := base64.StdEncoding.EncodeToString([]byte(j.Method + ":" + j.Password))
	ss := fmt.Sprintf("ss://%s@%s:%d", userinfo, j.Server, j.ServerPort)
	if j.Prefix != "" {
		ss += "?prefix=" + url.QueryEscape(j.Prefix)
	}
	return ss, nil
}
