# Architecture

**Version:** [v0.4.0](https://github.com/unhexx/outline-gate/releases/tag/v0.4.0) · diagrams: [docs/images/](images/)

```
LAN clients ──► outline-gate ──ss:// / ssconf://──► Outline Server
                   │
                   ├── SOCKS5 :1080          (explicit proxy + bypass match)
                   ├── transparent TCP       (nft REDIRECT, L3)
                   ├── nftables split tunnel (exclude / include + bypass sets)
                   ├── Web UI / API :8080    (/ui/, /api/v1/*)
                   └── health :8080          (/healthz, /readyz)
```

See also: [architecture-overview.svg](images/architecture-overview.svg).

## Components

| Package | Role |
|---------|------|
| `cmd/outline-gate` | Lifecycle, signals, wiring |
| `internal/config` | Env, CIDR files, UI flags, key persist path |
| `internal/outline` | outline-sdk StreamDialer, server IP, reconnect, `SetAccessKey` |
| `internal/proxy` | SOCKS5 + transparent TCP (`SO_ORIGINAL_DST`); SOCKS direct on bypass; connection hooks |
| `internal/connlog` | In-memory ring buffer of routing events for live UI log |
| `internal/routing` | Pure decision engine (IP sets) |
| `internal/gateway` | nftables apply/flush |
| `internal/bypass` | User rules (IP/CIDR/domain/`*.suffix`), store, DNS refresh, matcher |
| `internal/webui` | Embedded UI `/ui/` + API `/api/v1/*` (bypass, outline, connections, status) |
| `internal/health` | `/healthz`, `/readyz` |
| `internal/logging` | slog setup |

## Data path (L3)

1. Client routes packet via gate IP (`ip_forward=1`).
2. nftables `prerouting`: destinations in `@private` (RFC1918 / reserved) stay on the **kernel** path; all other TCP is **REDIRECT**ed to the transparent proxy.
3. Transparent proxy reads `SO_ORIGINAL_DST` and uses `routing.Engine`:
   - **tunnel** → Outline dialer  
   - **direct** → local `net.Dialer` (user bypass, Outline server IP, include residual)  
   - **drop** → close (include + `DIRECT_POLICY=drop`)
4. Each attempt is recorded in `connlog` (`via=tunnel|direct|drop`).
5. `postrouting` MASQUERADE rewrites source for return path.

UDP is not fully handled in v0.4.0 (TCP-first). Use SOCKS5 for apps that need full proxy semantics without L3.

## Data path (SOCKS5)

1. Client CONNECT to `host:1080` (no SOCKS auth).
2. If host/IP matches bypass rules → **direct** `net.Dialer`.
3. Else → Outline dialer.
4. Domain rules match CONNECT hostname exactly (including `*.suffix`).
5. Each CONNECT is recorded in `connlog` (`via=tunnel|direct`, optional matched rule).

## Connection log (Web UI)

- Ring buffer (~500 events) in process memory; no disk persistence.
- Sources: **SOCKS5** (tunnel + direct) and **L3 transparent** (tunnel + direct + drop for non-private TCP).
- Private/RFC1918 L3 flows stay in kernel and are not logged.
- API: `GET /api/v1/connections`, `GET /api/v1/connections/stream` (SSE; `?token=` for EventSource).
- UI tab **Лог** shows path chains: `client → SOCKS|L3 → VPN|Direct|Drop → host`.

## Config reload

- **SIGHUP** — re-read env/files, rebuild routing engine and nft sets.
- **Web UI** — mutates `BYPASS_RULES_FILE` and `OUTLINE_KEY_PERSIST_FILE` live; DNS refresh on interval / apply.

## Security surfaces

| Surface | Auth |
|---------|------|
| SOCKS5 | none (LAN only) |
| `/healthz`, `/readyz` | none |
| `/ui/` static | none |
| `/api/v1/*` | `UI_TOKEN` (Bearer, Basic password, or `?token=` for SSE) |
