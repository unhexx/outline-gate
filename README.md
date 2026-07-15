# outline-gate

Docker LAN-шлюз к [Outline](https://getoutline.org/) (Shadowsocks): SOCKS5 + опциональный L3 split-tunnel.

## Возможности

- Клиент Outline через **outline-sdk** (`ss://` access key)
- Локальный **SOCKS5** (`:1080`)
- Опциональный **L3 gateway** (nftables): режимы `exclude` / `include`
- Параметры и ключ при запуске: `.env`, файл секрета, `docker run -e …`
- Health: `/healthz`, `/readyz`

## Быстрый старт

```bash
cd deploy/compose
cp .env.example .env
chmod +x configure.sh
./configure.sh          # ввод ss:// ключа и параметров

docker compose up --build -d

curl -s http://127.0.0.1:8080/readyz
curl -s --socks5 127.0.0.1:1080 https://ifconfig.me
```

L3-шлюз (host network, клиенты LAN → IP хоста как default GW):

```bash
# в .env: GATEWAY_ENABLE=true
docker compose -f docker-compose.host.yml up --build -d
```

## Документация

| Документ | Содержание |
|----------|------------|
| **[docs/OPERATIONS.ru.md](docs/OPERATIONS.ru.md)** | **Пошаговое развёртывание и эксплуатация (RU)** |
| [docs/architecture.md](docs/architecture.md) | Архитектура |
| [docs/deployment.md](docs/deployment.md) | Профили сети A/B/C |
| [docs/routing.md](docs/routing.md) | Режимы маршрутизации |
| [docs/design-plan.md](docs/design-plan.md) | Дизайн и план PR |

## Сборка образа

```bash
docker build -f deploy/docker/Dockerfile -t outline-gate:local .
```

Ключ **не** вшивается в образ:

```bash
docker run --rm -d --name outline-gate \
  --cap-add=NET_ADMIN \
  -e OUTLINE_ACCESS_KEY='ss://...' \
  -e ROUTING_MODE=exclude \
  -e GATEWAY_ENABLE=false \
  -p 1080:1080 -p 8080:8080 \
  outline-gate:local
```

## Основные переменные

| Variable | Description |
|----------|-------------|
| `OUTLINE_ACCESS_KEY` / `OUTLINE_ACCESS_KEY_FILE` | Ключ Outline (обязательно один из) |
| `ROUTING_MODE` | `exclude` \| `include` |
| `BYPASS_CIDRS` / `BYPASS_CIDRS_FILE` | Исключения |
| `TUNNEL_CIDRS` / `TUNNEL_CIDRS_FILE` | Цели (include) |
| `DIRECT_POLICY` | `direct` \| `drop` |
| `GATEWAY_ENABLE` | L3 nftables |
| `SOCKS_LISTEN` / `HEALTH_LISTEN` | Адреса слушателей |
| `LOG_LEVEL` | `debug`/`info`/`warn`/`error` |

Полный список: `deploy/compose/.env.example`.

## Репозиторий

Публикация на **git.aservice24.ru**:

```bash
git remote add origin git@git.aservice24.ru:GROUP/outline-gate.git
git push -u origin master
```

Подробный чеклист — в [docs/OPERATIONS.ru.md](docs/OPERATIONS.ru.md) §10.

## Безопасность

- SOCKS без auth — только доверенная сеть
- Не коммитьте `.env` и реальные ключи
- В логах ключ редактируется

## License

MIT
