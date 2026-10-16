// Package amnezia brings up an AmneziaWG tunnel as an alternate SOCKS/L3 backend.
package amnezia

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

// Config is a parsed AmneziaWG .conf (INI) plus optional vpn:// metadata.
type Config struct {
	Address                string
	DNS                    []string
	PrivateKey             string // base64
	PeerPublicKey          string
	PresharedKey           string
	Endpoint               string // host:port
	AllowedIPs             []string
	PersistentKeepalive    string // "25" or "25-35"
	Jc, Jmin, Jmax         string
	S1, S2, S3, S4         string
	H1, H2, H3, H4         string
	HeaderProtectionKey    string // base64
	RekeyAfterTime         string
	RekeyTimeout           string
	RejectAfterTime        string
	KeepaliveTimeout       string
	MaxHandshakeAttempts   string
	ContentPaddingAddition string
	Name                   string
	ServiceType            string
	ServiceProtocol        string
}

// EndpointIP returns the IPv4 of Endpoint when the host is an IP literal.
func (c Config) EndpointIP() net.IP {
	host, _, err := net.SplitHostPort(strings.TrimSpace(c.Endpoint))
	if err != nil {
		host = strings.TrimSpace(c.Endpoint)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

// InterfaceAddr is the first Interface Address (CIDR or bare IP).
func (c Config) InterfaceAddr() (net.IP, *net.IPNet, error) {
	s := strings.TrimSpace(c.Address)
	if s == "" {
		return nil, nil, fmt.Errorf("amnezia config: missing Interface Address")
	}
	if strings.Contains(s, "/") {
		ip, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, nil, fmt.Errorf("amnezia Address: %w", err)
		}
		return ip, n, nil
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, nil, fmt.Errorf("amnezia Address: invalid IP %q", s)
	}
	if v4 := ip.To4(); v4 != nil {
		return v4, &net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}, nil
	}
	return ip, &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}, nil
}

// LoadFile reads an AmneziaWG INI config from path.
func LoadFile(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return ParseConf(string(b))
}

// ParseConf parses an AmneziaWG INI document.
func ParseConf(s string) (Config, error) {
	var c Config
	section := ""
	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch section {
		case "interface":
			switch strings.ToLower(k) {
			case "address":
				c.Address = firstCSV(v)
			case "dns":
				c.DNS = splitCSV(v)
			case "privatekey":
				c.PrivateKey = v
			case "jc":
				c.Jc = v
			case "jmin":
				c.Jmin = v
			case "jmax":
				c.Jmax = v
			case "s1":
				c.S1 = v
			case "s2":
				c.S2 = v
			case "s3":
				c.S3 = v
			case "s4":
				c.S4 = v
			case "h1":
				c.H1 = v
			case "h2":
				c.H2 = v
			case "h3":
				c.H3 = v
			case "h4":
				c.H4 = v
			case "headerprotectionkey":
				c.HeaderProtectionKey = v
			case "rekeyaftertime":
				c.RekeyAfterTime = v
			case "rekeytimeout":
				c.RekeyTimeout = v
			case "rejectaftertime":
				c.RejectAfterTime = v
			case "keepalivetimeout":
				c.KeepaliveTimeout = v
			case "maxhandshakeattempts":
				c.MaxHandshakeAttempts = v
			case "contentpaddingaddition":
				c.ContentPaddingAddition = v
			}
		case "peer":
			switch strings.ToLower(k) {
			case "publickey":
				c.PeerPublicKey = v
			case "presharedkey":
				c.PresharedKey = v
			case "endpoint":
				c.Endpoint = v
			case "allowedips":
				c.AllowedIPs = splitCSV(v)
			case "persistentkeepalive":
				c.PersistentKeepalive = v
			}
		}
	}
	if c.PrivateKey == "" || c.PeerPublicKey == "" || c.Endpoint == "" {
		return Config{}, fmt.Errorf("amnezia config: PrivateKey, PublicKey and Endpoint are required")
	}
	if len(c.AllowedIPs) == 0 {
		c.AllowedIPs = []string{"0.0.0.0/0"}
	}
	return c, nil
}

// DecodeVPNURI unpacks an Amnezia vpn:// share string (qCompress + base64url).
func DecodeVPNURI(uri string) (Config, json.RawMessage, error) {
	uri = strings.TrimSpace(uri)
	uri = strings.TrimPrefix(uri, "vpn://")
	if uri == "" {
		return Config{}, nil, fmt.Errorf("empty vpn:// uri")
	}
	raw, err := base64.URLEncoding.DecodeString(padB64(uri))
	if err != nil {
		raw, err = base64.RawURLEncoding.DecodeString(uri)
	}
	if err != nil {
		return Config{}, nil, fmt.Errorf("vpn:// base64: %w", err)
	}
	payload := raw
	if len(raw) >= 4 && raw[0] == 0 && raw[1] == 0 && raw[2] == 0 && raw[3] == 0xff {
		payload = raw[4:]
	}
	zr, err := zlib.NewReader(bytes.NewReader(payload))
	if err != nil {
		return Config{}, nil, fmt.Errorf("vpn:// zlib: %w", err)
	}
	defer zr.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(zr); err != nil {
		return Config{}, nil, fmt.Errorf("vpn:// inflate: %w", err)
	}
	js := buf.Bytes()
	var meta struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		APIConfig   struct {
			ServiceType     string `json:"service_type"`
			ServiceProtocol string `json:"service_protocol"`
			UserCountryCode string `json:"user_country_code"`
		} `json:"api_config"`
	}
	_ = json.Unmarshal(js, &meta)
	c := Config{
		Name:            meta.Name,
		ServiceType:     meta.APIConfig.ServiceType,
		ServiceProtocol: meta.APIConfig.ServiceProtocol,
	}
	return c, js, nil
}

// UAPISet is device keys followed by peer keys (tests / single-shot).
func (c Config) UAPISet() (string, error) {
	dev, err := c.UAPIDevice()
	if err != nil {
		return "", err
	}
	peer, err := c.UAPIPeer()
	if err != nil {
		return "", err
	}
	return dev + peer, nil
}

// UAPIDevice is the first UAPI set=1 body (obfuscation + private key, no peer).
// The official Amnezia Linux client applies this before adding the peer so
// S1–S4 and HeaderProtectionKey exist before the first handshake.
func (c Config) UAPIDevice() (string, error) {
	priv, err := keyToHex(c.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("private_key: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\n", priv)
	writeKV(&b, "jc", c.Jc)
	writeKV(&b, "jmin", c.Jmin)
	writeKV(&b, "jmax", c.Jmax)
	writeKV(&b, "s1", c.S1)
	writeKV(&b, "s2", c.S2)
	writeKV(&b, "s3", c.S3)
	writeKV(&b, "s4", c.S4)
	writeKV(&b, "h1", c.H1)
	writeKV(&b, "h2", c.H2)
	writeKV(&b, "h3", c.H3)
	writeKV(&b, "h4", c.H4)
	if k := strings.TrimSpace(c.HeaderProtectionKey); k != "" {
		hx, err := keyToHex(k)
		if err != nil {
			return "", fmt.Errorf("header_protection_key: %w", err)
		}
		fmt.Fprintf(&b, "header_protection_key=%s\n", hx)
	}
	writeKV(&b, "rekey_after_time", c.RekeyAfterTime)
	writeKV(&b, "rekey_timeout", c.RekeyTimeout)
	writeKV(&b, "reject_after_time", c.RejectAfterTime)
	writeKV(&b, "keepalive_timeout", c.KeepaliveTimeout)
	writeKV(&b, "max_handshake_attempts", c.MaxHandshakeAttempts)
	writeKV(&b, "content_padding_addition", c.ContentPaddingAddition)
	b.WriteString("replace_peers=true\n")
	return b.String(), nil
}

// UAPIPeer is the second UAPI set=1 body (endpoint, keys, IPv4 AllowedIPs).
func (c Config) UAPIPeer() (string, error) {
	pub, err := keyToHex(c.PeerPublicKey)
	if err != nil {
		return "", fmt.Errorf("public_key: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "public_key=%s\n", pub)
	if k := strings.TrimSpace(c.PresharedKey); k != "" {
		hx, err := keyToHex(k)
		if err != nil {
			return "", fmt.Errorf("preshared_key: %w", err)
		}
		fmt.Fprintf(&b, "preshared_key=%s\n", hx)
	}
	fmt.Fprintf(&b, "endpoint=%s\n", c.Endpoint)
	if v := strings.TrimSpace(c.PersistentKeepalive); v != "" {
		fmt.Fprintf(&b, "persistent_keepalive_interval=%s\n", v)
	}
	b.WriteString("replace_allowed_ips=true\n")
	wrote := false
	for _, ip := range c.AllowedIPs {
		ip = strings.TrimSpace(ip)
		if ip == "" || strings.Contains(ip, ":") {
			continue
		}
		fmt.Fprintf(&b, "allowed_ip=%s\n", ip)
		wrote = true
	}
	if !wrote {
		b.WriteString("allowed_ip=0.0.0.0/0\n")
	}
	return b.String(), nil
}

func writeKV(b *strings.Builder, k, v string) {
	v = strings.TrimSpace(v)
	if v == "" {
		return
	}
	fmt.Fprintf(b, "%s=%s\n", k, v)
}

func keyToHex(b64 string) (string, error) {
	b64 = strings.TrimSpace(b64)
	raw, err := base64.StdEncoding.DecodeString(padB64(b64))
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(b64, "="))
	}
	if err != nil {
		return "", err
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("want 32 bytes, got %d", len(raw))
	}
	return hex.EncodeToString(raw), nil
}

func padB64(s string) string {
	switch len(s) % 4 {
	case 2:
		return s + "=="
	case 3:
		return s + "="
	default:
		return s
	}
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstCSV(v string) string {
	parts := splitCSV(v)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
