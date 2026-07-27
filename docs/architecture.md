# Architecture

```
LAN clients ──► outline-gate ──ss://──► Outline Server
                   │
                   ├── SOCKS5 :1080          (explicit proxy)
                   ├── transparent TCP       (nft REDIRECT)
                   ├── nftables split tunnel (exclude / include)
                   └── health :8080
```

## Components

| Package | Role |
|---------|------|
| `cmd/outline-gate` | Lifecycle, signals, wiring |
| `internal/config` | Env + CIDR list files |
| `internal/outline` | outline-sdk StreamDialer, server IP, reconnect |
| `internal/proxy` | SOCKS5 + transparent TCP (SO_ORIGINAL_DST) |
| `internal/routing` | Pure decision engine |
| `internal/gateway` | nftables apply/flush |
| `internal/bypass` | User rules (IP/CIDR/domain), DNS resolve, matcher |
| `internal/webui` | Embedded UI `/ui/` + API `/api/v1/bypass` |
| `internal/health` | `/healthz`, `/readyz` |

## Data path (L3)

1. Client routes packet via gate IP (`ip_forward=1`).
2. nftables `prerouting` REDIRECTs matching TCP to local transparent port.
3. Transparent proxy reads original destination and dials via Outline.
4. `postrouting` MASQUERADE rewrites source for return path.

UDP is not fully handled in v1 (TCP-first). Use SOCKS5 for apps that need full proxy semantics without L3.
