# Routing modes

## Always bypass

Regardless of mode, the following never go through the tunnel:

- Default private/reserved ranges (RFC1918, CGNAT `100.64.0.0/10`, link-local, loopback, multicast, …)
- Extra `BYPASS_CIDRS` / file
- Resolved Outline server IPv4 (auto)

This prevents blackhole loops when the tunnel itself would encapsulate traffic to the proxy server.

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
