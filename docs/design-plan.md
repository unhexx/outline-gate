# Outline Gate — Design & Implementation Plan

## Context

**Проблема.** Нужен сетевой шлюз в Docker, который:
1. Подключается к удалённому Outline (Shadowsocks) серверу с access key `ss://...`, переданным при старте контейнера.
2. Выступает **LAN-шлюзом**: другие хосты локальной сети маршрутизируют трафик через него.
3. Реализует **split tunneling** в одном из двух режимов:
   - **exclude (bypass)**: весь трафик через туннель, кроме адресов/подсетей из списка исключений;
   - **include (only)**: через туннель только адреса/подсети из списка; остальное — напрямую (или drop — по политике).

**Текущее состояние репозитория.** Пустой git-репозиторий (`master`, без коммитов). Проект с нуля.

**Цель этого документа.** Дать достаточно детальный план для декомпозиции и параллельной разработки agentic-loop (design → PR DAG → implement → review).

---

## Goals / Non-Goals

### Goals
- Один сервис `outline-gate` в Docker Compose с параметрами через env/файлы.
- Стабильное активное подключение к Outline (reconnect, health).
- L3-шлюз для LAN (ip_forward + NAT + policy routing / transparent proxy).
- Два режима маршрутизации: `exclude` и `include`.
- Конфиг списков CIDR/IP (env, volume-mounted file).
- Наблюдаемость: health endpoint/logs, exit codes, docker healthcheck.
- Документация: compose example, network setup на клиентах LAN, security notes.

### Non-Goals (v1)
- GUI Outline Client / Electron.
- Outline **Server** / Manager API (мы только **клиент**).
- Multi-upstream failover pool (можно roadmap).
- WireGuard/OpenVPN overlay между LAN-клиентами.
- Web UI управления (CLI + env достаточно для v1).
- DNS-over-tunnel как отдельный сложный продукт (базовый DNS policy — да).

---

## Key Decisions (рекомендации)

| # | Решение | Выбор | Почему |
|---|---------|-------|--------|
| KD-1 | Стек | **Go + outline-sdk** | Официальный путь Outline; `ss://` access keys; example `outline-cli`; один бинарь в образе |
| KD-2 | Модель шлюза | **L3 gateway** (forward + NAT) + опционально **SOCKS5** | LAN-роутинг «как шлюз» — основной UX; SOCKS — для приложений без смены default GW |
| KD-3 | Механизм туннелирования | **TUN + tun2socks-подобный путь** *или* **ss-redir + nftables REDIRECT** | Рекомендация v1: **nftables + redir/socks bridge** на базе outline-sdk dialer + local transparent proxy; TUN — фаза 2 если redir не покрывает UDP |
| KD-4 | Split tunnel | **nftables sets + policy** | exclude/include = два режима membership в set `tunnel_targets` / `bypass` |
| KD-5 | Конфиг | Env + optional YAML/CIDR list files | Compose-first: секреты через env/`secrets`, списки — volume |
| KD-6 | Привилегии контейнера | `cap_add: [NET_ADMIN]`, `sysctls` ip_forward, `/dev/net/tun` при TUN | Без host network по умолчанию; отдельная user-defined bridge/macvlan — документировать |
| KD-7 | Язык/репо layout | Monorepo Go module `github.com/.../outline-gate` | Простая структура для agentic PR stack |

**Рекомендация по KD-3 (уточнение):**  
Для v1 предпочтителен путь:
1. Local SOCKS5 (outline-sdk transport) на `127.0.0.1:1080`.
2. `gost`/`tun2socks`/`redsocks` **или** собственный transparent proxy (TPROXY/REDIRECT) в Go.
3. nftables: PREROUTING → mark/redirect для matching traffic; POSTROUTING MASQUERADE для LAN.

Минимально жизнеспособный v1 без тяжёлых зависимостей:
- outline-sdk dialer + SOCKS5 server в процессе;
- **nftables + TPROXY/REDIRECT** для TCP (и UDP TPROXY если реализуемо);
- документированный fallback: LAN-клиенты явно указывают SOCKS5 шлюза.

---

## Architecture

```
                    ┌─────────────────────────────────────────────┐
                    │              Docker host / LAN              │
 LAN client A ──────┤                                             │
 (default GW /      │   docker network (bridge/macvlan/host)      │
  static route)     │                                             │
 LAN client B ──────┤         ┌───────────────────────┐           │
                    │         │   outline-gate        │           │
                    │         │  ┌─────────────────┐  │           │
                    │  eth0──►│  │ Gateway engine  │  │           │
                    │         │  │ nftables/policy │  │           │
                    │         │  └────────┬────────┘  │           │
                    │         │           │           │           │
                    │         │  ┌────────▼────────┐  │    ss://  │
                    │         │  │ Outline client  ├──────────────┼──► Outline Server
                    │         │  │ (outline-sdk)   │  │   tunnel  │
                    │         │  └─────────────────┘  │           │
                    │         │  ┌─────────────────┐  │           │
                    │         │  │ SOCKS5 :1080    │◄─┼── optional│
                    │         │  │ Health  :8080   │  │   clients │
                    │         │  └─────────────────┘  │           │
                    │         └───────────────────────┘           │
                    └─────────────────────────────────────────────┘
```

### Components

| Component | Responsibility |
|-----------|----------------|
| `cmd/outline-gate` | Entrypoint: parse config, start subsystems, signal handling |
| `internal/outline` | Connect/reconnect via outline-sdk; parse `ss://` / `ssconf://` |
| `internal/proxy` | Local SOCKS5 (and optional HTTP CONNECT) over outline dialer |
| `internal/gateway` | Enable forwarding, nftables rules, MASQUERADE, marks |
| `internal/routing` | Build sets from exclude/include lists; apply mode |
| `internal/health` | `/healthz`, `/readyz` (tunnel up + rules applied) |
| `internal/config` | Env + file loaders, validation |
| `deploy/docker` | Dockerfile multi-stage, entrypoint.sh (sysctl, wait) |
| `deploy/compose` | `docker-compose.yml`, `.env.example`, sample lists |

### Data flow (L3)

1. LAN client sends packet to internet with next-hop = gate IP.
2. Container receives on data interface; ip_forward=1.
3. **Routing policy:**
   - **exclude:** if dst ∈ bypass_set → normal route (direct/host); else → tunnel path.
   - **include:** if dst ∈ tunnel_set → tunnel path; else → direct (or drop if `DIRECT_POLICY=drop`).
4. Tunnel path: REDIRECT/TPROXY to local transparent proxy → outline-sdk → remote.
5. Return path: conntrack + MASQUERADE so replies return to LAN clients.

### Critical path: Outline server must never be tunnelled to itself
Always inject Outline server IP(s) into bypass set (auto from access key). Failure = blackhole loop.

---

## Configuration Model

### Environment variables (compose)

| Variable | Required | Description | Example |
|----------|----------|-------------|---------|
| `OUTLINE_ACCESS_KEY` | yes* | `ss://` or `ssconf://` key | `ss://Y2hhY2hh...@host:port` |
| `OUTLINE_ACCESS_KEY_FILE` | yes* | Path to file with key (Docker secret) | `/run/secrets/outline_key` |
| `ROUTING_MODE` | yes | `exclude` \| `include` | `exclude` |
| `BYPASS_CIDRS` | no | Comma-separated (exclude mode + always-local) | `192.168.0.0/16,10.0.0.0/8` |
| `BYPASS_CIDRS_FILE` | no | One CIDR per line | `/config/bypass.txt` |
| `TUNNEL_CIDRS` | no | Comma-separated (include mode) | `1.2.3.0/24,8.8.8.8/32` |
| `TUNNEL_CIDRS_FILE` | no | One CIDR per line | `/config/tunnel.txt` |
| `DIRECT_POLICY` | no | `direct` \| `drop` (include mode non-match) | `direct` |
| `LAN_INTERFACE` | no | Interface facing LAN (auto-detect default) | `eth0` |
| `GATEWAY_ENABLE` | no | Enable L3 gateway rules | `true` |
| `SOCKS_LISTEN` | no | SOCKS5 bind | `0.0.0.0:1080` |
| `HEALTH_LISTEN` | no | Health HTTP | `0.0.0.0:8080` |
| `LOG_LEVEL` | no | `debug`/`info`/`warn`/`error` | `info` |
| `RECONNECT_BASE_DELAY` | no | Backoff start | `1s` |
| `RECONNECT_MAX_DELAY` | no | Backoff cap | `60s` |
| `DNS_MODE` | no | `system` \| `tunnel` \| `static` | `system` |
| `DNS_SERVERS` | no | For static/tunnel assist | `1.1.1.1,8.8.8.8` |

\* One of `OUTLINE_ACCESS_KEY` / `OUTLINE_ACCESS_KEY_FILE` required.

### Default bypass (always applied)
- RFC1918 + link-local + CGNAT + loopback (unless explicitly overridden with care)
- Outline server endpoint IP(s)
- Docker/bridge networks if detected

### Example `docker-compose.yml` (sketch)

```yaml
services:
  outline-gate:
    image: outline-gate:local
    build: .
    cap_add:
      - NET_ADMIN
    sysctls:
      - net.ipv4.ip_forward=1
      # optional IPv6 later
    environment:
      OUTLINE_ACCESS_KEY_FILE: /run/secrets/outline_key
      ROUTING_MODE: exclude
      BYPASS_CIDRS_FILE: /config/bypass.txt
      GATEWAY_ENABLE: "true"
      SOCKS_LISTEN: "0.0.0.0:1080"
      HEALTH_LISTEN: "0.0.0.0:8080"
      LOG_LEVEL: info
    secrets:
      - outline_key
    volumes:
      - ./config:/config:ro
    ports:
      - "1080:1080"   # SOCKS for LAN apps
      - "8080:8080"   # health
    # network_mode: host  # optional doc path for true L3 gateway simplicity
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8080/readyz"]
      interval: 15s
      timeout: 3s
      retries: 3

secrets:
  outline_key:
    file: ./secrets/outline_key.txt
```

**Networking note (важно для L3):**  
На bridge network контейнер **не** становится default GW для LAN «из коробки». Документировать три deployment profiles:

| Profile | Use case | Notes |
|---------|----------|-------|
| **A. host network** | Простейший L3 gateway на домашнем сервере | `network_mode: host`, cap NET_ADMIN; LAN clients → host IP |
| **B. macvlan/ipvlan** | Контейнер со своим IP в LAN | Clients → container IP as GW |
| **C. SOCKS-only** | Без L3, только proxy | Минимальные привилегии; apps point to `:1080` |

v1 **must** support A + C; B documented as recommended for non-host Docker.

---

## Routing Modes (детально)

### Mode `exclude` (default)
```
if dst in ALWAYS_BYPASS ∪ BYPASS_CIDRS ∪ outline_server:
    direct
else:
    via_tunnel
```

### Mode `include`
```
if dst in ALWAYS_BYPASS ∪ outline_server:
    direct   # never loop
elif dst in TUNNEL_CIDRS:
    via_tunnel
else:
    DIRECT_POLICY  # direct | drop
```

### Implementation sketch (nftables)

```
table inet outline_gate {
  set bypass { type ipv4_addr; flags interval; }
  set tunnel { type ipv4_addr; flags interval; }

  chain prerouting {
    type nat hook prerouting priority dstnat;
    # exclude mode: redirect if NOT in bypass
    # include mode: redirect if in tunnel
  }
  chain postrouting {
    type nat hook postrouting priority srcnat;
    oifname "eth0" masquerade
  }
}
```

Rules rebuild atomically on config reload (SIGHUP) or file watch.

### UDP
- Phase 1: TCP-first (SOCKS + REDIRECT TCP); UDP best-effort (TPROXY or document limitation).
- Phase 2: full UDP via TPROXY or TUN.

Outline/Shadowsocks UDP support depends on server and transport; design for graceful degradation.

---

## Runtime Lifecycle

```
start
  → load & validate config
  → parse access key → resolve server endpoint → seed bypass
  → start health (not ready)
  → connect outline transport (with backoff)
  → start SOCKS5
  → if GATEWAY_ENABLE: apply nftables + forwarding
  → ready=true
  → watch: reconnect, SIGHUP reload lists, SIGTERM teardown (flush rules)
```

**Teardown must** remove nftables table to avoid host residual rules (especially host network).

---

## Repository Layout

```
outline-gate/
├── README.md
├── LICENSE
├── go.mod
├── go.sum
├── cmd/
│   └── outline-gate/
│       └── main.go
├── internal/
│   ├── config/
│   ├── outline/
│   ├── proxy/
│   ├── gateway/
│   ├── routing/
│   ├── health/
│   └── logging/
├── deploy/
│   ├── docker/
│   │   ├── Dockerfile
│   │   └── entrypoint.sh
│   └── compose/
│       ├── docker-compose.yml
│       ├── docker-compose.host.yml
│       ├── .env.example
│       └── config/
│           ├── bypass.example.txt
│           └── tunnel.example.txt
├── scripts/
│   ├── smoke-test.sh
│   └── e2e-netns.sh
├── docs/
│   ├── architecture.md
│   ├── deployment.md
│   └── routing.md
└── test/
    └── integration/
```

---

## Security

- Access key **only** via secret file or env; never bake into image.
- SOCKS bind: default `0.0.0.0` only if intentional; document firewall.
- No auth on SOCKS in v1 → **LAN trust model**; optional user/pass roadmap.
- Drop capabilities not needed; avoid full `privileged: true` if NET_ADMIN + tun suffices.
- Health endpoint: no secrets; localhost-friendly.
- Log redaction: never log full access key (mask userinfo).
- Image: distroless or alpine minimal; non-root where possible (root often required for nft — document; or setcap).

---

## Observability

- Structured logs (JSON optional via `LOG_FORMAT`).
- Metrics (optional v1.1): Prometheus `:9090` — bytes in/out, reconnects, rule reloads.
- `/healthz` — process up.
- `/readyz` — tunnel connected + (if gateway) rules applied.
- Docker healthcheck on `/readyz`.

---

## Testing Strategy

| Level | What |
|-------|------|
| Unit | config parse, CIDR merge, mode decision table, key redaction |
| Component | mock dialer → SOCKS CONNECT; nftables rule builder dry-run |
| Integration | docker compose + netns clients; curl via SOCKS; exclude/include matrix |
| Smoke | `scripts/smoke-test.sh` against real or mock Outline (optional CI secret) |
| Manual e2e | LAN phone/PC with GW set; verify IP leak sites |

**Decision table tests (must):**

| mode | dst | expected |
|------|-----|----------|
| exclude | public IP not in bypass | tunnel |
| exclude | 10.0.0.1 | direct |
| exclude | outline server IP | direct |
| include | listed IP | tunnel |
| include | unlisted public | direct/drop |
| include | outline server | direct |

---

## PR Plan (DAG for agentic-loop)

Зависимости: стрелка `→` = «после».

### PR-01: Scaffold & CI skeleton
- **Scope:** go.mod, `cmd/outline-gate` stub, Makefile, `.gitignore`, README skeleton, GitHub Actions (build/test).
- **Deps:** none
- **Acceptance:** `go test ./...` passes; binary builds.

### PR-02: Config loader
- **Scope:** `internal/config` — env, files, validation, ROUTING_MODE, CIDR lists, defaults (RFC1918 bypass).
- **Deps:** PR-01
- **Acceptance:** unit tests for all required/optional combos; fail-fast on invalid mode/empty include list when mode=include.

### PR-03: Outline client core
- **Scope:** `internal/outline` — parse `ss://`, dial via outline-sdk, reconnect with backoff, status API.
- **Deps:** PR-01
- **Acceptance:** unit tests with fake transport; integration test optional (skipped without key).

### PR-04: Local SOCKS5 proxy
- **Scope:** `internal/proxy` — SOCKS5 over outline dialer; TCP; graceful shutdown.
- **Deps:** PR-03
- **Acceptance:** integration test with local mock; curl `--socks5` works when key present.

### PR-05: Routing decision engine
- **Scope:** `internal/routing` — pure logic exclude/include + always-bypass + server IP injection.
- **Deps:** PR-02
- **Acceptance:** exhaustive table-driven tests (see above).

### PR-06: Gateway / nftables engine
- **Scope:** `internal/gateway` — apply/flush rules, MASQUERADE, REDIRECT/TPROXY to local port; host cleanup on exit.
- **Deps:** PR-05, PR-04
- **Acceptance:** dry-run + privileged integration test in CI (or documented manual); no residual rules after stop.

### PR-07: Health, signals, main wiring
- **Scope:** health endpoints, SIGHUP reload, SIGTERM teardown, main orchestration.
- **Deps:** PR-02…PR-06
- **Acceptance:** `/readyz` reflects tunnel; reload updates sets without restart.

### PR-08: Docker image & Compose
- **Scope:** multi-stage Dockerfile, entrypoint, compose profiles (host / bridge SOCKS / example macvlan docs), secrets example.
- **Deps:** PR-07
- **Acceptance:** `docker compose up --build` healthy with dummy/mock or real key; docs for network profiles.

### PR-09: Docs & smoke/e2e scripts
- **Scope:** architecture, deployment, routing docs; `scripts/smoke-test.sh`, netns e2e.
- **Deps:** PR-08
- **Acceptance:** README quickstart works end-to-end for profile A and C.

### PR-10 (optional): UDP / TUN path
- **Scope:** improve UDP; optional TUN interface.
- **Deps:** PR-06
- **Acceptance:** documented UDP matrix; tests if feasible.

```
PR-01 ─┬─► PR-02 ─► PR-05 ─┐
       │                   ├─► PR-06 ─► PR-07 ─► PR-08 ─► PR-09
       └─► PR-03 ─► PR-04 ─┘              │
                                          └─► PR-10 (optional)
```

**Parallelism for agents:**
- After PR-01: agents can take **PR-02** and **PR-03** in parallel.
- After those: **PR-04** ‖ **PR-05**.
- Then **PR-06** (needs both paths), then **PR-07→08→09** sequential.

---

## Agentic-Loop Execution Playbook

1. **Design freeze** — this plan (+ resolve Open Questions below).
2. **Graphite/stacked PRs** or feature branches per PR-0N.
3. Per PR: TDD where pure logic (config, routing); integration for network.
4. Orchestrator agents: implement → unit/integration → review skill → merge order by DAG.
5. Final gate: compose profile A smoke + include/exclude matrix manual or netns.

### Suggested agent roles
| Role | Owns |
|------|------|
| Scaffold agent | PR-01, CI |
| Config/routing agent | PR-02, PR-05 |
| Outline/proxy agent | PR-03, PR-04 |
| Net/gateway agent | PR-06 (needs NET_ADMIN expertise) |
| Integration agent | PR-07, PR-08, PR-09 |
| Reviewer agent | every PR before merge |

---

## Open Questions (defaults proposed)

| ID | Question | Proposed default |
|----|----------|------------------|
| OQ-1 | Только IPv4 или IPv6 в v1? | **IPv4 only** in v1; IPv6 roadmap |
| OQ-2 | Default deployment profile? | Document **host network (A)** as primary L3; SOCKS (C) always on |
| OQ-3 | Transparent proxy tool: in-process Go vs redsocks/tun2socks sidecar? | **In-process** (fewer moving parts); sidecar only if blocked |
| OQ-4 | SOCKS auth in v1? | **No** (LAN trust); document risk |
| OQ-5 | DNS: force through tunnel? | **system** default; optional static DNS via tunnel in include for tunnel CIDRs only |
| OQ-6 | Base image? | **alpine** multi-stage with nftables binary/tools |
| OQ-7 | Project module path / license? | User choice; MIT + local module path until remote set |

Если defaults приемлемы — реализация стартует без блокировок.

---

## Verification (end-to-end definition of done)

1. `docker compose -f deploy/compose/docker-compose.host.yml up --build -d` → healthy.
2. From another host: `curl --socks5 <gate>:1080 https://ifconfig.me` → Outline egress IP.
3. Mode exclude: traffic to `BYPASS` IP goes with real ISP IP; rest via Outline.
4. Mode include: only listed destinations via Outline; others direct.
5. Kill Outline server connectivity → reconnect logs + `/readyz` fails → recovers.
6. `docker compose down` → no leftover nftables table `outline_gate` on host.
7. Access key never appears in logs or image layers.

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Docker bridge ≠ L3 gateway | Explicit deployment profiles; host/macvlan docs first-class |
| Routing loop to Outline server | Auto-bypass server IPs; tests |
| Residual nft rules on crash | Defer cleanup; best-effort on start “replace table”; document |
| UDP/VoIP broken | TCP-first; document; PR-10 |
| outline-sdk API churn | Pin module version; thin adapter in `internal/outline` |
| Legal/ToS of upstream | User-provided key; we don't ship keys or servers |

---

## Roadmap (post-v1)

- IPv6, SOCKS auth, multi-upstream failover  
- Prometheus metrics, Web UI for lists  
- Kubernetes DaemonSet / hostNetwork chart  
- Full TUN path for all protocols  
- `ssconf://` dynamic config refresh  

---

## Summary for agents

**Outline Gate** — Go-сервис в Docker: клиент Outline (outline-sdk) + локальный SOCKS5 + L3 gateway (nftables split tunnel в режимах exclude/include). Конфиг из env/compose. Разработка — 9–10 PR по DAG; параллель config/routing vs outline/proxy после scaffold.

**Critical files to create (no existing code):**  
`cmd/outline-gate/main.go`, `internal/{config,outline,proxy,gateway,routing,health}/`, `deploy/docker/Dockerfile`, `deploy/compose/docker-compose*.yml`, `docs/*`, `scripts/*`.

**Reuse:** Go stdlib `net/http`, `log/slog`; **outline-sdk** (`golang.getoutline.org/sdk`); system **nftables** (via `google/nftables` Go lib or exec `nft -f`); optional `github.com/things-go/go-socks5` or minimal SOCKS5.

---

*Document status: implementation-ready plan for agentic decomposition. No application code in repo yet.*
