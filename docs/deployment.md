# Deployment

## Prerequisites

- Docker Engine + Compose plugin
- Outline access key (`ss://...`) from Outline Manager / provider
- For L3 gateway: ability to set default gateway on LAN clients; `NET_ADMIN` capability

## Secrets

```bash
mkdir -p deploy/compose/secrets
echo -n 'ss://...' > deploy/compose/secrets/outline_key.txt
chmod 600 deploy/compose/secrets/outline_key.txt
```

## Profile A — host network (L3 gateway)

Best when outline-gate runs on a always-on Linux box on the LAN.

```bash
cd deploy/compose
GATEWAY_ENABLE=true docker compose -f docker-compose.host.yml up --build -d
```

On clients:

- IPv4 gateway = host LAN IP
- DNS = host or public resolvers (document leaks if DNS is not tunnelled)

Verify:

```bash
curl --socks5 HOST_IP:1080 https://ifconfig.me
curl http://HOST_IP:8080/readyz
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

Default `docker-compose.yml`: `GATEWAY_ENABLE=false`, publish `1080`/`8080`.  
Apps configure SOCKS5 to `host:1080`. No default-gateway change required.

## Firewall

- Restrict `:1080` to LAN if exposed
- Do not publish SOCKS to the public Internet without auth (v1 has no SOCKS auth)
