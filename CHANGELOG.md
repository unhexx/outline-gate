# Changelog

[![Release](https://img.shields.io/github/v/release/unhexx/outline-gate?display_name=tag&sort=semver)](https://github.com/unhexx/outline-gate/releases/latest)
[![CI](https://github.com/unhexx/outline-gate/actions/workflows/ci.yml/badge.svg?branch=master)](https://github.com/unhexx/outline-gate/actions/workflows/ci.yml)

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Published releases: https://github.com/unhexx/outline-gate/releases

## [Unreleased]

### Added

- Live **connection routing log** in Web UI (tab **Лог**): path chains `client → SOCKS|L3 → VPN|Direct → host`
- In-memory `connlog` ring buffer (~500 events) from SOCKS5 and L3 transparent proxy
- API: `GET /api/v1/connections`, `GET /api/v1/connections/stream` (SSE; `?token=` for EventSource)
- API: `GET /api/v1/status` (outline + runtime + per-minute VPN/Direct counts)
- Bypass match returns rule name for log display (`MatchBypass` / `MatchHostDetail`)

### Changed

- Web UI redesigned with tabs: **Статус · Лог · Bypass · Ключ**, status pills, filters and pause on the log

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

### Known limitations

- TCP-first; full UDP not covered in L3 path
- L3 domain bypass is best-effort (resolved IPs); SOCKS hostname match is reliable
- IPv6 path is limited (IPv4-oriented nft sets)

[0.1.0]: https://github.com/unhexx/outline-gate/releases/tag/v0.1.0
