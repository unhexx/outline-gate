# Routing modes

**Version:** [v0.1.0](https://github.com/unhexx/outline-gate/releases/tag/v0.1.0) · diagrams: [l3-exclude.svg](images/l3-exclude.svg) · [l3-include.svg](images/l3-include.svg) · [socks5-flow.svg](images/socks5-flow.svg)

## Always bypass

Regardless of mode, the following never go through the tunnel:

- Default private/reserved ranges (RFC1918, CGNAT `100.64.0.0/10`, link-local, loopback, multicast, …)
- Extra `BYPASS_CIDRS` / file
- User rules from `BYPASS_RULES_FILE` / Web UI (`/ui/`): IP, CIDR, domains, `*.suffix`
- Resolved Outline server IPv4 (auto)

This prevents blackhole loops when the tunnel itself would encapsulate traffic to the proxy server.

### Domains and masks

| Rule | SOCKS5 | L3 gateway (nftables) |
|------|--------|------------------------|
| `8.8.8.8`, `10.0.0.0/8` | match by IP | in bypass set |
| `example.com` | match CONNECT hostname | DNS resolve → `/32` in set (refresh `BYPASS_DNS_REFRESH`) |
| `*.example.com` | host == apex or `*.example.com` suffix | resolve apex `example.com` only (best-effort) |

L3 domain bypass is best-effort: clients resolve DNS themselves; only IPs known after resolve land in the nft set. SOCKS matches hostnames reliably.

## Mode `exclude` (default)

```
if dst ∈ bypass → direct
else            → tunnel
```

Typical “VPN for everything except LAN and exceptions”.

## Mode `include`

```
if dst ∈ bypass        → direct
elif dst ∈ tunnel list → tunnel
else                   → DIRECT_POLICY (direct|drop)
```

Requires non-empty `TUNNEL_CIDRS` or `TUNNEL_CIDRS_FILE`.

## Examples

```bash
# Everything via Outline except private nets
ROUTING_MODE=exclude GATEWAY_ENABLE=true

# Only specific services via Outline
ROUTING_MODE=include
TUNNEL_CIDRS=203.0.113.0/24,8.8.8.8/32
DIRECT_POLICY=direct
```

## L3 vs SOCKS

| | SOCKS | L3 gateway |
|--|-------|------------|
| App config | per-app proxy | default GW / routes |
| Protocols | TCP (SOCKS5 CONNECT) | TCP via REDIRECT (v1) |
| Privileges | low | NET_ADMIN + nftables |

## Reload

Send `SIGHUP` to re-read environment/files and rebuild nft sets (process must receive updated env or use list files on a volume).

Web UI / API updates `BYPASS_RULES_FILE` immediately and triggers DNS refresh + nft update when the gateway is active. Outline keys set via UI persist to `OUTLINE_KEY_PERSIST_FILE` and reconnect the dialer without full container recreate.

## Related

- [OPERATIONS.ru.md](OPERATIONS.ru.md) — deploy steps
- [README.md](../README.md) — SOCKS/L3 practical examples
- [CHANGELOG.md](../CHANGELOG.md) — release notes
