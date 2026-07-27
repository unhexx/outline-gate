# outline-gate — пошаговая инструкция по развёртыванию и эксплуатации

**Релиз:** [v0.4.0](https://github.com/unhexx/outline-gate/releases/tag/v0.4.0) · [CHANGELOG](../CHANGELOG.md) · [README](../README.md)

| | |
|--|--|
| GitHub | https://github.com/unhexx/outline-gate |
| aservice | https://git.aservice24.ru/scm/expert/outline-gate.git |
| Ветка релиза | `master` + tags `v*` |

## 1. Что это

**outline-gate** — Docker-сервис, который:

1. Подключается к удалённому **Outline** (Shadowsocks) по access key `ss://...` или динамическому `ssconf://...`.
2. Отдаёт локальный **SOCKS5** (`:1080`) для приложений и клиентов LAN.
3. Опционально работает как **L3-шлюз** (nftables): клиенты LAN направляют трафик через хост, сервис решает, что идёт в туннель, а что напрямую.
4. Отдаёт **Web UI** (`/ui/`) для bypass-списка и замены ключа Outline (с `UI_ENABLE` + `UI_TOKEN`).

| Режим `ROUTING_MODE` | Поведение |
|----------------------|-----------|
| `exclude` (по умолчанию) | В туннель — всё, **кроме** bypass (частные сети + ваш список + IP Outline-сервера) |
| `include` | В туннель — **только** адреса из `TUNNEL_*`; остальное — `direct` или `drop` |

---

## 2. Требования

- Linux-хост (для L3-шлюза) или любой Docker-хост (для SOCKS)
- Docker Engine 20+ и Docker Compose v2
- Access key Outline (`ss://...` или `ssconf://...`) от Outline Manager / провайдера
- Свободные порты на хосте: **1080** (SOCKS), **health/UI** (`HOST_HEALTH_PORT`, часто `8080` или `28080`)
- Для L3: capability `NET_ADMIN`, желательно `network_mode: host`

---

## 3. Получение кода

### 3.1. Релизный тег (рекомендуется)

```bash
# GitHub
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate
git checkout v0.4.0

# или aservice
git clone https://git.aservice24.ru/scm/expert/outline-gate.git
cd outline-gate
git checkout v0.4.0
```

Бинарник Linux amd64 (без Docker):

```bash
curl -fsSL -o outline-gate \
  https://github.com/unhexx/outline-gate/releases/download/v0.4.0/outline-gate_linux_amd64
chmod +x outline-gate
```

### 3.2. Клонирование development-ветки

```bash
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate
# master — актуальная линия разработки; releases — git tags v*
```

---

## 4. Настройка ключа и параметров

Все рабочие секреты — в `deploy/compose/.env` (в git **не** попадает).

```bash
cd deploy/compose
cp .env.example .env
chmod +x configure.sh
./configure.sh
```

Скрипт спросит:

1. **Ключ Outline** — в `.env` (`OUTLINE_ACCESS_KEY`) или в `secrets/outline_key.txt`
2. **ROUTING_MODE** — `exclude` / `include`
3. При `include` — список `TUNNEL_CIDRS`
4. Доп. `BYPASS_CIDRS` (опционально)
5. **GATEWAY_ENABLE** — `false` для SOCKS, `true` для L3 host
6. **LOG_LEVEL**

### Ручная правка `.env`

```bash
# Обязательно
OUTLINE_ACCESS_KEY=ss://....@server:port

# Частые опции
ROUTING_MODE=exclude
GATEWAY_ENABLE=false
HOST_SOCKS_PORT=1080
HOST_HEALTH_PORT=28080   # health + Web UI на хосте
UI_ENABLE=true
UI_TOKEN=длинный-случайный-секрет
LOG_LEVEL=info
```

Полный список переменных — в `.env.example` и в таблице ниже.

### Альтернатива: только файл ключа

```bash
printf '%s\n' 'ss://YOUR_KEY' > secrets/outline_key.local.txt
chmod 600 secrets/outline_key.local.txt
```

В `.env`:

```bash
OUTLINE_ACCESS_KEY=
OUTLINE_ACCESS_KEY_FILE=/run/secrets/outline_key
OUTLINE_KEY_HOST_PATH=./secrets/outline_key.local.txt
```

---

## 5. Сборка образа Docker

Из **корня** репозитория:

```bash
docker build -f deploy/docker/Dockerfile -t outline-gate:local .
```

Или через Compose (соберёт при первом `up`):

```bash
cd deploy/compose
docker compose build
```

Образ **не** содержит access key — ключ передаётся только при запуске.

### Запуск одной командой `docker run` (SOCKS)

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
  outline-gate:v0.4.0
```

Доп. параметры — любыми `-e ИМЯ=значение` (см. таблицу).

---

## 6. Профили развёртывания

### 6.1. Profile C — SOCKS (рекомендуется для старта)

Клиенты/приложения указывают SOCKS5 `HOST:1080`. Смена default gateway **не** нужна.

```bash
cd deploy/compose
# GATEWAY_ENABLE=false в .env
docker compose up --build -d
docker compose ps
docker compose logs -f
```

Проверка:

```bash
curl -s http://127.0.0.1:8080/healthz    # ok
curl -s http://127.0.0.1:8080/readyz     # {"ready":true,...}
curl -s --socks5 127.0.0.1:1080 https://ifconfig.me
echo
```

IP в ответе должен совпадать с egress Outline-сервера (не с «домашним» IP, если туннель работает).

### 6.2. Profile A — L3-шлюз (host network)

Хост становится шлюзом для LAN. На клиентах: **шлюз по умолчанию = IP хоста**.

```bash
cd deploy/compose
# в .env:
#   GATEWAY_ENABLE=true
#   ROUTING_MODE=exclude   # или include + TUNNEL_CIDRS
#   # LAN_INTERFACE=eth0   # при необходимости

docker compose -f docker-compose.host.yml up --build -d
```

На клиенте (пример Linux):

```bash
# IP_ХОСТА — адрес машины с outline-gate в LAN
sudo ip route replace default via IP_ХОСТА
```

Windows/macOS/роутер: укажите default gateway / DHCP option 3.

**Важно:** на bridge-сети Docker контейнер **не** становится LAN-gateway «из коробки». Для L3 используйте host (или macvlan — см. `docs/deployment.md`).

### 6.3. Остановка и очистка

```bash
cd deploy/compose
docker compose down
# или
docker compose -f docker-compose.host.yml down
```

После остановки таблица nftables `inet outline_gate` снимается процессом. Если процесс убит жёстко:

```bash
sudo nft list tables
sudo nft delete table inet outline_gate   # если осталась
```

---

## 7. Справочник параметров

| Переменная | Обязательно | По умолчанию | Описание |
|------------|-------------|--------------|----------|
| `OUTLINE_ACCESS_KEY` | да* | — | Ключ `ss://...` или `ssconf://...` |
| `OUTLINE_ACCESS_KEY_FILE` | да* | — | Путь к файлу с ключом в контейнере |
| `ROUTING_MODE` | нет | `exclude` | `exclude` \| `include` |
| `BYPASS_CIDRS` | нет | — | Статические CIDR-исключения, CSV |
| `BYPASS_CIDRS_FILE` | нет | `/config/bypass.txt` | Файл CIDR (по строке) |
| `BYPASS_RULES_FILE` | нет | `/config/bypass.rules.txt` | User-правила UI: IP/CIDR/домены/`*.mask` |
| `BYPASS_DNS_REFRESH` | нет | `60s` | Период DNS-резолва доменов для L3 |
| `UI_ENABLE` | нет | `false` | Web UI `/ui/` и API |
| `UI_TOKEN` | при UI | — | Токен (Bearer или Basic password) |
| `OUTLINE_KEY_PERSIST_FILE` | нет | `/config/outline_key.runtime.txt` | Ключ после замены в UI (приоритет при старте) |
| `HOST_HEALTH_PORT` | нет | `8080` | Порт health/UI на хосте (bridge) |
| `TUNNEL_CIDRS` | для include | — | Цели туннеля, CSV |
| `TUNNEL_CIDRS_FILE` | для include | `/config/tunnel.txt` | Файл CIDR |
| `DIRECT_POLICY` | нет | `direct` | `direct` \| `drop` (include) |
| `GATEWAY_ENABLE` | нет | `false` / host:`true` | L3 nftables |
| `LAN_INTERFACE` | нет | — | oif для MASQUERADE |
| `SOCKS_LISTEN` | нет | `0.0.0.0:1080` | SOCKS5 |
| `SOCKS_ALLOW_CIDRS` | нет | (пусто = все) | Allowlist source IP клиентов SOCKS (CSV) |
| `SOCKS_ALLOW_CIDRS_FILE` | нет | — | Файл CIDR source allowlist (по строке) |
| `METRICS_ENABLE` | нет | `false` | Prometheus text metrics на `/metrics` |
| `HEALTH_LISTEN` | нет | `0.0.0.0:8080` | Health HTTP |
| `TRANSPROXY_LISTEN` | нет | `127.0.0.1:12345` | REDIRECT target |
| `LOG_LEVEL` | нет | `info` | debug/info/warn/error |
| `LOG_FORMAT` | нет | `text` | text/json |
| `RECONNECT_BASE_DELAY` | нет | `1s` | backoff |
| `RECONNECT_MAX_DELAY` | нет | `60s` | cap backoff |
| `HOST_SOCKS_PORT` | нет | `1080` | publish на хосте |
| `HOST_HEALTH_PORT` | нет | `8080` | publish на хосте |
| `IMAGE_TAG` | нет | `outline-gate:local` | тег образа |

\* Нужен **хотя бы один** способ передать ключ.

Файлы списков: `deploy/compose/config/bypass.txt`, `bypass.rules.txt`, `tunnel.txt` (монтируются в `/config`, **rw** — UI пишет rules).

### 7.1. Web UI (bypass + ключ Outline)

В `.env`:

```bash
UI_ENABLE=true
UI_TOKEN=длинный-случайный-секрет
HOST_HEALTH_PORT=28080   # любой свободный порт хоста → контейнер :8080
```

```bash
docker compose up --build -d
```

Откройте:

```text
http://IP-хоста:28080/ui/
```

Введите `UI_TOKEN`. В интерфейсе:

1. **Ключ Outline** — вставка `ss://` / `ssconf://`, «Заменить ключ» (reconnect + запись в `OUTLINE_KEY_PERSIST_FILE`).
2. **Bypass** — IP / CIDR / домен / `*.suffix`.

| Пример bypass | Значение |
|---------------|----------|
| `8.8.8.8` | один IP |
| `203.0.113.0/24` | подсеть |
| `example.com` | точный домен |
| `*.cdn.example.net` | домен и поддомены (SOCKS; L3 — DNS apex) |

API (тот же токен; порт = `HOST_HEALTH_PORT`):

```bash
PORT=28080
# статус ключа (redacted)
curl -s -H "Authorization: Bearer $UI_TOKEN" http://127.0.0.1:$PORT/api/v1/outline

# заменить ключ
curl -s -X PUT -H "Authorization: Bearer $UI_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"access_key":"ss://...@host:port"}' \
  http://127.0.0.1:$PORT/api/v1/outline

# список bypass
curl -s -H "Authorization: Bearer $UI_TOKEN" http://127.0.0.1:$PORT/api/v1/bypass

# добавить правило
curl -s -X POST -H "Authorization: Bearer $UI_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"rule":"*.example.com"}' \
  http://127.0.0.1:$PORT/api/v1/bypass
```

`/healthz` и `/readyz` **без** токена (для healthcheck).

**Приоритет ключа при старте:** `OUTLINE_KEY_PERSIST_FILE` → `OUTLINE_ACCESS_KEY` → `OUTLINE_ACCESS_KEY_FILE`.

---

## 8. Эксплуатация

### 8.1. Статус и логи

```bash
docker compose ps
docker compose logs -f --tail=200
curl -s http://127.0.0.1:8080/readyz | jq .
```

| Endpoint | Смысл |
|----------|--------|
| `GET /healthz` | процесс жив |
| `GET /readyz` | dialer Outline готов (+ gateway rules, если включены) |
| `GET /ui/` | Web UI списка исключений (если `UI_ENABLE=true`) |

### 8.2. Смена ключа / режима

1. Отредактируйте `.env` или `secrets/…`
2. Пересоздайте контейнер:

```bash
docker compose up -d --force-recreate
```

Перезагрузка списков CIDR без полного рестарта (если процесс видит те же volume-файлы после правки — нужен SIGHUP):

```bash
docker kill -s HUP outline-gate
```

SIGHUP перечитывает env **процесса** (не обязательно перечитает обновлённый `.env` с диска, если переменные уже зафиксированы compose). Надёжнее: `compose up -d --force-recreate`.

### 8.3. Обновление версии

```bash
cd outline-gate
git pull
cd deploy/compose
docker compose up --build -d
```

### 8.4. Бэкап конфигурации

Сохраните (вне git, в секрет-хранилище):

- `deploy/compose/.env`
- `deploy/compose/secrets/outline_key.local.txt` (если используете)
- `deploy/compose/config/*.txt`

### 8.5. Безопасность

- SOCKS **без пароля** — только доверенная LAN / firewall; не публикуйте `:1080` в интернет.
- Не коммитьте `.env` и реальные ключи.
- Ключ в логах редактируется (`ss://***@host:port`).
- Ограничьте доступ к `:8080`, если не нужен снаружи.
- При `UI_ENABLE=true` обязательно задайте сильный `UI_TOKEN`; API без токена отвечает 401. UI-страница публична (секретов нет), данные меняются только через API с токеном.

### 8.6. Типовые проблемы

| Симптом | Что проверить |
|---------|----------------|
| `missing access key` | `.env` / файл ключа; `configure.sh` |
| `/readyz` 503 | ключ, сеть до Outline, логи `docker compose logs` |
| SOCKS timeout | firewall, `ROUTING_MODE`, жив ли Outline server |
| L3 не работает | host profile? `GATEWAY_ENABLE=true`? default GW на клиентах? |
| Остались nft rules | `sudo nft delete table inet outline_gate` |
| Routing loop | IP сервера Outline должен быть в bypass (добавляется автоматически) |

---

## 9. Клиенты LAN

### SOCKS (Profile C)

| Клиент | Настройка |
|--------|-----------|
| curl | `curl --socks5 IP:1080 https://example.com` |
| Firefox | Settings → Network → Manual → SOCKS5 host/port, DNS through proxy optional |
| SSH | `ssh -o ProxyCommand='nc -X 5 -x IP:1080 %h %p' …` |
| Система | proxychains / системный SOCKS (зависит от ОС) |

### L3 (Profile A)

- Default gateway → IP хоста outline-gate  
- DNS: осознанно (DNS может «утекать» мимо туннеля; при необходимости резолверы в `TUNNEL`/`exclude` политике)

---

## 10. Публикация в git.aservice24.ru (чеклист)

1. Создать проект `outline-gate` на `git.aservice24.ru`.
2. Локально:

```bash
git remote add origin git@git.aservice24.ru:GROUP/outline-gate.git
git push -u origin master
```

3. На сервере развёртывания: clone → `configure.sh` → `docker compose up --build -d`.
4. Убедиться, что `.env` и ключи **не** в репозитории (`git check-ignore -v deploy/compose/.env`).

---

## 11. Связанные документы

- [architecture.md](architecture.md) — схема компонентов  
- [deployment.md](deployment.md) — профили A/B/C (EN)  
- [routing.md](routing.md) — режимы маршрутизации  
- [design-plan.md](design-plan.md) — полный дизайн / PR DAG  
