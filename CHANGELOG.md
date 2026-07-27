# Changelog

[![Release](https://img.shields.io/github/v/release/unhexx/outline-gate?display_name=tag&sort=semver)](https://github.com/unhexx/outline-gate/releases/latest)
[![CI](https://github.com/unhexx/outline-gate/actions/workflows/ci.yml/badge.svg?branch=master)](https://github.com/unhexx/outline-gate/actions/workflows/ci.yml)

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Published releases: https://github.com/unhexx/outline-gate/releases

## [0.4.0] — 2026-07-27

### Added

- Build-time version (`internal/version`) shown in Web UI header/footer via public `GET /api/v1/version`
- Version also in `GET /api/v1/status` and startup logs
- Optional Prometheus metrics at `/metrics` (`METRICS_ENABLE=true`)
- IPv6 `IP6T_SO_ORIGINAL_DST` recovery in transparent proxy (nft L3 sets remain IPv4-only)
- SOCKS startup **Warn** when listen is non-loopback and `SOCKS_ALLOW_CIDRS` is empty
- connlog event **TTL** (default 1h) in addition to ring capacity
- `proxy.EnginePathDecider` (moved L3 path mapping out of `main`)

### Fixed

- Web UI no longer hardcodes stale `v0.2.0` — displays process build version
- `gateway.Flush` returns real nft errors (was always `nil`)
- `gateway.UpdateEngine` uses single lock path with re-apply (no race with `Flush`)
- `nft` resolved via `LookPath` / `/usr/sbin/nft` (not bare PATH-only)
- clearer `ip_forward` errors with CAP_NET_ADMIN hint
- bypass rules file: temp write + **fsync** + rename
- Go module path aligned with GitHub: `github.com/unhexx/outline-gate`

### Changed

- Dockerfile/Makefile inject `VERSION` via `-ldflags`
- README badges and install docs for v0.4.0

## [0.3.0] — 2026-07-27

### Added

- Optional SOCKS source allowlist: `SOCKS_ALLOW_CIDRS` / `SOCKS_ALLOW_CIDRS_FILE` (reject + `Warn` when outside)
- Gateway `Apply` retries with exponential backoff (`RECONNECT_BASE_DELAY` / `RECONNECT_MAX_DELAY`) instead of fatal on first nft failure
- Entrypoint validates access key prefix (`ss://` or `ssconf://`) before starting the process

### Fixed

- Shutdown no longer hangs forever if a goroutine stalls: bounded `wg.Wait` (10s)
- Config reload (`SIGHUP`) vs Web UI status: all live `cfg` reads go through mutex
- Transparent proxy logs `Warn` on `SO_ORIGINAL_DST` failure (was silent `Debug`); clears deadlines before relay
- MaintainReady panics are recovered and reported as fatal errors

### Changed

- Gateway apply loop refactored (no `goto`); process stays up if nft is temporarily unavailable
- Dockerfile: soft-pin `nftables=~1.1`, comment that transproxy port 12345 is loopback-only
- Docs: explicit IPv6 L3 gap (IPv4-only nft path; dual-stack IPv6 bypasses tunnel)

### Security

- Documented and implemented optional source CIDR restriction for unauthenticated SOCKS5

## [0.2.0] — 2026-07-27

### Added

- Live **connection routing log** in Web UI (tab **Лог**): path chains `client → SOCKS|L3 → VPN|Direct → host`
- In-memory `connlog` ring buffer (~500 events) from SOCKS5 and L3 transparent proxy
- API: `GET /api/v1/connections`, `GET /api/v1/connections/stream` (SSE; `?token=` for EventSource)
- API: `GET /api/v1/status` (outline + runtime + per-minute VPN/Direct counts)
- Bypass match returns rule name for log display (`MatchBypass` / `MatchHostDetail`)
- L3 **userspace path decision**: non-private TCP (VPN + Direct + Drop) through transparent proxy and logged
- Compact Web UI icons (logo, favicon, VPN/Direct/Drop, tabs) under `/ui/icons/`

### Changed

- Web UI: dense layout (~13px), tabs **Статус · Лог · Bypass · Ключ**, status pills, filters, SSE pause
- nftables: only RFC1918/reserved stay on kernel path; user Direct is no longer skipped by nft `@bypass`
- Transparent proxy dials Direct or Outline based on `routing.Engine`
- Docs and mockups updated for live log + L3 userspace routing

## [0.1.0] — 2026-07-27

First public release of **outline-gate**: Docker LAN gateway to Outline (Shadowsocks).

### Added

- Outline client via **outline-sdk** with `ss://` and dynamic `ssconf://` access keys
- Local **SOCKS5** proxy (`:1080`) with CONNECT (TCP)
- Optional **L3 gateway** (nftables): transparent TCP redirect, masquerade
- Split-tunnel modes: **`exclude`** (default) and **`include`** with `DIRECT_POLICY`
- Always-bypass for private/reserved ranges and Outline server IP (loop protection)
- User bypass rules: IP, CIDR, domains, `*.suffix` (file + Web UI)
- Domain DNS refresh for L3 bypass sets (`BYPASS_DNS_REFRESH`)
- SOCKS path: direct dial when destination matches bypass
- Embedded **Web UI** (`/ui/`) and JSON API:
  - manage bypass list
  - replace Outline access key (persist to `OUTLINE_KEY_PERSIST_FILE`)
  - token auth (`UI_TOKEN` / Bearer / Basic password)
- Health endpoints: `/healthz`, `/readyz`
- Docker multi-stage image, compose profiles (bridge SOCKS, host L3)
- Documentation (RU operations, architecture, routing, SOCKS/L3 guides, diagrams)
- CI: test, vet, build on push/PR

### Security notes (v1)

- SOCKS has **no authentication** — restrict to trusted LAN only
- Web UI API requires `UI_TOKEN`; health endpoints are open for probes
- Access keys must not be committed; use `.env` / secrets / UI persist file
