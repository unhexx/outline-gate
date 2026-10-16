package amnezia

import (
	"strings"
	"testing"
)

func TestParseConf(t *testing.T) {
	raw := `[Interface]
Address = 100.101.254.38/32
DNS = 100.64.0.1, 8.8.4.4
PrivateKey = UeO7Zc84kdTluEB9FNc0pGP1q5vEKD7ioy7IxiY8w3c=
Jc = 6
Jmin = 10
Jmax = 80
S1 = 1113
S2 = 600
S3 = 599
S4 = 12
H1 = 1
H2 = 2
H3 = 3
H4 = 4
HeaderProtectionKey = 8P6j9u8YXw64UXA+rpMLdBt+ERxvk+EtenRzUVQ9iHc=
RekeyAfterTime = 100-120
[Peer]
PublicKey = HXdwZYifxJ8Mqn02/PjdxVVKA4+5f+vMjjvgN76GYGI=
PresharedKey = wHkLRBjorYq1PJFIBQtNTpjw0av9dPmhXxZzMPsqLmk=
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = 146.70.206.148:9506
PersistentKeepalive = 25-35
`
	c, err := ParseConf(raw)
	if err != nil {
		t.Fatal(err)
	}
	if c.Address != "100.101.254.38/32" {
		t.Fatalf("address %q", c.Address)
	}
	if ip := c.EndpointIP(); ip == nil || ip.String() != "146.70.206.148" {
		t.Fatalf("endpoint ip %v", ip)
	}
	ip, n, err := c.InterfaceAddr()
	if err != nil {
		t.Fatal(err)
	}
	if ip.String() != "100.101.254.38" || n.String() != "100.101.254.38/32" {
		t.Fatalf("iface %s %s", ip, n)
	}
	uapi, err := c.UAPISet()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"private_key=",
		"public_key=",
		"preshared_key=",
		"header_protection_key=",
		"jc=6",
		"s1=1113",
		"h1=1",
		"endpoint=146.70.206.148:9506",
		"persistent_keepalive_interval=25-35",
		"allowed_ip=0.0.0.0/0",
		"rekey_after_time=100-120",
		"replace_peers=true",
	} {
		if !strings.Contains(uapi, want) {
			t.Fatalf("uapi missing %q\n%s", want, uapi)
		}
	}
	if strings.Contains(uapi, "PrivateKey") {
		t.Fatal("uapi must use hex keys, not INI names")
	}
	if strings.Contains(uapi, "allowed_ip=::/0") {
		t.Fatal("IPv6 AllowedIPs must be omitted so the userspace stack stays IPv4")
	}
}

func TestDecodeVPNURI(t *testing.T) {
	// qCompress-style: 00 00 00 ff + zlib(json)
	uri := "AAAA_3icNYxBCoAgFAXv8ta2aellROQnH9IvakaJdy-JtsPMdEQbCBoVCk7ixt40yoUlQq8KNrH5MHRHodzYkalXmo0NkW62S8oU-Ajv4RdSlipO9imdHmM8a44lDA=="
	c, raw, err := DecodeVPNURI("vpn://" + uri)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "config_version") && !strings.Contains(string(raw), "name") {
		t.Fatalf("json %s", raw)
	}
	_ = c
}

func TestParseConfMissingPeer(t *testing.T) {
	_, err := ParseConf("[Interface]\nPrivateKey = UeO7Zc84kdTluEB9FNc0pGP1q5vEKD7ioy7IxiY8w3c=\n")
	if err == nil {
		t.Fatal("expected error")
	}
}
