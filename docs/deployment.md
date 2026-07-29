# Deployment

**Version:** [v0.4.0](https://github.com/unhexx/outline-gate/releases/tag/v0.4.0) · full guide (RU): [OPERATIONS.ru.md](OPERATIONS.ru.md) · new-host steps (RU): [DEPLOY.ru.md](DEPLOY.ru.md)

One-shot on a fresh host (after clone):

```bash
cd deploy/compose
./configure.sh   # key + profile + UI token
./install.sh     # build, up, wait for /readyz
```

## Prerequisites

- Docker Engine + Compose plugin
- Outline access key (`ss://...` or `ssconf://...`) from Outline Manager / provider
- For L3 gateway: ability to set default gateway on LAN clients; `NET_ADMIN` capability

## Secrets

```bash
mkdir -p deploy/compose/secrets
echo -n 'ss://...' > deploy/compose/secrets/outline_key.txt
chmod 600 deploy/compose/secrets/outline_key.txt
```

Prefer `.env` (gitignored) or a host path via `OUTLINE_KEY_HOST_PATH`. Keys replaced in the Web UI go to `OUTLINE_KEY_PERSIST_FILE` (default `/config/outline_key.runtime.txt`) and take priority on next start.

## Profile A — host network (L3 gateway)

Best when outline-gate runs on an always-on Linux box on the LAN.

```bash
cd deploy/compose
# .env: GATEWAY_ENABLE=true, OUTLINE_ACCESS_KEY=..., optionally UI_*
docker compose -f docker-compose.host.yml up --build -d
```

On clients:

- IPv4 gateway = host LAN IP
- DNS = host or public resolvers (document leaks if DNS is not tunnelled)

Verify:

```bash
curl -s --socks5h HOST_IP:1080 https://ifconfig.me
curl -s http://HOST_IP:8080/readyz
# UI (if UI_ENABLE=true): http://HOST_IP:8080/ui/
```

On stop, rules table `inet outline_gate` is removed. If the process crashes, re-run or:

```bash
sudo nft delete table inet outline_gate
```

## Profile B — macvlan (container with LAN IP)

Create a macvlan network (example):

```bash
docker network create -d macvlan \
  --subnet=192.168.1.0/24 \
  --gateway=192.168.1.1 \
  -o parent=eth0 lan
```

Attach the service to `lan` with a static IP, set `GATEWAY_ENABLE=true`, point clients at the container IP.

## Profile C — bridge SOCKS only

Default `docker-compose.yml`: `GATEWAY_ENABLE=false`, publish `1080` / health (`HOST_HEALTH_PORT` → container `8080`).  
Apps configure SOCKS5 to `host:1080`. No default-gateway change required.

Bridge network `outline-gate_net` uses **explicit IPAM** (`COMPOSE_SUBNET` default `192.168.102.0/24`) so Docker does not allocate from `default-address-pools`. This matches the recommended host daemon layout in `deploy/docker/daemon.json.example`:

| Range | Role |
|-------|------|
| `192.168.100.0/24` | docker0 (`bip` / `fixed-cidr`) |
| `192.168.101.0/24` | auto networks (other compose projects) |
| `192.168.102.0/24` | outline-gate (this compose file) |

```bash
# from repo root
./install.sh 'ss://...'
curl -s --socks5h 127.0.0.1:1080 https://ifconfig.me
curl -s http://127.0.0.1:28080/readyz
```

## Install from release tag

```bash
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate && git checkout v0.4.0
docker build -f deploy/docker/Dockerfile -t outline-gate:v0.4.0 .
```

Binary (no Docker):

```text
https://github.com/unhexx/outline-gate/releases/download/v0.4.0/outline-gate_linux_amd64
```

## Firewall

- Restrict `:1080` to LAN if exposed
- Do not publish SOCKS to the public Internet without auth (v0.4.0 has no SOCKS auth)
- Restrict health/UI port when `UI_ENABLE=true`; protect with strong `UI_TOKEN` (+ reverse-proxy TLS if exposed beyond LAN)
