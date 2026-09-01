# outline-gate

<p align="center">
  <a href="https://github.com/unhexx/outline-gate/actions/workflows/ci.yml"><img src="https://github.com/unhexx/outline-gate/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI"></a>
  <a href="https://github.com/unhexx/outline-gate/releases/latest"><img src="https://img.shields.io/github/v/release/unhexx/outline-gate?display_name=tag&sort=semver" alt="Release"></a>
  <a href="https://github.com/unhexx/outline-gate/releases/latest"><img src="https://img.shields.io/github/release-date/unhexx/outline-gate" alt="Release date"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/unhexx/outline-gate" alt="Go"></a>
  <a href="https://pkg.go.dev/github.com/unhexx/outline-gate"><img src="https://pkg.go.dev/badge/github.com/unhexx/outline-gate.svg" alt="Go Reference"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/unhexx/outline-gate" alt="License"></a>
</p>

<p align="center"><strong>English</strong> · <a href="README.ru.md">Русский</a></p>

**Current release: [v0.6.0](https://github.com/unhexx/outline-gate/releases/latest)** · [Changelog](CHANGELOG.md) · [Binary `linux/amd64`](https://github.com/unhexx/outline-gate/releases/latest/download/outline-gate_linux_amd64)

## About

**outline-gate** is a self-contained Docker gateway that turns an [Outline](https://getoutline.org/) (Shadowsocks) access key into **working VPN access for a LAN and for apps**: no Outline Client GUI on every device, with **split-tunnel** and a fast exception list.

One container on a Linux host:

1. brings up an Outline client (`ss://` / `ssconf://`);
2. serves **SOCKS5** for opt-in proxy;
3. optionally becomes the **default gateway** (L3 + nftables) for all TCP on the network;
4. ships a **Web UI** to manage the "not via VPN" list and rotate the key without rebuilding the image.

### What it is for

| Job | How outline-gate covers it |
|-----|----------------------------|
| **VPN for the whole network** without Outline Client on TVs, IoT, phones | L3: clients set GW to the host; traffic goes through Outline |
| **VPN for some apps only** | SOCKS5 `:1080` in the browser, curl, Git, Docker; everything else stays direct |
| **Split-tunnel: everything via VPN except…** | `exclude` + bypass (RFC1918, IP/CIDR, domains, `*.mask`) |
| **Split-tunnel: VPN only for selected destinations** | `include` + `TUNNEL_CIDRS` (+ `direct` / `drop` for the rest) |
| **Do not break LAN, banks, internal APIs** | Always-bypass of private nets + UI/API exception list |
| **Rotate the Outline key without a redeploy** | Web UI / `PUT /api/v1/outline` → reconnect + persist file |
| **Quickly add "do not send through VPN"** | Web UI: IP, subnet, `example.com`, `*.cdn.example.net` |
| **Block domains / addresses / masks** | **Block** tab; matches show as "Block" in the log |
| **Check that the tunnel is alive** | `/readyz`, Docker healthcheck, egress-check via SOCKS |
| **One service instead of a zoo of clients** | Compose + `.env` / secrets; the key is not baked into the image |
| **Provider dynamic key** | `ssconf://` is resolved on Connect and re-fetched periodically; a TCP tunnel probe resets a stuck dialer |

### Who it is for

- **Home / small office** — one always-on Linux box (NUC, mini-PC, VM): "a router with Outline".
- **Dev and ops** — SOCKS for CLI/IDE/containers, without flipping a system VPN.
- **LAN admins** — central egress and an exclude/include policy, not per-device apps.

### What it is not

- Not **Outline Server / Manager** — **client only**, for a key you already have.
- Not full **DNS-over-VPN** and not full **UDP/L3** (v0.6 is TCP-first; IPv6 L3 nft is a gap).
- Not a multi-user IdP: Web UI is one `UI_TOKEN`; SOCKS has **no password** (LAN + optional `SOCKS_ALLOW_CIDRS`).

<p align="center">
  <img src="docs/images/architecture-overview.svg" alt="outline-gate architecture: SOCKS5 and L3 gateway" width="920"/>
</p>

## Features

- Outline client via **outline-sdk** (`ss://`, `ssconf://`)
- **SOCKS5** (`:1080`) — explicit proxy; bypass → direct dial
- **L3 gateway** (nftables): `exclude` / `include`, REDIRECT + MASQUERADE
- **Web UI** (`/ui/`): compact UI, live log, bypass, key replace; process version from `/api/v1/version`
- Config: `.env`, volume files, Docker secrets, SIGHUP reload
- Health: `/healthz`, `/readyz`; optional Prometheus `/metrics` (`METRICS_ENABLE=true`)

## Contents

1. [About](#about)
2. [Quick start](#quick-start)
   - [Proxy and gateway together](#proxy-and-gateway-together)
3. [Connectivity check](#connectivity-check)
4. [Release and install](#release-and-install)
5. [SOCKS5 vs L3](#socks5-vs-l3)
6. [Using SOCKS5](#using-socks5)
7. [Using L3 gateway](#using-l3-gateway)
8. [Web UI](#web-ui)
9. [Environment variables](#environment-variables)
10. [Building the image](#building-the-image)
11. [Best practices](#best-practices)
12. [Documentation](#documentation)

---

## Quick start

Full steps: **[docs/DEPLOY.md](docs/DEPLOY.md)** · [Русский](docs/DEPLOY.ru.md).

```bash
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate
./install.sh 'ss://YOUR_OUTLINE_KEY'

curl -s http://127.0.0.1:28080/readyz
# then — [connectivity check](#connectivity-check)
```

L3 gateway (host network; **SOCKS5 still listens**):

```bash
./install.sh --host 'ss://YOUR_OUTLINE_KEY'
```

### Proxy and gateway together

`--host` starts **one** process in both modes: SOCKS5 for apps and L3 gateway for the LAN. You do not need a second container for the proxy.

```bash
./install.sh --host 'ss://YOUR_OUTLINE_KEY'
```

`deploy/compose/.env`:

```bash
COMPOSE_PROFILE=host
GATEWAY_ENABLE=true
SOCKS_LISTEN=0.0.0.0:1080
HEALTH_LISTEN=0.0.0.0:8080    # health + Web UI (host network)
UI_ENABLE=true
UI_TOKEN=your-secret
```

| What | Where |
|------|-------|
| SOCKS5 | `HOST:1080` — browser, curl, Git ([using SOCKS5](#using-socks5)) |
| L3 gateway | clients' default gateway = host IP ([using L3](#using-l3-gateway)) |
| Web UI / health | `http://HOST:8080/ui/` (port = `HEALTH_LISTEN`) |

Check:

```bash
curl -s http://127.0.0.1:8080/readyz
curl -s --socks5h 127.0.0.1:1080 https://ifconfig.me
# then — [connectivity check](#connectivity-check)
```

Do not run `docker-compose.yml` and `docker-compose.host.yml` side by side: the container name is `outline-gate`, ports 1080 and health collide.  
A bridge profile with `GATEWAY_ENABLE=true` is not a LAN gateway out of the box.

Bridge network `outline-gate_net` = `192.168.102.0/24` (explicit IPAM; does not consume Docker `default-address-pools`).  
Daemon example: [`deploy/docker/daemon.json.example`](deploy/docker/daemon.json.example).

---

## Connectivity check

On a **SOCKS-only** profile (`GATEWAY_ENABLE=false`) the host does **not** send its own traffic into the tunnel. Only a client that **explicitly** uses SOCKS goes via VPN. A direct request from the host always shows the ISP path — that is not a bug.

### 1. Process is alive

```bash
curl -fsS --max-time 5 "http://127.0.0.1:${HOST_HEALTH_PORT:-28080}/readyz"
```

Expect HTTP 200 and `"ready":true`. Otherwise see `docker compose logs` — key, path to Outline, ssconf.

### 2. Compare host path vs SOCKS path

You need any **HTTPS IP-echo** (a service that replies with the client address in the body). Put the URL in a variable and **do not print** the values:

```bash
# set your own echo URL
: "${IP_ECHO:?set IP_ECHO to an HTTPS IP-echo URL}"

SOCKS="socks5h://127.0.0.1:${HOST_SOCKS_PORT:-1080}"

direct=$(curl -fsS --max-time 10 "$IP_ECHO") || exit 1
via_socks=$(curl -fsS --max-time 15 -x "$SOCKS" "$IP_ECHO") || exit 1

if [ -n "$direct" ] && [ -n "$via_socks" ] && [ "$direct" != "$via_socks" ]; then
  echo "ok: SOCKS egress differs from the host path"
else
  echo "fail: SOCKS path matches the host (proxy not used, or tunnel not carrying traffic)" >&2
  exit 1
fi
```

| Result | Meaning |
|--------|---------|
| strings **differ** | traffic through SOCKS went into Outline |
| strings **match** | request did not use the tunnel (no proxy, fallback, or `GATEWAY_ENABLE=false` and the client went direct) |
| curl error / SOCKS `0x08` | often local IPv6 resolve: use **`socks5h`**, not `socks5` |

**`socks5h`** (and `curl --socks5-hostname`) resolves the name **on the proxy**. Scheme `socks5://` resolves on the host; outline-gate rejects IPv6 CONNECT, so the client may skip the proxy.

In a browser: SOCKS5 + **DNS through SOCKS**. In the UI log a successful check is the chain `SOCKS → VPN`.

### 3. What not to do

- Do not judge by a direct curl/browser on the host: without SOCKS that is always the ISP.
- Do not paste the resulting addresses into tickets.
- Do not confuse process readiness (`/readyz`) with *your* app actually using `:1080`.

---

## Release and install

| Channel | Link |
|---------|------|
| GitHub Releases | https://github.com/unhexx/outline-gate/releases |
| Latest tag | [`v0.6.0`](https://github.com/unhexx/outline-gate/releases/tag/v0.6.0) |
| Changelog | [CHANGELOG.md](CHANGELOG.md) |
| Module path | `github.com/unhexx/outline-gate` |
| Internal git | `https://git.aservice24.ru/scm/expert/outline-gate.git` (branch `master`, tag `v0.6.0`) |

### Docker (recommended)

```bash
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate
./install.sh 'ss://...'          # or: git checkout v0.6.0 && ./install.sh 'ss://...'
```

Image with the release tag:

```bash
docker build -f deploy/docker/Dockerfile --build-arg VERSION=0.6.0 -t outline-gate:v0.6.0 .
```

### Linux amd64 binary

```bash
curl -fsSL -o outline-gate \
  https://github.com/unhexx/outline-gate/releases/download/v0.6.0/outline-gate_linux_amd64
chmod +x outline-gate
export OUTLINE_ACCESS_KEY='ss://...'
./outline-gate
```

Needs Linux (L3 needs root/`NET_ADMIN` + `nft`). Prefer the Docker image in production.

---

## SOCKS5 vs L3

<p align="center">
  <img src="docs/images/compare-modes.svg" alt="SOCKS5 vs L3 gateway" width="920"/>
</p>

| | SOCKS5 | L3 gateway |
|--|--------|------------|
| Client setup | proxy in the app | default gateway / static route |
| Coverage | only apps with proxy | almost all LAN TCP |
| Compose | `docker-compose.yml`, `GATEWAY_ENABLE=false` | `docker-compose.host.yml`, `GATEWAY_ENABLE=true` |
| Privileges | ordinary Docker | `NET_ADMIN`, nftables, usually `network_mode: host` |
| Domains in bypass | **exact** hostname match | DNS → IP (best-effort) |
| UDP | not in v1 | not in v1 (TCP-first) |
| Typical case | laptop, browser, CLI | TV, IoT, "router with VPN" |

**You can use both at once:** L3 for devices without a proxy + SOCKS for apps on the same host. Quick start: [proxy and gateway together](#proxy-and-gateway-together).

---

## Using SOCKS5

<p align="center">
  <img src="docs/images/socks5-flow.svg" alt="SOCKS5 flow" width="920"/>
</p>

### 1. Start (bridge, SOCKS only)

`deploy/compose/.env`:

```bash
OUTLINE_ACCESS_KEY=ss://...@server:port
ROUTING_MODE=exclude
GATEWAY_ENABLE=false
SOCKS_LISTEN=0.0.0.0:1080
HOST_SOCKS_PORT=1080
HOST_HEALTH_PORT=28080
UI_ENABLE=true
UI_TOKEN=your-secret
```

```bash
cd deploy/compose
docker compose up --build -d
curl -s http://127.0.0.1:28080/readyz
```

Then [connectivity check](#connectivity-check) (`socks5h`, compare paths without printing addresses).

### 2. curl / wget

```bash
# HTTP(S) via SOCKS (DNS on the proxy)
curl -x socks5h://127.0.0.1:1080 "$IP_ECHO"

# same
curl --socks5-hostname 127.0.0.1:1080 "$IP_ECHO"

export ALL_PROXY=socks5h://127.0.0.1:1080
curl -fsS "$IP_ECHO" >/dev/null
unset ALL_PROXY
```

From another LAN host, use the outline-gate machine address instead of loopback (same `socks5h` and `HOST_SOCKS_PORT`).

### 3. Firefox

1. **Settings → Network Settings → Settings…**
2. **Manual proxy configuration**
3. **SOCKS Host:** `127.0.0.1` (or the gate IP), **Port:** `1080`
4. Select **SOCKS v5**
5. Enable **Proxy DNS when using SOCKS v5**
6. OK → open any HTTPS IP-echo and compare with a request *without* proxy (as in [connectivity check](#connectivity-check))

### 4. Chromium / Chrome

Chrome has no built-in SOCKS UI. Options:

```bash
# Linux: separate profile + proxy-server
google-chrome --user-data-dir=/tmp/chrome-socks \
  --proxy-server="socks5://127.0.0.1:1080" \
  --host-resolver-rules="MAP * ~NOTFOUND , EXCLUDE localhost"
```

(Chrome resolves DNS itself unless you set resolver-rules; for an egress check, curl + `socks5h` is more reliable.)

Or an extension / system proxy (OS-dependent).

### 5. SSH over SOCKS (ProxyCommand / Dynamic)

SSH *through* Outline:

```bash
ssh -o ProxyCommand='nc -X 5 -x 127.0.0.1:1080 %h %p' user@remote-host
```

(`nc`/`ncat` depends on the distro; `connect-proxy` is an alternative.)

### 6. Git via SOCKS

```bash
git config --global http.proxy socks5h://127.0.0.1:1080
git config --global https.proxy socks5h://127.0.0.1:1080
# disable:
git config --global --unset http.proxy
git config --global --unset https.proxy
```

### 7. Docker container egress via SOCKS

```bash
docker run --rm curlimages/curl:latest \
  -x "socks5h://host.docker.internal:${HOST_SOCKS_PORT:-1080}" "$IP_ECHO"
# Linux: add --add-host=host.docker.internal:host-gateway
```

### 8. Bypass in SOCKS

If the destination **matches** a bypass rule (IP/CIDR/domain/`*.mask` from the UI or `BYPASS_*`), outline-gate dials **direct**, skipping Outline. Otherwise through the tunnel.

```bash
# example: add an exception in the UI, or:
# BYPASS_CIDRS=8.8.8.8/32
# BYPASS_RULES_FILE=/config/bypass.rules.txt  →  example.com
```

**Preset masks** (template → `config/bypass.rules.txt`):  
`*.max.ru`, `*.aq.ru`, `*.aq.local`, `*.aservice24.ru`, `*.yandex.cloud`, `*.yandex.ru`.

**Simple commands** (need `UI_ENABLE=true` + `UI_TOKEN`; port = `HOST_HEALTH_PORT`):

```bash
PORT=28080
AUTH=(-H "Authorization: Bearer $UI_TOKEN")

# list
curl -s "${AUTH[@]}" "http://127.0.0.1:${PORT}/api/v1/bypass"

# add
curl -s -X POST "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"rule":"*.example.com"}' "http://127.0.0.1:${PORT}/api/v1/bypass"

# delete
curl -s -X DELETE "${AUTH[@]}" \
  "http://127.0.0.1:${PORT}/api/v1/bypass?rule=*.example.com"

# block list (drop)
curl -s -X POST "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"rule":"*.ads.example"}' "http://127.0.0.1:${PORT}/api/v1/block"
```

Without UI: edit `deploy/compose/config/bypass.rules.txt` or `block.rules.txt` + `docker kill -s HUP outline-gate`.  
Details: [docs/OPERATIONS.md §7.2](docs/OPERATIONS.md).

### 9. SOCKS security

- **No SOCKS password** in v1 — do **not** publish `:1080` to the internet.
- Restrict firewall: LAN / `127.0.0.1` only.
- Optional: `SOCKS_ALLOW_CIDRS` (CSV) or `SOCKS_ALLOW_CIDRS_FILE` — **source IP** allowlist. Empty = accept all (as before).
- Do not commit `.env` with keys.

```bash
# loopback and LAN 10.0.0.0/8 only
SOCKS_ALLOW_CIDRS=127.0.0.0/8,10.0.0.0/8
```

---

## Using L3 gateway

L3 mode makes the host a **router**: LAN clients set **default gateway** (or a policy route) to the outline-gate machine. nftables decides: redirect TCP into the transparent proxy → Outline, or leave it direct.

### Prerequisites

- Linux host on the same L2/L3 network as the clients
- `GATEWAY_ENABLE=true`
- Usually: `docker compose -f docker-compose.host.yml` (`network_mode: host`)
- Capability `NET_ADMIN`, `ip_forward=1`
- Clients: IPv4 gateway = host LAN IP (example: `192.168.1.10`)

### Start (host network)

`deploy/compose/.env`:

```bash
OUTLINE_ACCESS_KEY=ss://...@server:port
GATEWAY_ENABLE=true
ROUTING_MODE=exclude          # or include
# LAN_INTERFACE=eth0          # optional, for MASQUERADE oif
HOST_HEALTH_PORT=28080        # on host network, ports are bound directly
UI_ENABLE=true
UI_TOKEN=your-secret
```

```bash
cd deploy/compose
docker compose -f docker-compose.host.yml up --build -d
curl -s http://127.0.0.1:8080/readyz   # host: HEALTH_LISTEN as-is
# or http://192.168.1.10:8080/readyz
```

### Client setup (Linux example)

```bash
# assume gate host: 192.168.1.10, client iface eth0
sudo ip route replace default via 192.168.1.10 dev eth0

# DNS (L3 does not "fix" DNS leaks by itself)
# router DNS, or 1.1.1.1 — pick on purpose
```

**Windows (GUI):**  
Settings → Network → adapter properties → IPv4 → Gateway: `192.168.1.10`.

**Android / iOS:**  
Wi-Fi static IP → Router / Gateway = host IP running outline-gate.

### L3 check

On a client (no SOCKS): the same [connectivity check](#connectivity-check) script, but take "direct" from a machine **outside** this gateway. With working exclude, the client's egress must not match that control machine's ISP. RFC1918 stays on the kernel path.

On the host:

```bash
docker logs outline-gate --tail=50
# nft table (host network):
sudo nft list table inet outline_gate
```

Stop / flush rules:

```bash
docker compose -f docker-compose.host.yml down
# after a hard kill:
sudo nft delete table inet outline_gate
```

---

### `exclude` mode (default)

<p align="center">
  <img src="docs/images/l3-exclude.svg" alt="L3 exclude mode" width="920"/>
</p>

**Meaning:** all TCP not in bypass → Outline tunnel.  
Fit: "VPN for the whole apartment, except LAN and chosen services".

```bash
ROUTING_MODE=exclude
GATEWAY_ENABLE=true
# extra exceptions optional:
BYPASS_CIDRS=203.0.113.0/24
# or via Web UI: example.com, *.cdn.example.net
```

**Logic:**

```text
if dst ∈ bypass (RFC1918 + UI + BYPASS_* + Outline server IP)
    → DIRECT
else
    → TUNNEL (nft REDIRECT → Outline)
```

**Practice: exclude a bank / internal API**

1. Web UI → add `bank.example.com` or `10.50.0.0/16`
2. Or file `/config/bypass.rules.txt`:

```text
bank.example.com
203.0.113.0/24
```

3. SIGHUP / UI apply / DNS refresh — the L3 set updates.

---

### `include` mode

<p align="center">
  <img src="docs/images/l3-include.svg" alt="L3 include mode" width="920"/>
</p>

**Meaning:** VPN **only** for addresses in `TUNNEL_*`. The rest is `DIRECT_POLICY` (`direct` or `drop`).  
Fit: "only foreign services / office CIDRs through Outline, the rest of the internet as before".

```bash
ROUTING_MODE=include
GATEWAY_ENABLE=true
TUNNEL_CIDRS=8.8.8.8/32,203.0.113.0/24
# or TUNNEL_CIDRS_FILE=/config/tunnel.txt
DIRECT_POLICY=direct
```

**Logic:**

```text
if dst ∈ bypass          → DIRECT
elif dst ∈ tunnel list   → TUNNEL
else                     → DIRECT_POLICY  # direct | drop
```

**Practice: only Google DNS and one subnet via VPN**

```bash
# .env
ROUTING_MODE=include
TUNNEL_CIDRS=8.8.8.8/32,8.8.4.4/32,203.0.113.0/24
DIRECT_POLICY=direct
GATEWAY_ENABLE=true
```

On a client with GW=gate, `traceroute` / `tcpdump` to addresses in `TUNNEL_CIDRS` is simpler than IP-echo: the echo service is almost certainly not in the include list.

**`DIRECT_POLICY=drop`:** everything that is not bypass and not tunnel is **cut** (hard allow-list). Easy to kill internet on clients if the tunnel list is incomplete.

```bash
DIRECT_POLICY=drop
TUNNEL_CIDRS=1.2.3.0/24
# client reaches 1.2.3.0/24 via Outline; 8.8.8.8 is drop
```

---

### L3: v1 limits

| Topic | Behaviour |
|-------|-----------|
| Protocols | TCP redirect; **UDP is not complete** |
| Domains in bypass | A/AAAA resolve + refresh (`BYPASS_DNS_REFRESH`); rare subdomains may briefly go into the tunnel |
| IPv6 | **not tunnelled** on L3: nft sets are `ipv4_addr`, IPv6 CIDR in bypass is skipped; on a dual-stack host IPv6 goes **around** Outline (direct). SOCKS IPv6 ATYP is rejected. Full dual-stack is roadmap |
| DNS | not "magic tunnel DNS"; configure DNS on clients separately |
| Always bypass | RFC1918, CGNAT, link-local, Outline server IP |

Details: [`docs/routing.md`](docs/routing.md).

---

## Web UI

<p align="center">
  <img src="docs/images/webui-mockup.svg" alt="outline-gate Web UI mockup" width="720"/>
</p>

### Sign-in: no default login/password

**There is no separate login/password** and **no default credentials** (`admin` / `password` do not exist).

| | Default |
|--|---------|
| Web UI | **off** (`UI_ENABLE=false`) |
| Login | **not used** |
| Password / token | **you set** `UI_TOKEN` |

```bash
# deploy/compose/.env
UI_ENABLE=true
UI_TOKEN=your-long-random-secret
HOST_HEALTH_PORT=28080
```

```bash
openssl rand -hex 24   # → UI_TOKEN
docker compose up -d --force-recreate
# http://127.0.0.1:28080/ui/  → "Access token" = UI_TOKEN
```

**API auth**

| Method | How |
|--------|-----|
| UI form | `UI_TOKEN` → sessionStorage → `Authorization: Bearer …` |
| curl | `Authorization: Bearer <UI_TOKEN>` |
| HTTP Basic | any username (`admin`), **password** = `UI_TOKEN` |

| URL | Auth | Purpose |
|-----|------|---------|
| `/ui/` | token for API | Web UI (tabs: Status · Log · Bypass · Key) |
| `GET /api/v1/status` | Bearer / Basic | ready / SOCKS / gateway / connlog summary |
| `GET /api/v1/connections` | Bearer / Basic | connection ring-buffer snapshot |
| `GET /api/v1/connections/stream` | Bearer / Basic / `?token=` | SSE live log (EventSource) |
| `GET/PUT /api/v1/outline` | Bearer / Basic | status / replace key |
| `GET/POST/DELETE /api/v1/bypass` | Bearer / Basic | exception rules |
| `GET/POST/DELETE /api/v1/block` | Bearer / Basic | block list (drop + log) |
| `/healthz`, `/readyz` | none | healthcheck |

**Connection log:** SOCKS and L3 show the chain `client → SOCKS|L3 → VPN|Direct → host` (and the bypass rule if known). On L3, private/RFC1918 stays on the kernel path with no log line; other Internet TCP (including user Direct) goes through the transparent proxy.

A key replaced in the UI is written to `OUTLINE_KEY_PERSIST_FILE` (default `/config/outline_key.runtime.txt`) and **wins** over `OUTLINE_ACCESS_KEY` on the next start.

---

## Environment variables

| Variable | Description |
|----------|-------------|
| `OUTLINE_ACCESS_KEY` / `OUTLINE_ACCESS_KEY_FILE` | Outline key `ss://` or `ssconf://` |
| `OUTLINE_KEY_PERSIST_FILE` | Key file after a UI replace |
| `ROUTING_MODE` | `exclude` \| `include` |
| `BYPASS_CIDRS` / `BYPASS_CIDRS_FILE` | Static CIDR exceptions |
| `BYPASS_RULES_FILE` | User rules (IP/domains) from the UI |
| `BLOCK_RULES_FILE` | Block list (IP/CIDR/domain/`*.mask`); drop + log |
| `TUNNEL_CIDRS` / `TUNNEL_CIDRS_FILE` | Targets (include) |
| `DIRECT_POLICY` | `direct` \| `drop` (include) |
| `GATEWAY_ENABLE` | L3 nftables |
| `UI_ENABLE` / `UI_TOKEN` | Web UI + API |
| `HOST_SOCKS_PORT` / `HOST_HEALTH_PORT` | Host ports (bridge compose) |
| `SOCKS_LISTEN` / `HEALTH_LISTEN` | Listeners in the container |
| `SOCKS_ALLOW_CIDRS` / `_FILE` | SOCKS source IP allowlist (empty = all; non-loopback → `Warn` at start) |
| `METRICS_ENABLE` | `true` → Prometheus text on `/metrics` (health port) |
| `SSCONF_REFRESH_INTERVAL` | How often to re-fetch `ssconf://` (default `2m`, `0` = off) |
| `TUNNEL_PROBE_ADDR` | TCP probe through the tunnel (default `1.1.1.1:443`, `off` = off) |
| `TUNNEL_PROBE_INTERVAL` / `_TIMEOUT` / `_FAILS` | Period / deadline / fail threshold before reconnect |
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` |

Full list: [`deploy/compose/.env.example`](deploy/compose/.env.example).

### Prometheus metrics

Optional **unauthenticated** endpoint on the same port as health/UI (`HEALTH_LISTEN` / `HOST_HEALTH_PORT`).

```bash
# deploy/compose/.env
METRICS_ENABLE=true

cd deploy/compose && docker compose up -d --force-recreate

curl -s http://127.0.0.1:${HOST_HEALTH_PORT:-8080}/metrics
# outline_gate_up 1
# outline_gate_connections_total{via="tunnel",result="ok"} …
```

The variable is passed through compose (`METRICS_ENABLE`). Do not publish the health port to the internet without a firewall — `/metrics` is as open as `/healthz`.

---

## Building the image

```bash
docker build -f deploy/docker/Dockerfile -t outline-gate:local .
```

The key is **not** baked into the image:

```bash
docker run --rm -d --name outline-gate \
  --cap-add=NET_ADMIN \
  -e OUTLINE_ACCESS_KEY='ss://...' \
  -e ROUTING_MODE=exclude \
  -e GATEWAY_ENABLE=false \
  -e UI_ENABLE=true \
  -e UI_TOKEN='change-me' \
  -p 1080:1080 -p 28080:8080 \
  -v "$PWD/deploy/compose/config:/config" \
  outline-gate:local
```

---

## Best practices

1. **Secrets** — `.env` / secrets / UI persist file; never git, never the image.
2. **UI_TOKEN** — long random; keep UI/API off the public internet without a TLS reverse-proxy.
3. **SOCKS `:1080`** — LAN / localhost only; no SOCKS auth in v1.
4. **L3** — always auto-bypass the Outline server IP; re-check after a key change.
5. **Domains on L3** — best-effort; use SOCKS for exact hostname match.
6. **Updates** — `docker compose up --build -d`; rules live in volume `./config`.
7. **DNS** — configure separately; L3 does not replace a DoH/DoT policy.

---

## Documentation

| Doc | Contents |
|-----|----------|
| **[docs/DEPLOY.md](docs/DEPLOY.md)** | Deploy on another host (step by step) · [Русский](docs/DEPLOY.ru.md) |
| **[docs/OPERATIONS.md](docs/OPERATIONS.md)** | Full deploy and operations reference · [Русский](docs/OPERATIONS.ru.md) |
| [docs/architecture.md](docs/architecture.md) | Component architecture |
| [docs/deployment.md](docs/deployment.md) | Network profiles A/B/C |
| [docs/routing.md](docs/routing.md) | Routing modes and bypass |
| [docs/images/](docs/images/) | Diagrams |
| [CHANGELOG.md](CHANGELOG.md) | Release history (Keep a Changelog) |
| [Releases](https://github.com/unhexx/outline-gate/releases) | Binaries and notes |
| [README.ru.md](README.ru.md) | Russian README |

## Repository

| Remote | URL | Default for release |
|--------|-----|---------------------|
| GitHub | https://github.com/unhexx/outline-gate | `main`, tags `v*` |
| aservice (origin) | https://git.aservice24.ru/scm/expert/outline-gate.git | `master`, tags `v*` |

```bash
# GitHub
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate
git checkout v0.6.0

# Internal
git clone https://git.aservice24.ru/scm/expert/outline-gate.git
cd outline-gate
git checkout v0.6.0
```

## Security

- SOCKS without auth — trusted network only (`SOCKS_ALLOW_CIDRS` recommended)
- Do not commit `.env`, `*.runtime.txt`, or real keys
- Keys in logs are redacted (`ss://***@host:port`)
- UI API without a token → `401` (`/api/v1/version` is public)
- v0.6 limits: TCP-first (UDP L3 incomplete), IPv6 nft gap, domain-bypass on L3 is best-effort

## License

[MIT](LICENSE)
