# Развёртывание outline-gate на другом хосте

**Релиз:** [v0.4.0](https://github.com/unhexx/outline-gate/releases/tag/v0.4.0) · подробная эксплуатация: [OPERATIONS.ru.md](OPERATIONS.ru.md)

Краткая пошаговая инструкция: от пустого Linux-хоста до рабочего SOCKS / L3-шлюза.

| Ресурс | URL |
|--------|-----|
| GitHub | https://github.com/unhexx/outline-gate |
| Internal (aservice) | https://git.aservice24.ru/scm/expert/outline-gate.git |
| Релизный тег | `v0.4.0` (ветка `master`) |

---

## Что получите

На целевом хосте:

1. Контейнер **outline-gate** с клиентом Outline (`ss://` / `ssconf://`).
2. **SOCKS5** на порту хоста (по умолчанию `1080`).
3. Опционально **L3-шлюз** (клиенты LAN → IP хоста как default gateway).
4. Опционально **Web UI** для bypass-списка и смены ключа.

Секреты (ключ Outline, `UI_TOKEN`, `.env`) **не** хранятся в git — настраиваются только на хосте.

---

## Шаг 0. Требования на целевом хосте

| Требование | Минимум |
|------------|---------|
| ОС | Linux x86_64 (Docker; для L3 — nftables / `NET_ADMIN`) |
| Docker | Engine 20+ и **Compose v2** (`docker compose version`) |
| Порты | свободны `1080` (SOCKS) и health/UI (часто `28080` или `8080`) |
| Ключ | Outline access key: `ss://...` или `ssconf://...` |
| Сеть | доступ хоста до Outline-сервера (UDP/TCP по ключу) |

Проверка:

```bash
docker --version
docker compose version
curl -fsSL https://get.docker.com | sh   # если Docker ещё нет (официальный install)
```

Пользователь деплоя должен быть в группе `docker` (или запускать через root).

---

## Шаг 1. Получить код

### Вариант A — релизный тег (рекомендуется)

```bash
# GitHub
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate
git checkout v0.4.0

# или internal aservice
git clone https://git.aservice24.ru/scm/expert/outline-gate.git
cd outline-gate
git checkout v0.4.0
```

### Вариант B — актуальный master

```bash
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate
# git pull  # при обновлении существующей копии
```

### Вариант C — только бинарник (без Docker)

```bash
curl -fsSL -o outline-gate \
  https://github.com/unhexx/outline-gate/releases/download/v0.4.0/outline-gate_linux_amd64
chmod +x outline-gate
export OUTLINE_ACCESS_KEY='ss://...'
./outline-gate
```

Для production предпочтителен Docker (шаги ниже).

---

## Шаг 2. Выбрать профиль

| Профиль | Compose-файл | Когда |
|---------|--------------|--------|
| **socks** (bridge) | `docker-compose.yml` | Приложения указывают SOCKS5 `HOST:1080`. Без смены gateway. |
| **host** (L3) | `docker-compose.host.yml` | Хост = default GW для LAN; TV/IoT без proxy. |

Можно использовать **оба**: L3 для устройств + SOCKS для приложений на том же хосте (host-профиль слушает `:1080` на всех интерфейсах).

---

## Шаг 3. Настроить секреты и параметры

```bash
cd deploy/compose
chmod +x configure.sh install.sh
./configure.sh
```

Скрипт создаст `.env` и спросит:

1. **Профиль** — socks или host (L3)
2. **Ключ Outline** — в `.env` или в `secrets/outline_key.txt`
3. **ROUTING_MODE** — `exclude` (всё в VPN кроме bypass) / `include`
4. **Web UI** — `UI_ENABLE`, автогенерация `UI_TOKEN`
5. **Порты** хоста (для socks) и опционально `SOCKS_ALLOW_CIDRS`

### Без интерактива

```bash
cd deploy/compose
cp .env.example .env
# отредактируйте:
#   OUTLINE_ACCESS_KEY=ss://...@server:port
#   UI_ENABLE=true
#   UI_TOKEN=$(openssl rand -hex 24)
#   HOST_HEALTH_PORT=28080
#   COMPOSE_PROFILE=socks   # или host
#   GATEWAY_ENABLE=false    # true для L3
nano .env
```

Файлы runtime (не в git):

| Путь | Назначение |
|------|------------|
| `deploy/compose/.env` | переменные окружения |
| `secrets/outline_key.txt` | опционально ключ вместо env |
| `config/bypass.rules.txt` | правила UI (создаётся из example при install) |
| `config/outline_key.runtime.txt` | ключ после замены в Web UI |

---

## Шаг 4. Собрать и запустить

Одной командой:

```bash
cd deploy/compose
./install.sh
```

Скрипт:

- проверит Docker / Compose;
- при отсутствии `.env` предложит `configure.sh`;
- подготовит `config/*` и stub для secrets;
- выполнит `docker compose up --build -d` для выбранного профиля;
- дождётся `GET /readyz`.

### Вручную

```bash
# SOCKS (bridge)
docker compose -f docker-compose.yml up --build -d

# L3 (host network)
# в .env: GATEWAY_ENABLE=true  COMPOSE_PROFILE=host
docker compose -f docker-compose.host.yml up --build -d
```

Полезные флаги `install.sh`:

```bash
./install.sh --configure   # настройка + запуск
./install.sh --host        # принудительно L3
./install.sh --socks       # принудительно SOCKS
./install.sh --check       # только /readyz
./install.sh --down        # остановка
./install.sh --no-build    # up без rebuild
```

---

## Шаг 5. Проверить

Порты по умолчанию: SOCKS `1080`, health/UI `28080` (bridge) или `8080` (host).

```bash
# готовность туннеля
curl -s http://127.0.0.1:28080/readyz
# {"ready":true,...}

# egress через Outline (IP должен быть egress VPN, не «домашний»)
curl -s --socks5h 127.0.0.1:1080 https://ifconfig.me
echo

# логи
docker compose -f docker-compose.yml logs -f --tail=100
# или: docker compose -f docker-compose.host.yml logs -f --tail=100
```

Web UI (если `UI_ENABLE=true`):

```text
http://IP-ХОСТА:28080/ui/
```

Введите `UI_TOKEN` → bypass-список и замена ключа Outline.

---

## Шаг 6. Подключить клиентов

### SOCKS

| Клиент | Пример |
|--------|--------|
| curl | `curl --socks5h IP:1080 https://example.com` |
| Firefox | Settings → Network → SOCKS5 `IP` / `1080` |
| Система / CLI | proxychains, `ALL_PROXY=socks5h://IP:1080` |

### L3 (host-профиль)

На клиенте LAN default gateway = **IP хоста** с outline-gate.

```bash
# Linux-клиент (пример)
sudo ip route replace default via IP_ХОСТА
```

DHCP option 3 / static gateway на роутере — для всей сети.

**DNS:** может ходить мимо туннеля; учитывайте политику `exclude`/`include` и bypass.

---

## Шаг 7. Безопасность (обязательно)

1. **Не** публикуйте `:1080` в интернет — SOCKS **без пароля**.
2. Ограничьте firewall LAN; желательно `SOCKS_ALLOW_CIDRS=192.168.0.0/16,10.0.0.0/8,...`.
3. Сильный `UI_TOKEN`; health/UI порт только в доверенной сети (или reverse-proxy + TLS).
4. `.env` и ключи **не** коммитить (`git check-ignore -v deploy/compose/.env`).
5. `METRICS_ENABLE=true` открывает `/metrics` **без** auth — только localhost/LAN.

---

## Шаг 8. Обновление на уже развёрнутом хосте

```bash
cd outline-gate
git fetch --tags
git checkout v0.4.0   # или git pull на master
cd deploy/compose
./install.sh          # rebuild + recreate
# .env и config/bypass.rules.txt сохраняются (volume / gitignore)
```

Откат: `git checkout <prev-tag>` + `./install.sh`.

---

## Шаг 9. Перенос конфигурации с текущего хоста

На **старом** хосте сохраните (вне git, в секрет-хранилище):

```bash
cd deploy/compose
tar czf /tmp/outline-gate-host-config.tgz \
  .env \
  secrets/ \
  config/bypass.rules.txt \
  config/bypass.txt \
  config/tunnel.txt \
  config/outline_key.runtime.txt 2>/dev/null || true
# скопируйте tgz на новый хост защищённым каналом (scp, age, vault)
```

На **новом** хосте после clone:

```bash
cd outline-gate/deploy/compose
tar xzf /path/to/outline-gate-host-config.tgz
chmod 600 .env secrets/* 2>/dev/null || true
./install.sh
```

---

## Шаг 10. Остановка и очистка

```bash
cd deploy/compose
./install.sh --down
# или
docker compose -f docker-compose.yml down
docker compose -f docker-compose.host.yml down
```

Если L3-процесс убит жёстко и остались nft-правила:

```bash
sudo nft list tables
sudo nft delete table inet outline_gate
```

---

## Типовые проблемы

| Симптом | Действие |
|---------|----------|
| `missing access key` | `.env` / `configure.sh` / `secrets/outline_key.txt` |
| `/readyz` 503 | сеть до Outline, формат ключа `ss://`\|`ssconf://`, `docker compose logs` |
| SOCKS timeout | firewall, allowlist, жив ли Outline server |
| L3 «не работает» | host-compose? `GATEWAY_ENABLE=true`? GW на клиентах = IP хоста? |
| Порт занят | смените `HOST_SOCKS_PORT` / `HOST_HEALTH_PORT` в `.env` |
| Permission denied на `config/*` | файлы от root из контейнера: `docker run --rm -v $PWD/config:/c alpine chown -R $(id -u):$(id -g) /c` |

---

## Чеклист «новый хост за 10 минут»

- [ ] Docker + Compose v2
- [ ] `git clone` + `git checkout v0.4.0`
- [ ] `cd deploy/compose && ./configure.sh`
- [ ] `./install.sh`
- [ ] `curl` `/readyz` + SOCKS `ifconfig.me`
- [ ] Firewall: только LAN на 1080/UI
- [ ] (опц.) клиенты: SOCKS или default GW
- [ ] (опц.) бэкап `.env` + `config/bypass.rules.txt`

---

## Связанные документы

- [OPERATIONS.ru.md](OPERATIONS.ru.md) — полный справочник env, API, эксплуатация  
- [deployment.md](deployment.md) — профили A/B/C (EN)  
- [architecture.md](architecture.md) — компоненты  
- [routing.md](routing.md) — exclude / include  
- [README.md](../README.md) — обзор продукта  
