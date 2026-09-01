# Changelog

[![Release](https://img.shields.io/github/v/release/unhexx/outline-gate?display_name=tag&sort=semver)](https://github.com/unhexx/outline-gate/releases/latest)
[![CI](https://github.com/unhexx/outline-gate/actions/workflows/ci.yml/badge.svg?branch=master)](https://github.com/unhexx/outline-gate/actions/workflows/ci.yml)

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Published releases: https://github.com/unhexx/outline-gate/releases

## [Unreleased]

### Added

- README: connectivity check that compares host vs SOCKS egress without printing addresses; stack badges
- README / DEPLOY / OPERATIONS: раздел «прокси и шлюз одновременно» (`./install.sh --host` = SOCKS5 + L3)

## [0.6.0] — 2026-08-17

### Added

- Destination block list: IP / CIDR / domain / `*.suffix` (`BLOCK_RULES_FILE`, Web UI tab **Блок**, `GET/POST/DELETE /api/v1/block`)
- Blocked SOCKS and L3 connections appear in the live log as **Блок** (`via=drop`) with the matched rule

### Changed

- README badges and install docs for v0.6.0

## [0.5.0] — 2026-08-17

### Added

- Periodic `ssconf://` refresh and Outline tunnel TCP probe so a rotated provider endpoint no longer leaves SOCKS healthy-but-dead (`SSCONF_REFRESH_INTERVAL`, `TUNNEL_PROBE_*`)
- Prometheus counters `outline_gate_tunnel_probe_total` and `outline_gate_ssconf_refresh_total`
- `docs/DEPLOY.ru.md` — пошаговое развёртывание на другом хосте
- `deploy/compose/install.sh` / root `install.sh` — one-shot build/up/readyz (`--host`, `--socks`, `--down`, `--check`)
- Makefile targets: `install`, `install-host`, `down`
- Compose/env: явный проброс `SOCKS_ALLOW_CIDRS` / `SOCKS_ALLOW_CIDRS_FILE`

### Fixed

- Stale Outline dialer after `ssconf://` backend rotation: `/readyz` stayed 200 while SOCKS timed out
- SOCKS/L3 tunnel dial failures now logged at Warn (were Debug-only)
- Compose bridge uses explicit IPAM (`outline-gate_net` / `COMPOSE_SUBNET`) so `docker compose up` works when Docker default-address-pools are exhausted
- `install.sh` invoked via `bash` (no `+x` / noexec required)
- SOCKS IP CONNECT now bypasses static/RFC1918 CIDRs (`BYPASS_CIDRS`), not only user rules
- `?token=` authorizes only `GET /api/v1/connections/stream` (no longer all `/api/*`)
- connlog: no panic when an SSE client unsubscribes during `Record`
- ssconf JSON → `ss://`: URL-safe base64 (SIP002) and IPv6 `JoinHostPort`
- Outline client marks not-ready after consecutive transport timeouts so `MaintainReady` reconnects
- SIGHUP applies `SOCKS_ALLOW_CIDRS` to the live SOCKS listener
- Persist access key with fsync; `/readyz` 503 JSON Content-Type
- Web UI: VPN log filter no longer includes Drop; ready pill keeps its icon

### Changed

- `configure.sh`: профиль socks|host, UI_TOKEN (автоген), порты, SOCKS allowlist; bootstrap `config/*`
- `.env.example`: `HOST_HEALTH_PORT=28080` по умолчанию, комментарии allowlist / `COMPOSE_PROFILE`
- Runtime `config/bypass.rules.txt` больше не в git (шаблон — `bypass.rules.example.txt`)
- `bypass.rules.example.txt`: предустановленные исключения `*.max.ru`, `*.aq.ru`, `*.aq.local`, `*.aservice24.ru`, `*.yandex.cloud`, `*.yandex.ru`
- Docs: простые команды добавления/удаления bypass (API + файл + `SIGHUP`) — `OPERATIONS.ru.md` §7.2, README
- README badges and install docs for v0.5.0

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
