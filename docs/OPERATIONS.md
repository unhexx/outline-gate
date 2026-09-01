# outline-gate — deploy and operations reference

**English** · [Русский](OPERATIONS.ru.md)

**Release:** [v0.6.0](https://github.com/unhexx/outline-gate/releases/tag/v0.6.0) · [CHANGELOG](../CHANGELOG.md) · [README](../README.md)

> **Fast deploy on a new host:** see **[DEPLOY.md](DEPLOY.md)**  
> (`configure.sh` → `install.sh` → check). This document is the full reference.

| | |
|--|--|
| GitHub | https://github.com/unhexx/outline-gate |
| aservice | https://git.aservice24.ru/scm/expert/outline-gate.git |
| Release branch | `main` + tags `v*` |
| Deploy | [DEPLOY.md](DEPLOY.md) · [Русский](DEPLOY.ru.md) |

## 1. What this is

**outline-gate** is a Docker service that:

1. Connects to remote **Outline** (Shadowsocks) with access key `ss://...` or dynamic `ssconf://...`.
2. Serves local **SOCKS5** (`:1080`) for apps and LAN clients.
3. Optionally acts as an **L3 gateway** (nftables): LAN clients send traffic through the host; the service decides tunnel vs direct.
4. Serves a **Web UI** (`/ui/`) for the bypass list and Outline key replace (with `UI_ENABLE` + `UI_TOKEN`).

| `ROUTING_MODE` | Behaviour |
|----------------|-----------|
| `exclude` (default) | Tunnel everything **except** bypass (private nets + your list + Outline server IP) |
| `include` | Tunnel **only** addresses in `TUNNEL_*`; the rest is `direct` or `drop` |

---

## 2. Requirements

- Linux host (for L3) or any Docker host (for SOCKS)
- Docker Engine 20+ and Docker Compose v2
- Outline access key (`ss://...` or `ssconf://...`) from Outline Manager / provider
- Free host ports: **1080** (SOCKS), **health/UI** (`HOST_HEALTH_PORT`, often `8080` or `28080`)
- For L3: capability `NET_ADMIN`, preferably `network_mode: host`

---

## 3. Get the code

Short "from scratch" path is in **[DEPLOY.md](DEPLOY.md)**. Details below.

### 3.1. Release tag (recommended)

```bash
# GitHub
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate
git checkout v0.6.0

# or aservice
git clone https://git.aservice24.ru/scm/expert/outline-gate.git
cd outline-gate
git checkout v0.6.0
```

Linux amd64 binary (no Docker):

```bash
curl -fsSL -o outline-gate \
  https://github.com/unhexx/outline-gate/releases/download/v0.6.0/outline-gate_linux_amd64
chmod +x outline-gate
```

### 3.2. Clone the development branch

```bash
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate
# main is the development line; releases are git tags v*
```

---

## 4. Key and parameters

All working secrets live in `deploy/compose/.env` (**not** in git).

```bash
cd deploy/compose
cp .env.example .env
chmod +x configure.sh install.sh
./configure.sh
./install.sh          # build + up + /readyz
```

`configure.sh` asks for:

1. **Profile** — socks (bridge) / host (L3)
2. **Outline key** — in `.env` (`OUTLINE_ACCESS_KEY`) or in `secrets/outline_key.txt`
3. **ROUTING_MODE** — `exclude` / `include`
4. On `include` — `TUNNEL_CIDRS` list
5. Extra `BYPASS_CIDRS` (optional)
6. **UI_ENABLE** / **UI_TOKEN** (token can be generated)
7. `HOST_*` ports (bridge) and optional `SOCKS_ALLOW_CIDRS`
8. **LOG_LEVEL**

### Manual `.env`

```bash
# required
OUTLINE_ACCESS_KEY=ss://....@server:port

# common options
ROUTING_MODE=exclude
GATEWAY_ENABLE=false
COMPOSE_PROFILE=socks    # socks | host (for install.sh)
HOST_SOCKS_PORT=1080
HOST_HEALTH_PORT=28080   # health + Web UI on the host
UI_ENABLE=true
UI_TOKEN=long-random-secret
# SOCKS_ALLOW_CIDRS=192.168.0.0/16,10.0.0.0/8,127.0.0.0/8
LOG_LEVEL=info
```

Full variable list: `.env.example` and the table below.

### Alternative: key file only

```bash
printf '%s\n' 'ss://YOUR_KEY' > secrets/outline_key.local.txt
chmod 600 secrets/outline_key.local.txt
```

In `.env`:

```bash
OUTLINE_ACCESS_KEY=
OUTLINE_ACCESS_KEY_FILE=/run/secrets/outline_key
OUTLINE_KEY_HOST_PATH=./secrets/outline_key.local.txt
```

---

## 5. Build the Docker image

From the **repo root**:

```bash
docker build -f deploy/docker/Dockerfile -t outline-gate:local .
```

Or via Compose (builds on first `up`):

```bash
cd deploy/compose
docker compose build
```

The image does **not** contain the access key — the key is passed at run time.

### One-shot `docker run` (SOCKS)

```bash
docker run --rm -d --name outline-gate \
  --cap-add=NET_ADMIN \
  -e OUTLINE_ACCESS_KEY='ss://...' \
  -e ROUTING_MODE=exclude \
  -e GATEWAY_ENABLE=false \
  -e UI_ENABLE=true \
  -e UI_TOKEN='change-me' \
  -e LOG_LEVEL=info \
  -p 1080:1080 -p 28080:8080 \
  -v "$PWD/deploy/compose/config:/config" \
  outline-gate:v0.6.0
```

Extra parameters: any `-e NAME=value` (see the table).

---

## 6. Deploy profiles

### 6.1. Profile C — SOCKS (good first start)

Clients/apps point SOCKS5 at `HOST:1080`. No default-gateway change.

```bash
cd deploy/compose
# GATEWAY_ENABLE=false, COMPOSE_PROFILE=socks in .env
./install.sh
# or: docker compose up --build -d
docker compose ps
docker compose logs -f
```

Check (health port = `HOST_HEALTH_PORT`, default in example `28080`):

```bash
curl -s http://127.0.0.1:28080/healthz    # ok
curl -s http://127.0.0.1:28080/readyz     # {"ready":true,...}
curl -s --socks5h 127.0.0.1:1080 https://ifconfig.me
echo
```

The IP in the response should match Outline server egress (not the "home" IP, if the tunnel works).

### 6.2. Profile A — L3 gateway (host network)

The host becomes the LAN gateway. On clients: **default gateway = host IP**.

```bash
cd deploy/compose
# in .env:
#   COMPOSE_PROFILE=host
#   GATEWAY_ENABLE=true
#   ROUTING_MODE=exclude   # or include + TUNNEL_CIDRS
#   # LAN_INTERFACE=eth0   # if needed

./install.sh --host
# or: docker compose -f docker-compose.host.yml up --build -d
```

On a client (Linux example):

```bash
# HOST_IP — address of the machine running outline-gate on the LAN
sudo ip route replace default via HOST_IP
```

Windows/macOS/router: set default gateway / DHCP option 3.

**Important:** on a Docker bridge network the container is **not** a LAN gateway out of the box. For L3 use host (or macvlan — see `docs/deployment.md`).

SOCKS5 is **not** turned off with `--host`: the same process listens on `:1080`. That is "proxy and gateway together" (see [README quick start](../README.md#proxy-and-gateway-together)).

### 6.3. Proxy and gateway together

One container, host network, both listeners.

```bash
# in .env:
#   COMPOSE_PROFILE=host
#   GATEWAY_ENABLE=true
#   SOCKS_LISTEN=0.0.0.0:1080
#   HEALTH_LISTEN=0.0.0.0:8080
#   UI_ENABLE=true
#   UI_TOKEN=...

./install.sh --host
```

| Client | How it goes |
|--------|-------------|
| App with SOCKS | `HOST:1080` (`socks5h`) |
| LAN device without proxy | default gateway = host IP |
| Web UI | `http://HOST:8080/ui/` |

Do not run `docker-compose.yml` in parallel with the host profile.

### 6.4. Stop and clean up

```bash
cd deploy/compose
docker compose down
# or
docker compose -f docker-compose.host.yml down
```

On stop, nftables table `inet outline_gate` is removed by the process. If the process was killed hard:

```bash
sudo nft list tables
sudo nft delete table inet outline_gate   # if it is still there
```

---

## 7. Parameter reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `OUTLINE_ACCESS_KEY` | yes* | — | Key `ss://...` or `ssconf://...` |
| `OUTLINE_ACCESS_KEY_FILE` | yes* | — | Path to the key file in the container |
| `ROUTING_MODE` | no | `exclude` | `exclude` \| `include` |
| `BYPASS_CIDRS` | no | — | Static CIDR exceptions, CSV |
| `BYPASS_CIDRS_FILE` | no | `/config/bypass.txt` | CIDR file (one per line) |
| `BYPASS_RULES_FILE` | no | `/config/bypass.rules.txt` | UI user rules: IP/CIDR/domains/`*.mask` |
| `BLOCK_RULES_FILE` | no | `/config/block.rules.txt` | Block list: same rule types; drop + log |
| `BYPASS_DNS_REFRESH` | no | `60s` | Domain DNS refresh period for L3 |
| `UI_ENABLE` | no | `false` | Web UI `/ui/` and API |
| `UI_TOKEN` | if UI | — | Token (Bearer or Basic password) |
| `OUTLINE_KEY_PERSIST_FILE` | no | `/config/outline_key.runtime.txt` | Key after UI replace (wins at start) |
| `HOST_HEALTH_PORT` | no | `8080` | health/UI port on the host (bridge) |
| `TUNNEL_CIDRS` | for include | — | Tunnel targets, CSV |
| `TUNNEL_CIDRS_FILE` | for include | `/config/tunnel.txt` | CIDR file |
| `DIRECT_POLICY` | no | `direct` | `direct` \| `drop` (include) |
| `GATEWAY_ENABLE` | no | `false` / host:`true` | L3 nftables |
| `LAN_INTERFACE` | no | — | oif for MASQUERADE |
| `SOCKS_LISTEN` | no | `0.0.0.0:1080` | SOCKS5 |
| `SOCKS_ALLOW_CIDRS` | no | (empty = all) | SOCKS client source IP allowlist (CSV) |
| `SOCKS_ALLOW_CIDRS_FILE` | no | — | Source allowlist CIDR file (one per line) |
| `METRICS_ENABLE` | no | `false` | Prometheus text at `http://<health>/metrics` (no auth) |
| `HEALTH_LISTEN` | no | `0.0.0.0:8080` | Health HTTP |
| `TRANSPROXY_LISTEN` | no | `127.0.0.1:12345` | REDIRECT target |
| `LOG_LEVEL` | no | `info` | debug/info/warn/error |
| `LOG_FORMAT` | no | `text` | text/json |
| `RECONNECT_BASE_DELAY` | no | `1s` | backoff |
| `RECONNECT_MAX_DELAY` | no | `60s` | backoff cap |
| `SSCONF_REFRESH_INTERVAL` | no | `2m` | re-fetch `ssconf://`; `0` = off |
| `TUNNEL_PROBE_ADDR` | no | `1.1.1.1:443` | TCP probe through the tunnel; `off`/`none`/`-` = off |
| `TUNNEL_PROBE_INTERVAL` | no | `30s` | probe period |
| `TUNNEL_PROBE_TIMEOUT` | no | `8s` | one probe deadline |
| `TUNNEL_PROBE_FAILS` | no | `2` | consecutive fails → `Ready=false` + rebuild dialer |
| `HOST_SOCKS_PORT` | no | `1080` | publish on the host |
| `HOST_HEALTH_PORT` | no | `28080` (example) | publish health/UI on the host |
| `COMPOSE_PROFILE` | no | `socks` | `socks` \| `host` — which file `install.sh` uses |
| `IMAGE_TAG` | no | `outline-gate:local` | image tag |

\* You need **at least one** way to pass the key.

Enable metrics (bridge compose):

```bash
# in .env
METRICS_ENABLE=true
# recreate
docker compose up -d --force-recreate
curl -s "http://127.0.0.1:${HOST_HEALTH_PORT:-28080}/metrics" | head
```

List files: `deploy/compose/config/bypass.txt`, `bypass.rules.txt` (runtime, gitignored; template is `bypass.rules.example.txt`), `tunnel.txt` (mounted at `/config`, **rw** — UI writes rules).

### 7.1. Web UI (bypass + Outline key)

In `.env`:

```bash
UI_ENABLE=true
UI_TOKEN=long-random-secret
HOST_HEALTH_PORT=28080   # any free host port → container :8080
```

```bash
docker compose up --build -d
```

Open:

```text
http://HOST_IP:28080/ui/
```

Enter `UI_TOKEN`. In the UI:

1. **Outline key** — paste `ss://` / `ssconf://`, "Replace key" (reconnect + write `OUTLINE_KEY_PERSIST_FILE`).
2. **Bypass** — IP / CIDR / domain / `*.suffix`.
3. **Block** — same rule types; matches go neither VPN nor direct. In **Log**, filter "Block".

| Bypass example | Meaning |
|----------------|---------|
| `8.8.8.8` | one IP |
| `203.0.113.0/24` | subnet |
| `example.com` | exact domain |
| `*.cdn.example.net` | domain and subdomains (SOCKS; L3 — DNS apex) |

**Preset exceptions** (template `config/bypass.rules.example.txt` → runtime `bypass.rules.txt`):

```text
*.max.ru
*.aq.ru
*.aq.local
*.aservice24.ru
*.yandex.cloud
*.yandex.ru
```

### 7.2. Bypass exceptions: simple commands

Health/UI port = `HOST_HEALTH_PORT` (often `28080`). API needs `UI_ENABLE=true` and `UI_TOKEN`.

```bash
cd deploy/compose
PORT="${HOST_HEALTH_PORT:-28080}"
# UI_TOKEN from .env:
set -a; source .env; set +a
AUTH=(-H "Authorization: Bearer ${UI_TOKEN}")
```

**List rules**

```bash
curl -s "${AUTH[@]}" "http://127.0.0.1:${PORT}/api/v1/bypass" | jq .
```

**Add an exception** (IP, CIDR, domain, or `*.suffix`)

```bash
# one domain / mask
curl -s -X POST "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"rule":"*.example.com"}' \
  "http://127.0.0.1:${PORT}/api/v1/bypass"

# IP or subnet
curl -s -X POST "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"rule":"203.0.113.0/24"}' \
  "http://127.0.0.1:${PORT}/api/v1/bypass"
```

**Delete an exception**

```bash
# query parameter
curl -s -X DELETE "${AUTH[@]}" \
  "http://127.0.0.1:${PORT}/api/v1/bypass?rule=*.example.com"

# or JSON body
curl -s -X DELETE "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"rule":"*.example.com"}' \
  "http://127.0.0.1:${PORT}/api/v1/bypass"
```

**Block list** (same syntax; `via=drop` in the log):

```bash
curl -s "${AUTH[@]}" "http://127.0.0.1:${PORT}/api/v1/block"
curl -s -X POST "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"rule":"*.ads.example"}' \
  "http://127.0.0.1:${PORT}/api/v1/block"
curl -s -X DELETE "${AUTH[@]}" \
  "http://127.0.0.1:${PORT}/api/v1/block?rule=*.ads.example"
```

Priority: **block > bypass > VPN**. SOCKS answers `0x02` (not allowed by ruleset).

**Apply DNS resolve now** (L3 sets after domain changes)

```bash
curl -s -X POST "${AUTH[@]}" "http://127.0.0.1:${PORT}/api/v1/bypass/apply"
```

**Without UI/API — edit the file + reload**

```bash
# 1) edit the runtime file (mounted at /config)
$EDITOR deploy/compose/config/bypass.rules.txt
# 2) re-read rules without recreating the container
docker kill -s HUP outline-gate
# or:
docker compose kill -s HUP
```

One rule per line; `#` is a comment. Add by hand:

```bash
echo '*.new-service.example' >> deploy/compose/config/bypass.rules.txt
docker kill -s HUP outline-gate
```

Delete the line from the file (sed/editor) and `SIGHUP` again.

API (same token; port = `HOST_HEALTH_PORT`):

```bash
PORT=28080
# key status (redacted)
curl -s -H "Authorization: Bearer $UI_TOKEN" http://127.0.0.1:$PORT/api/v1/outline

# replace key
curl -s -X PUT -H "Authorization: Bearer $UI_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"access_key":"ss://...@host:port"}' \
  http://127.0.0.1:$PORT/api/v1/outline

# list / add / delete — see §7.2 above
```

`/healthz` and `/readyz` need **no** token (healthcheck).

**Key priority at start:** `OUTLINE_KEY_PERSIST_FILE` → `OUTLINE_ACCESS_KEY` → `OUTLINE_ACCESS_KEY_FILE`.

---

## 8. Operations

### 8.1. Status and logs

```bash
docker compose ps
docker compose logs -f --tail=200
curl -s http://127.0.0.1:8080/readyz | jq .
```

| Endpoint | Meaning |
|----------|---------|
| `GET /healthz` | process is alive |
| `GET /readyz` | Outline dialer ready (+ gateway rules, if enabled) |
| `GET /ui/` | Web UI exception list (if `UI_ENABLE=true`) |

### 8.2. Change key / mode

1. Edit `.env` or `secrets/…`
2. Recreate the container:

```bash
docker compose up -d --force-recreate
```

Reload CIDR lists without a full restart (if the process still sees the same volume files after the edit — you need SIGHUP):

```bash
docker kill -s HUP outline-gate
```

SIGHUP re-reads the **process** env (it does not necessarily re-read an updated `.env` from disk if compose already froze the variables). Safer: `compose up -d --force-recreate`.

### 8.3. Version upgrade

```bash
cd outline-gate
git fetch --tags
git checkout v0.6.0   # or: git pull
cd deploy/compose
./install.sh
```

### 8.4. Config backup

Save (outside git, in a secret store):

- `deploy/compose/.env`
- `deploy/compose/secrets/outline_key.local.txt` / `outline_key.txt` (if you use them)
- `deploy/compose/config/bypass.rules.txt` and other `config/*.txt`
- `deploy/compose/config/outline_key.runtime.txt` (key after UI)

Move to another host — **step 9** in [DEPLOY.md](DEPLOY.md).

### 8.5. Security

- SOCKS **without a password** — trusted LAN / firewall only; do not publish `:1080` to the internet.
- Do not commit `.env` or real keys.
- Keys in logs are redacted (`ss://***@host:port`).
- Restrict `:8080` if you do not need it from outside.
- With `UI_ENABLE=true` set a strong `UI_TOKEN`; API without a token returns 401. The UI page is public (no secrets on it); data changes only through the API with a token.

### 8.6. Common problems

| Symptom | Check |
|---------|-------|
| `missing access key` | `.env` / key file; `configure.sh` |
| `/readyz` 503 | key, path to Outline, `docker compose logs` |
| SOCKS timeout | firewall, `ROUTING_MODE`, is the Outline server alive |
| `/readyz` 200, SOCKS timeout | used to be a stuck `ssconf://` (dialer not re-fetched). Look for `ssconf endpoint changed` / `tunnel probe failed`; the process refresh/probes itself |
| L3 does not work | host profile? `GATEWAY_ENABLE=true`? default GW on clients? |
| Leftover nft rules | `sudo nft delete table inet outline_gate` |
| Routing loop | Outline server IP must be in bypass (added automatically) |

---

## 9. LAN clients

### SOCKS (Profile C)

| Client | Setup |
|--------|-------|
| curl | `curl --socks5 IP:1080 https://example.com` |
| Firefox | Settings → Network → Manual → SOCKS5 host/port, DNS through proxy optional |
| SSH | `ssh -o ProxyCommand='nc -X 5 -x IP:1080 %h %p' …` |
| System | proxychains / system SOCKS (OS-dependent) |

### L3 (Profile A)

- Default gateway → outline-gate host IP
- DNS: pick on purpose (DNS may "leak" around the tunnel; put resolvers in `TUNNEL`/`exclude` policy if needed)

---

## 10. Publish to git.aservice24.ru (checklist)

1. Create project `outline-gate` on `git.aservice24.ru`.
2. Locally:

```bash
git remote add origin git@git.aservice24.ru:GROUP/outline-gate.git
git push -u origin master
```

3. On the deploy server: clone → `configure.sh` → `./install.sh` (see [DEPLOY.md](DEPLOY.md)).
4. Confirm `.env`, keys, and runtime rules are **not** in the repo:

```bash
git check-ignore -v deploy/compose/.env deploy/compose/config/bypass.rules.txt
```

---

## 11. Related docs

- [DEPLOY.md](DEPLOY.md) — **deploy on another host (step by step)** · [Русский](DEPLOY.ru.md)
- [architecture.md](architecture.md) — component diagram
- [deployment.md](deployment.md) — profiles A/B/C
- [routing.md](routing.md) — routing modes
- [design-plan.md](design-plan.md) — full design / PR DAG
- [README.md](../README.md) — product overview · [Русский](../README.ru.md)
