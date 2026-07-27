# outline-gate

[![CI](https://github.com/unhexx/outline-gate/actions/workflows/ci.yml/badge.svg)](https://github.com/unhexx/outline-gate/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/unhexx/outline-gate)](https://goreportcard.com/report/github.com/unhexx/outline-gate)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/unhexx/outline-gate)](go.mod)
[![Release](https://img.shields.io/github/v/release/unhexx/outline-gate?include_prereleases&sort=semver)](https://github.com/unhexx/outline-gate/releases)

Docker LAN-шлюз к [Outline](https://getoutline.org/) (Shadowsocks): SOCKS5 + опциональный L3 split-tunnel + Web UI.

## Возможности

- Клиент Outline через **outline-sdk** (`ss://` и динамические `ssconf://`)
- Локальный **SOCKS5** (`:1080`)
- Опциональный **L3 gateway** (nftables): режимы `exclude` / `include`
- **Web UI** (`/ui/`): исключения (IP / CIDR / домены / `*.mask`) и **замена ключа Outline**
- Параметры: `.env`, volume-файлы, Docker secrets
- Health: `/healthz`, `/readyz` (без auth; UI/API — с `UI_TOKEN`)

## Быстрый старт

```bash
cd deploy/compose
cp .env.example .env
chmod +x configure.sh
./configure.sh          # ввод ss:// ключа и параметров

# Web UI (рекомендуется для LAN)
# в .env:
#   UI_ENABLE=true
#   UI_TOKEN=<длинный-секрет>
#   HOST_HEALTH_PORT=28080   # любой свободный порт хоста

docker compose up --build -d

curl -s http://127.0.0.1:28080/readyz
# UI: http://127.0.0.1:28080/ui/
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
| [docs/routing.md](docs/routing.md) | Режимы маршрутизации и bypass |
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
  -e UI_ENABLE=true \
  -e UI_TOKEN='change-me' \
  -p 1080:1080 -p 28080:8080 \
  -v "$PWD/deploy/compose/config:/config" \
  outline-gate:local
```

## Основные переменные

| Variable | Description |
|----------|-------------|
| `OUTLINE_ACCESS_KEY` / `OUTLINE_ACCESS_KEY_FILE` | Ключ Outline `ss://` или `ssconf://` |
| `OUTLINE_KEY_PERSIST_FILE` | Файл ключа после замены в UI (приоритет при старте) |
| `ROUTING_MODE` | `exclude` \| `include` |
| `BYPASS_CIDRS` / `BYPASS_CIDRS_FILE` | Статические CIDR-исключения |
| `BYPASS_RULES_FILE` | User-правила (IP/домены) для UI |
| `UI_ENABLE` / `UI_TOKEN` | Web UI `/ui/` + API |
| `HOST_HEALTH_PORT` | Порт UI/health на хосте (compose bridge) |
| `TUNNEL_CIDRS` / `TUNNEL_CIDRS_FILE` | Цели (include) |
| `DIRECT_POLICY` | `direct` \| `drop` |
| `GATEWAY_ENABLE` | L3 nftables |
| `SOCKS_LISTEN` / `HEALTH_LISTEN` | Адреса слушателей в контейнере |
| `LOG_LEVEL` | `debug`/`info`/`warn`/`error` |

Полный список: [`deploy/compose/.env.example`](deploy/compose/.env.example).

## Web UI

| URL | Auth | Назначение |
|-----|------|------------|
| `/ui/` | API с токеном | SPA управления |
| `GET/PUT /api/v1/outline` | Bearer `UI_TOKEN` | Статус / замена ключа |
| `GET/POST/DELETE /api/v1/bypass` | Bearer `UI_TOKEN` | Правила исключений |
| `/healthz`, `/readyz` | нет | Docker healthcheck |

Ключ, заменённый в UI, сохраняется в `OUTLINE_KEY_PERSIST_FILE` (по умолчанию `/config/outline_key.runtime.txt`) и при следующем старте имеет приоритет над `OUTLINE_ACCESS_KEY` в `.env`.

## Best practices

1. **Секреты** — только `.env` / Docker secrets / UI persist-файл; никогда в git и в образ.
2. **UI_TOKEN** — длинный случайный; не публикуйте `:HEALTH` в интернет без reverse-proxy + TLS.
3. **Порты** — SOCKS (`1080`) и UI только в доверенной LAN / firewall.
4. **Bypass по доменам на L3** — best-effort (DNS refresh); для точного match используйте SOCKS.
5. **Обновления** — `docker compose up --build -d`; списки rules в volume `./config`.
6. **SIGHUP** — перечитывает volume-файлы; смена env из compose надёжнее через recreate.

## Репозиторий

- GitHub: https://github.com/unhexx/outline-gate
- Origin (Bitbucket/aservice): `git.aservice24.ru`

```bash
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate
```

Подробный чеклист — в [docs/OPERATIONS.ru.md](docs/OPERATIONS.ru.md).

## Безопасность

- SOCKS без auth — только доверенная сеть
- Не коммитьте `.env`, `*.runtime.txt` и реальные ключи
- В логах ключ редактируется (`ss://***@host:port`)
- API UI без токена → `401`

## License

MIT
