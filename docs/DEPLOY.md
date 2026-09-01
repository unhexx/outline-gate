# Deploy outline-gate on another host

**English** · [Русский](DEPLOY.ru.md)

**Release:** [v0.6.0](https://github.com/unhexx/outline-gate/releases/tag/v0.6.0) · operations: [OPERATIONS.md](OPERATIONS.md)

| Resource | URL |
|----------|-----|
| GitHub | https://github.com/unhexx/outline-gate |
| Internal (aservice) | https://git.aservice24.ru/scm/expert/outline-gate.git |
| Branch | `main` / `master` |

---

## Fast install (2 steps)

Needs: Docker Engine + Compose v2, Outline key (`ss://` or `ssconf://`).

```bash
# 1) code
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate

# 2) run (creates .env, net 192.168.102.0/24, build+up, /readyz)
./install.sh 'ss://YOUR_KEY_HERE'
```

Check:

```bash
curl -s http://127.0.0.1:28080/readyz
curl -s --socks5h 127.0.0.1:1080 https://ifconfig.me
```

L3 gateway (host network):

```bash
./install.sh --host 'ss://YOUR_KEY_HERE'
```

### Proxy and gateway together

`./install.sh --host` is not "L3 only". The same container listens on SOCKS5 `:1080` and installs the nft gateway for the LAN.

```bash
./install.sh --host 'ss://YOUR_KEY_HERE'
```

In `.env`: `COMPOSE_PROFILE=host`, `GATEWAY_ENABLE=true`. SOCKS stays on.

```bash
# proxy (apps on the host / LAN)
curl -s --socks5h 127.0.0.1:1080 https://ifconfig.me
# gateway: on the client, default gateway = this host's IP
# UI (UI_ENABLE=true): http://HOST_IP:8080/ui/
```

Do not start bridge and host compose at the same time: one container name, ports collide.

Update on an already installed host:

```bash
git pull
./install.sh                  # key already in deploy/compose/.env
# or: ./install.sh --no-build
```

---

## Docker daemon on the host (recommended)

On nodes with a **narrow** `default-address-pools` (one `/24`) Docker auto-networks run out fast
(`all predefined address pools have been fully subnetted`).

Recommended `/etc/docker/daemon.json` (see `deploy/docker/daemon.json.example`):

```json
{
  "bip": "192.168.100.1/24",
  "fixed-cidr": "192.168.100.0/24",
  "default-address-pools": [
    { "base": "192.168.101.0/24", "size": 24 }
  ]
}
```

| Range | Role |
|-------|------|
| `192.168.100.0/24` | docker0 (`bip` / `fixed-cidr`) |
| `192.168.101.0/24` | auto networks of other compose projects |
| `192.168.102.0/24` | **outline-gate** — explicit IPAM in `docker-compose.yml` (not taken from the pool) |

After editing the daemon: `sudo systemctl restart docker`.

Override the outline-gate subnet: `COMPOSE_SUBNET` / `COMPOSE_GATEWAY` in `deploy/compose/.env`.

---

## What you get

1. **outline-gate** container (`ss://` / `ssconf://`).
2. **SOCKS5** on `:1080` (bridge or host network).
3. **L3 gateway together with SOCKS** (`./install.sh --host`): LAN default-GW + `:1080`.
4. Optional **Web UI** — after install: `UI_ENABLE=true` + `UI_TOKEN` in `.env`, then `./install.sh --no-build`.

Secrets are **not** in git.

---

## Requirements

| Requirement | Minimum |
|-------------|---------|
| OS | Linux x86_64 |
| Docker | Engine 20+ + **Compose v2** |
| Ports | `1080` (SOCKS), `28080` (health; host profile — `8080`) |
| Key | `ss://...` or `ssconf://...` |

```bash
docker --version && docker compose version
```

---

## Profiles

| Profile | Compose | Command |
|---------|---------|---------|
| **socks** (bridge) | `docker-compose.yml` | `./install.sh 'ss://...'` |
| **host** (SOCKS + L3) | `docker-compose.host.yml` | `./install.sh --host 'ss://...'` |

---

## Interactive / manual setup

```bash
./install.sh --configure          # wizard → .env → up
# or
cd deploy/compose && cp .env.example .env && $EDITOR .env && ./install.sh
```

Runtime files:

| Path | Purpose |
|------|---------|
| `deploy/compose/.env` | env (key, ports, `COMPOSE_SUBNET`) |
| `secrets/outline_key.txt` | optional key file |
| `config/bypass.rules.txt` | UI rules |
| `config/outline_key.runtime.txt` | key after a UI replace |

Flags:

```bash
./install.sh --configure | --host | --socks | --check | --down | --no-build
```

---

## Step 5. Verify

Default ports: SOCKS `1080`, health/UI `28080` (bridge) or `8080` (host).

```bash
# tunnel ready
curl -s http://127.0.0.1:28080/readyz
# {"ready":true,...}

# egress via Outline (IP must be VPN egress, not "home")
curl -s --socks5h 127.0.0.1:1080 https://ifconfig.me
echo

# logs
docker compose -f docker-compose.yml logs -f --tail=100
# or: docker compose -f docker-compose.host.yml logs -f --tail=100
```

Web UI (if `UI_ENABLE=true`):

```text
http://HOST_IP:28080/ui/
```

Enter `UI_TOKEN` → bypass list and Outline key replace.

---

## Step 6. Attach clients

### SOCKS

| Client | Example |
|--------|---------|
| curl | `curl --socks5h IP:1080 https://example.com` |
| Firefox | Settings → Network → SOCKS5 `IP` / `1080` |
| System / CLI | proxychains, `ALL_PROXY=socks5h://IP:1080` |

### L3 (host profile)

On the LAN client, default gateway = **host IP** running outline-gate.

```bash
# Linux client (example)
sudo ip route replace default via HOST_IP
```

DHCP option 3 / static gateway on the router — for the whole network.

**DNS:** may go around the tunnel; account for `exclude`/`include` and bypass.

---

## Step 7. Security (required)

1. Do **not** publish `:1080` to the internet — SOCKS has **no password**.
2. Restrict firewall to LAN; prefer `SOCKS_ALLOW_CIDRS=192.168.0.0/16,10.0.0.0/8,...`.
3. Strong `UI_TOKEN`; health/UI port only on a trusted network (or reverse-proxy + TLS).
4. Do **not** commit `.env` and keys (`git check-ignore -v deploy/compose/.env`).
5. `METRICS_ENABLE=true` opens `/metrics` **without** auth — localhost/LAN only.

---

## Step 8. Upgrade on an already deployed host

```bash
cd outline-gate
git fetch --tags
git checkout v0.6.0   # or git pull on main
cd deploy/compose
./install.sh          # rebuild + recreate
# .env and config/bypass.rules.txt are kept (volume / gitignore)
```

Rollback: `git checkout <prev-tag>` + `./install.sh`.

---

## Step 9. Move config from the current host

On the **old** host save (outside git, in a secret store):

```bash
cd deploy/compose
tar czf /tmp/outline-gate-host-config.tgz \
  .env \
  secrets/ \
  config/bypass.rules.txt \
  config/bypass.txt \
  config/tunnel.txt \
  config/outline_key.runtime.txt 2>/dev/null || true
# copy the tgz to the new host over a protected channel (scp, age, vault)
```

On the **new** host after clone:

```bash
cd outline-gate/deploy/compose
tar xzf /path/to/outline-gate-host-config.tgz
chmod 600 .env secrets/* 2>/dev/null || true
./install.sh
```

---

## Step 10. Stop and clean up

```bash
cd deploy/compose
./install.sh --down
# or
docker compose -f docker-compose.yml down
docker compose -f docker-compose.host.yml down
```

If the L3 process was killed hard and nft rules remain:

```bash
sudo nft list tables
sudo nft delete table inet outline_gate
```

---

## Common problems

| Symptom | Action |
|---------|--------|
| `missing access key` | `.env` / `configure.sh` / `secrets/outline_key.txt` |
| `/readyz` 503 | path to Outline, key format `ss://`\|`ssconf://`, `docker compose logs` |
| SOCKS timeout | firewall, allowlist, is the Outline server alive |
| L3 "does not work" | host-compose? `GATEWAY_ENABLE=true`? GW on clients = host IP? |
| Port in use | change `HOST_SOCKS_PORT` / `HOST_HEALTH_PORT` in `.env` |
| `all predefined address pools have been fully subnetted` | outline-gate does **not** use the pool: net `outline-gate_net` = `192.168.102.0/24` (explicit IPAM). Update the code (`git pull`) and `./install.sh`. Subnet clash → change `COMPOSE_SUBNET`/`COMPOSE_GATEWAY`. Recommended daemon: `deploy/docker/daemon.json.example`. Host profile does not create a bridge. |
| Permission denied on `config/*` | files owned by root from the container: `docker run --rm -v $PWD/config:/c alpine chown -R $(id -u):$(id -g) /c` |

---

## New-host checklist

- [ ] Docker + Compose v2 (optional `daemon.json` from `deploy/docker/daemon.json.example`)
- [ ] `git clone` + `./install.sh 'ss://...'`
- [ ] `curl` `/readyz` + SOCKS `ifconfig.me`
- [ ] Firewall: LAN only on 1080/UI
- [ ] (opt.) `./install.sh --host` for SOCKS + L3
- [ ] (opt.) backup `.env` + `config/bypass.rules.txt`

---

## Related docs

- [OPERATIONS.md](OPERATIONS.md) — full env, API, operations reference · [Русский](OPERATIONS.ru.md)
- [deployment.md](deployment.md) — profiles A/B/C
- [architecture.md](architecture.md) — components
- [routing.md](routing.md) — exclude / include
- [README.md](../README.md) — product overview · [Русский](../README.ru.md)
