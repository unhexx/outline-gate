# outline-gate

<p align="center">
  <a href="https://github.com/unhexx/outline-gate/actions/workflows/ci.yml"><img src="https://github.com/unhexx/outline-gate/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI"></a>
  <a href="https://github.com/unhexx/outline-gate/releases/latest"><img src="https://img.shields.io/github/v/release/unhexx/outline-gate?display_name=tag&sort=semver" alt="Release"></a>
  <a href="https://github.com/unhexx/outline-gate/releases/latest"><img src="https://img.shields.io/github/release-date/unhexx/outline-gate" alt="Release date"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/unhexx/outline-gate" alt="Go"></a>
  <a href="https://pkg.go.dev/github.com/unhexx/outline-gate"><img src="https://pkg.go.dev/badge/github.com/unhexx/outline-gate.svg" alt="Go Reference"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/unhexx/outline-gate" alt="License"></a>
</p>

**Current release: [v0.6.0](https://github.com/unhexx/outline-gate/releases/latest)** · [Changelog](CHANGELOG.md) · [Binary `linux/amd64`](https://github.com/unhexx/outline-gate/releases/latest/download/outline-gate_linux_amd64)

## О продукте

**outline-gate** — самодостаточный Docker-шлюз, который превращает [Outline](https://getoutline.org/) (Shadowsocks) access key в **рабочий VPN-доступ для LAN и приложений**: без клиентского GUI на каждом устройстве, с **split-tunnel** и **быстрым управлением исключениями**.

Один контейнер на Linux-хосте:

1. поднимает клиент Outline (`ss://` / `ssconf://`);
2. отдаёт **SOCKS5** для выборочного proxy;
3. опционально становится **default gateway** (L3 + nftables) для всего TCP-трафика сети;
4. даёт **Web UI** для списка «не через VPN» и смены ключа без пересборки образа.

### Какие задачи решает оперативно

| Задача | Как outline-gate закрывает |
|--------|----------------------------|
| **VPN «на всю сеть»** без Outline Client на TV, IoT, смартфонах | L3: клиенты ставят GW на хост — трафик идёт через Outline |
| **VPN только для части приложений** | SOCKS5 `:1080` в браузере, curl, Git, Docker — остальное без proxy |
| **Split-tunnel: «всё через VPN, кроме…»** | Режим `exclude` + bypass (RFC1918, IP/CIDR, домены, `*.mask`) |
| **Split-tunnel: «через VPN только выбранное»** | Режим `include` + `TUNNEL_CIDRS` (+ `direct` / `drop` для остального) |
| **Не ломать локальную сеть и банки / внутренние API** | Always-bypass частных сетей + UI/API-список исключений |
| **Сменить Outline-ключ без деплоя** | Web UI / `PUT /api/v1/outline` → reconnect + persist-файл |
| **Быстро добавить «не гонять через VPN»** | Web UI: IP, подсеть, `example.com`, `*.cdn.example.net` |
| **Заблокировать домены / адреса / маски** | Вкладка **Блок**; совпадения в логе как «Блок» |
| **Проверить, что туннель жив** | `/readyz`, healthcheck Docker, egress-check через SOCKS |
| **Единый сервис вместо зоопарка клиентов** | Compose + `.env` / secrets; ключ не вшит в образ |
| **Динамический ключ провайдера** | `ssconf://` раскрывается при Connect и периодически перечитывается; TCP-probe туннеля сбрасывает застрявший dialer |

### Для кого

- **Дом / малый офис** — один always-on Linux (NUC, mini-PC, VM): «роутер с Outline».
- **Разработка и ops** — SOCKS для CLI/IDE/контейнеров, без смены системного VPN.
- **Админы LAN** — централизованный egress и политика exclude/include, не per-device apps.

### Чем не является

- Не **Outline Server / Manager** — только **клиент** к уже выданному ключу.
- Не полноценный **DNS-over-VPN** и не полный **UDP/L3** (v0.6 — TCP-first; IPv6 L3 nft — gap).
- Не multi-user IdP: Web UI защищается **одним `UI_TOKEN`**, SOCKS **без пароля** (LAN + опционально `SOCKS_ALLOW_CIDRS`).

<p align="center">
  <img src="docs/images/architecture-overview.svg" alt="Архитектура outline-gate: SOCKS5 и L3 gateway" width="920"/>
</p>

## Возможности (технически)

- Клиент Outline через **outline-sdk** (`ss://`, `ssconf://`)
- **SOCKS5** (`:1080`) — explicit proxy; bypass → direct dial
- **L3 gateway** (nftables): `exclude` / `include`, REDIRECT + MASQUERADE
- **Web UI** (`/ui/`): компактный UI, live-лог, bypass, замена ключа; версия процесса из `/api/v1/version`
- Конфиг: `.env`, volume-файлы, Docker secrets, SIGHUP-reload
- Health: `/healthz`, `/readyz`; опционально Prometheus `/metrics` (`METRICS_ENABLE=true`)

## Оглавление

1. [О продукте](#о-продукте)
2. [Быстрый старт](#быстрый-старт)
   - [Прокси и шлюз одновременно](#прокси-и-шлюз-одновременно)
3. [Проверка подключения](#проверка-подключения)
4. [Релиз и установка](#релиз-и-установка)
5. [SOCKS5 vs L3 — что выбрать](#socks5-vs-l3--что-выбрать)
6. [Использование SOCKS5](#использование-socks5)
7. [Использование L3 gateway](#использование-l3-gateway)
8. [Web UI](#web-ui)
9. [Переменные окружения](#основные-переменные)
10. [Сборка образа](#сборка-образа)
11. [Best practices](#best-practices)
12. [Документация](#документация)

---

## Быстрый старт

Подробно: **[docs/DEPLOY.ru.md](docs/DEPLOY.ru.md)**.

```bash
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate
./install.sh 'ss://YOUR_OUTLINE_KEY'

curl -s http://127.0.0.1:28080/readyz
# затем — [проверка подключения](#проверка-подключения)
```

L3-шлюз (host network; **SOCKS5 тоже слушается**):

```bash
./install.sh --host 'ss://YOUR_OUTLINE_KEY'
```

### Прокси и шлюз одновременно

`--host` поднимает **один** процесс с обоими режимами: SOCKS5 для приложений и L3-шлюз для LAN. Отдельный контейнер под прокси не нужен.

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
UI_TOKEN=ваш-секрет
```

| Что | Куда |
|-----|------|
| SOCKS5 | `HOST:1080` — браузер, curl, Git ([использование SOCKS5](#использование-socks5)) |
| L3 gateway | default gateway клиентов = IP хоста ([использование L3](#использование-l3-gateway)) |
| Web UI / health | `http://HOST:8080/ui/` (порт = `HEALTH_LISTEN`) |

Проверка:

```bash
curl -s http://127.0.0.1:8080/readyz
curl -s --socks5h 127.0.0.1:1080 https://ifconfig.me
# затем — [проверка подключения](#проверка-подключения)
```

Не запускайте рядом `docker-compose.yml` и `docker-compose.host.yml`: контейнер один (`outline-gate`), порты 1080 и health пересекаются.  
Bridge-профиль (`GATEWAY_ENABLE=true` без host-сети) не станет LAN-шлюзом «из коробки».

Bridge-сеть `outline-gate_net` = `192.168.102.0/24` (явный IPAM; не расходует Docker `default-address-pools`).  
Пример daemon: [`deploy/docker/daemon.json.example`](deploy/docker/daemon.json.example).

---

## Проверка подключения

На профиле **SOCKS-only** (`GATEWAY_ENABLE=false`) хост **не** маршрутизирует трафик в туннель сам. В VPN попадает только клиент, который **явно** использует SOCKS. Прямой запрос с хоста всегда показывает ISP-путь — это не поломка.

### 1. Процесс жив

```bash
curl -fsS --max-time 5 "http://127.0.0.1:${HOST_HEALTH_PORT:-28080}/readyz"
```

Ожидание: HTTP 200 и `"ready":true`. Иначе смотрите `docker compose logs` — ключ, сеть до Outline, ssconf.

### 2. Сравнить путь хоста и путь SOCKS

Нужен любой **HTTPS IP-echo** (сервис, который отвечает телом с адресом клиента). URL в переменную, значения **не печатать**:

```bash
# задайте свой echo-URL
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

| Результат | Значение |
|-----------|----------|
| строки **разные** | трафик через SOCKS ушёл в Outline |
| строки **одинаковые** | запрос не через туннель (нет proxy, fallback, или `GATEWAY_ENABLE=false` и клиент ходил напрямую) |
| ошибка curl / SOCKS `0x08` | часто локальный резолв в IPv6: нужен **`socks5h`**, не `socks5` |

**`socks5h`** (и `curl --socks5-hostname`) резолвит имя **на стороне proxy**. Схема `socks5://` резолвит на хосте; при AAAA outline-gate отклоняет IPv6 CONNECT — клиент может уйти в обход прокси.

В браузере: SOCKS5 + **DNS through SOCKS**. В журнале UI (**Лог**) успешная проверка — цепочка `SOCKS → VPN`.

### 3. Чего не делать

- Не судить по прямому curl/браузеру на хосте: без SOCKS это всегда ISP.
- Не вставлять в отчёты и тикеты полученные адреса.
- Не путать готовность процесса (`/readyz`) с тем, что *ваше* приложение настроено на `:1080`.

---

## Релиз и установка

| Канал | Ссылка |
|-------|--------|
| GitHub Releases | https://github.com/unhexx/outline-gate/releases |
| Latest tag | [`v0.6.0`](https://github.com/unhexx/outline-gate/releases/tag/v0.6.0) |
| Changelog | [CHANGELOG.md](CHANGELOG.md) |
| Module path | `github.com/unhexx/outline-gate` |
| Internal git | `https://git.aservice24.ru/scm/expert/outline-gate.git` (ветка `master`, tag `v0.6.0`) |

### Docker (рекомендуется)

```bash
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate
./install.sh 'ss://...'          # или: git checkout v0.6.0 && ./install.sh 'ss://...'
```

Образ с меткой релиза:

```bash
docker build -f deploy/docker/Dockerfile --build-arg VERSION=0.6.0 -t outline-gate:v0.6.0 .
```

### Бинарник Linux amd64

```bash
curl -fsSL -o outline-gate \
  https://github.com/unhexx/outline-gate/releases/download/v0.6.0/outline-gate_linux_amd64
chmod +x outline-gate
export OUTLINE_ACCESS_KEY='ss://...'
./outline-gate
```

Требуется Linux (для L3 — root/`NET_ADMIN` + `nft`). Для production предпочтителен Docker-образ.

---

## SOCKS5 vs L3 — что выбрать

<p align="center">
  <img src="docs/images/compare-modes.svg" alt="Сравнение SOCKS5 и L3 gateway" width="920"/>
</p>

| Критерий | SOCKS5 | L3 gateway |
|----------|--------|------------|
| Настройка клиента | proxy в приложении | default gateway / static route |
| Охват | только apps с proxy | почти весь TCP LAN-трафик |
| Compose | `docker-compose.yml`, `GATEWAY_ENABLE=false` | `docker-compose.host.yml`, `GATEWAY_ENABLE=true` |
| Привилегии | обычный Docker | `NET_ADMIN`, nftables, часто `network_mode: host` |
| Домены в bypass | **точный** match hostname | DNS → IP (best-effort) |
| UDP | не в v1 | не в v1 (TCP-first) |
| Типичный кейс | ноутбук, браузер, CLI | TV, IoT, «роутер с VPN» |

**Можно использовать оба сразу:** L3 для устройств без proxy + SOCKS для приложений на том же хосте. Быстрый старт: [прокси и шлюз одновременно](#прокси-и-шлюз-одновременно).

---

## Использование SOCKS5

<p align="center">
  <img src="docs/images/socks5-flow.svg" alt="Поток SOCKS5" width="920"/>
</p>

### 1. Запуск (bridge, только SOCKS)

`deploy/compose/.env`:

```bash
OUTLINE_ACCESS_KEY=ss://...@server:port
ROUTING_MODE=exclude
GATEWAY_ENABLE=false
SOCKS_LISTEN=0.0.0.0:1080
HOST_SOCKS_PORT=1080
HOST_HEALTH_PORT=28080
UI_ENABLE=true
UI_TOKEN=ваш-секрет
```

```bash
cd deploy/compose
docker compose up --build -d
curl -s http://127.0.0.1:28080/readyz
```

Дальше — [проверка подключения](#проверка-подключения) (`socks5h`, сравнение путей без печати адресов).

### 2. curl / wget

```bash
# HTTP(S) через SOCKS (DNS на стороне proxy)
curl -x socks5h://127.0.0.1:1080 "$IP_ECHO"

# эквивалент
curl --socks5-hostname 127.0.0.1:1080 "$IP_ECHO"

export ALL_PROXY=socks5h://127.0.0.1:1080
curl -fsS "$IP_ECHO" >/dev/null
unset ALL_PROXY
```

С другого хоста LAN подставьте адрес машины с outline-gate вместо loopback (тот же `socks5h` и порт `HOST_SOCKS_PORT`).

### 3. Firefox

1. **Settings → Network Settings → Settings…**
2. **Manual proxy configuration**
3. **SOCKS Host:** `127.0.0.1` (или IP gate), **Port:** `1080`
4. Выберите **SOCKS v5**
5. Включите **Proxy DNS when using SOCKS v5**
6. OK → откройте любой IP-echo по HTTPS и сравните с запросом *без* proxy (как в [проверке подключения](#проверка-подключения))

### 4. Chromium / Chrome

Chrome не имеет встроенного SOCKS-UI. Варианты:

```bash
# Linux: отдельный профиль + proxy-server
google-chrome --user-data-dir=/tmp/chrome-socks \
  --proxy-server="socks5://127.0.0.1:1080" \
  --host-resolver-rules="MAP * ~NOTFOUND , EXCLUDE localhost"
```

(Chrome сам резолвит DNS, если не задать resolver-rules; для проверки egress надёжнее curl + `socks5h`.)

Или расширение / системный proxy (зависит от ОС).

### 5. SSH over SOCKS (ProxyCommand / Dynamic)

Если нужен SSH *через* Outline:

```bash
# ssh с ProxyCommand + nc/connect через SOCKS
ssh -o ProxyCommand='nc -X 5 -x 127.0.0.1:1080 %h %p' user@remote-host
```

(набор `nc`/`ncat` зависит от дистрибутива; альтернатива — `connect-proxy`.)

### 6. Git через SOCKS

```bash
git config --global http.proxy socks5h://127.0.0.1:1080
git config --global https.proxy socks5h://127.0.0.1:1080
# отключить:
git config --global --unset http.proxy
git config --global --unset https.proxy
```

### 7. Docker-контейнер, ходящий наружу через SOCKS

```bash
docker run --rm curlimages/curl:latest \
  -x "socks5h://host.docker.internal:${HOST_SOCKS_PORT:-1080}" "$IP_ECHO"
# Linux: добавьте --add-host=host.docker.internal:host-gateway
```

### 8. Bypass в SOCKS

Если destination **совпал** с правилом bypass (IP/CIDR/домен/`*.mask` из UI или `BYPASS_*`), outline-gate dial'ит **напрямую**, минуя Outline. Иначе — через туннель.

```bash
# пример: добавить исключение в UI или:
# BYPASS_CIDRS=8.8.8.8/32
# BYPASS_RULES_FILE=/config/bypass.rules.txt  →  example.com
```

**Предустановленные маски** (шаблон → `config/bypass.rules.txt`):  
`*.max.ru`, `*.aq.ru`, `*.aq.local`, `*.aservice24.ru`, `*.yandex.cloud`, `*.yandex.ru`.

**Простые команды** (нужны `UI_ENABLE=true` + `UI_TOKEN`; порт = `HOST_HEALTH_PORT`):

```bash
PORT=28080
AUTH=(-H "Authorization: Bearer $UI_TOKEN")

# список
curl -s "${AUTH[@]}" "http://127.0.0.1:${PORT}/api/v1/bypass"

# добавить
curl -s -X POST "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"rule":"*.example.com"}' "http://127.0.0.1:${PORT}/api/v1/bypass"

# удалить
curl -s -X DELETE "${AUTH[@]}" \
  "http://127.0.0.1:${PORT}/api/v1/bypass?rule=*.example.com"

# блок-лист (drop)
curl -s -X POST "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"rule":"*.ads.example"}' "http://127.0.0.1:${PORT}/api/v1/block"
```

Без UI: правка `deploy/compose/config/bypass.rules.txt` или `block.rules.txt` + `docker kill -s HUP outline-gate`.  
Подробнее: [docs/OPERATIONS.ru.md §7.2](docs/OPERATIONS.ru.md).

### 9. Безопасность SOCKS

- **Нет пароля SOCKS** в v1 — публиковать `:1080` в интернет **нельзя**.
- Ограничьте firewall: только LAN / `127.0.0.1`.
- Опционально: `SOCKS_ALLOW_CIDRS` (CSV) или `SOCKS_ALLOW_CIDRS_FILE` — allowlist **source IP** клиента. Пусто = принимать всех (как раньше).
- Не коммитьте `.env` с ключами.

```bash
# только loopback и LAN 10.0.0.0/8
SOCKS_ALLOW_CIDRS=127.0.0.0/8,10.0.0.0/8
```

---

## Использование L3 gateway

L3-режим делает хост **маршрутизатором**: LAN-клиенты ставят **default gateway** (или policy route) на IP машины с outline-gate. nftables решает: redirect TCP в transparent proxy → Outline, или оставить direct.

### Предпосылки

- Linux-хост в той же L2/L3 сети, что и клиенты
- `GATEWAY_ENABLE=true`
- Обычно: `docker compose -f docker-compose.host.yml` (`network_mode: host`)
- Capability `NET_ADMIN`, `ip_forward=1`
- Клиенты: IPv4 gateway = LAN IP хоста (пример: `192.168.1.10`)

### Запуск (host network)

`deploy/compose/.env`:

```bash
OUTLINE_ACCESS_KEY=ss://...@server:port
GATEWAY_ENABLE=true
ROUTING_MODE=exclude          # или include
# LAN_INTERFACE=eth0          # опционально, для MASQUERADE oif
HOST_HEALTH_PORT=28080        # при host network порты слушаются напрямую
UI_ENABLE=true
UI_TOKEN=ваш-секрет
```

```bash
cd deploy/compose
docker compose -f docker-compose.host.yml up --build -d
curl -s http://127.0.0.1:8080/readyz   # при host: HEALTH_LISTEN как есть
# или http://192.168.1.10:8080/readyz
```

### Настройка клиента (пример Linux)

```bash
# предположим, хост gate: 192.168.1.10, интерфейс клиента eth0
sudo ip route replace default via 192.168.1.10 dev eth0

# DNS (важно: DNS-утечки не «лечатся» L3 автоматически)
# либо router DNS, либо 1.1.1.1 — осознанно
```

**Windows (GUI):**  
Параметры → Сеть → Свойства адаптера → IPv4 → Шлюз: `192.168.1.10`.

**Android / iOS:**  
Статический IP Wi‑Fi → Router / Gateway = IP host с outline-gate.

### Проверка L3

На клиенте (без SOCKS): тот же скрипт [проверки подключения](#проверка-подключения), но «direct» снимите с машины **вне** этого шлюза. При рабочем exclude egress клиента не должен совпадать с ISP той контрольной машины. RFC1918 остаётся на kernel-path.

На хосте:

```bash
docker logs outline-gate --tail=50
# table nft (host network):
sudo nft list table inet outline_gate
```

Остановка / сброс правил:

```bash
docker compose -f docker-compose.host.yml down
# при аварийном kill:
sudo nft delete table inet outline_gate
```

---

### Режим `exclude` (по умолчанию)

<p align="center">
  <img src="docs/images/l3-exclude.svg" alt="L3 режим exclude" width="920"/>
</p>

**Смысл:** весь TCP (не из bypass) → туннель Outline.  
Идеально: «VPN на всю квартиру, кроме локалки и выбранных сервисов».

```bash
ROUTING_MODE=exclude
GATEWAY_ENABLE=true
# опционально доп. исключения:
BYPASS_CIDRS=203.0.113.0/24
# или через Web UI: example.com, *.cdn.example.net
```

**Логика:**

```text
if dst ∈ bypass (RFC1918 + UI + BYPASS_* + IP Outline-сервера)
    → DIRECT
else
    → TUNNEL (nft REDIRECT → Outline)
```

**Практика: исключить банк / внутренний API**

1. Web UI → добавить `bank.example.com` или `10.50.0.0/16`
2. Или файл `/config/bypass.rules.txt`:

```text
bank.example.com
203.0.113.0/24
```

3. SIGHUP / UI apply / DNS refresh — L3 set обновится.

---

### Режим `include`

<p align="center">
  <img src="docs/images/l3-include.svg" alt="L3 режим include" width="920"/>
</p>

**Смысл:** через VPN **только** адреса из `TUNNEL_*`. Остальное — `DIRECT_POLICY` (`direct` или `drop`).  
Идеально: «только зарубежные сервисы / офисные CIDR через Outline, остальной интернет — как был».

```bash
ROUTING_MODE=include
GATEWAY_ENABLE=true
TUNNEL_CIDRS=8.8.8.8/32,203.0.113.0/24
# или TUNNEL_CIDRS_FILE=/config/tunnel.txt
DIRECT_POLICY=direct
```

**Логика:**

```text
if dst ∈ bypass          → DIRECT
elif dst ∈ tunnel list   → TUNNEL
else                     → DIRECT_POLICY  # direct | drop
```

**Практика: только Google DNS и одна подсеть через VPN**

```bash
# .env
ROUTING_MODE=include
TUNNEL_CIDRS=8.8.8.8/32,8.8.4.4/32,203.0.113.0/24
DIRECT_POLICY=direct
GATEWAY_ENABLE=true
```

На клиенте с GW=gate проще `traceroute` / `tcpdump` по адресам из `TUNNEL_CIDRS`, чем IP-echo: echo-сервис почти наверняка не входит в include-список.

**`DIRECT_POLICY=drop`:** всё, что не bypass и не tunnel, **режется** (жёсткий allow-list). Осторожно: легко «убить» интернет на клиентах, если tunnel-список неполный.

```bash
DIRECT_POLICY=drop
TUNNEL_CIDRS=1.2.3.0/24
# клиент достучится до 1.2.3.0/24 через Outline; 8.8.8.8 — drop
```

---

### L3: ограничения v1

| Тема | Поведение |
|------|-----------|
| Протоколы | TCP redirect; **UDP не полный** |
| Домены в bypass | резолв A/AAAA + refresh (`BYPASS_DNS_REFRESH`); редкие поддомены могут кратко уйти в tunnel |
| IPv6 | **не туннелируется** L3: nft-сеты `ipv4_addr`, IPv6 CIDR в bypass пропускаются; на dual-stack хосте IPv6 идёт **мимо** Outline (direct). SOCKS IPv6 ATYP отвергается. Полный dual-stack — roadmap |
| DNS | не «магический tunnel DNS»; настраивайте DNS на клиентах отдельно |
| Always bypass | RFC1918, CGNAT, link-local, IP сервера Outline |

Подробнее: [`docs/routing.md`](docs/routing.md).

---

## Web UI

<p align="center">
  <img src="docs/images/webui-mockup.svg" alt="Макет Web UI outline-gate" width="720"/>
</p>

### Вход: логин и пароль по умолчанию

**Отдельного логина/пароля нет** — и **нет учётных данных по умолчанию** (`admin` / `password` не существуют).

| Что | По умолчанию |
|-----|----------------|
| Web UI | **выключен** (`UI_ENABLE=false`) |
| Логин | **не используется** |
| Пароль / токен | **задаёте сами** в `UI_TOKEN` |

```bash
# deploy/compose/.env
UI_ENABLE=true
UI_TOKEN=ваш-длинный-случайный-секрет
HOST_HEALTH_PORT=28080
```

```bash
openssl rand -hex 24   # → в UI_TOKEN
docker compose up -d --force-recreate
# http://127.0.0.1:28080/ui/  → поле «Токен доступа» = UI_TOKEN
```

**Авторизация API**

| Способ | Как |
|--------|-----|
| Форма UI | `UI_TOKEN` → sessionStorage → `Authorization: Bearer …` |
| curl | `Authorization: Bearer <UI_TOKEN>` |
| HTTP Basic | username любой (`admin`), **password** = `UI_TOKEN` |

| URL | Auth | Назначение |
|-----|------|------------|
| `/ui/` | токен для API | Web UI (вкладки: Статус · Лог · Bypass · Ключ) |
| `GET /api/v1/status` | Bearer / Basic | сводка ready / SOCKS / gateway / connlog |
| `GET /api/v1/connections` | Bearer / Basic | снимок ring-buffer подключений |
| `GET /api/v1/connections/stream` | Bearer / Basic / `?token=` | SSE live-лог (EventSource) |
| `GET/PUT /api/v1/outline` | Bearer / Basic | статус / замена ключа |
| `GET/POST/DELETE /api/v1/bypass` | Bearer / Basic | правила исключений |
| `GET/POST/DELETE /api/v1/block` | Bearer / Basic | блок-лист (drop + лог) |
| `/healthz`, `/readyz` | нет | healthcheck |

**Лог подключений:** SOCKS и L3 показывают цепочку `клиент → SOCKS|L3 → VPN|Direct → host` (и правило bypass, если известно). На L3 private/RFC1918 остаётся kernel-path без записи в лог; остальной Internet TCP (включая user Direct) идёт через transparent proxy.

Ключ, заменённый в UI → `OUTLINE_KEY_PERSIST_FILE` (по умолчанию `/config/outline_key.runtime.txt`), при старте **приоритетнее** `OUTLINE_ACCESS_KEY`.

---

## Основные переменные

| Variable | Description |
|----------|-------------|
| `OUTLINE_ACCESS_KEY` / `OUTLINE_ACCESS_KEY_FILE` | Ключ Outline `ss://` или `ssconf://` |
| `OUTLINE_KEY_PERSIST_FILE` | Файл ключа после замены в UI |
| `ROUTING_MODE` | `exclude` \| `include` |
| `BYPASS_CIDRS` / `BYPASS_CIDRS_FILE` | Статические CIDR-исключения |
| `BYPASS_RULES_FILE` | User-правила (IP/домены) UI |
| `BLOCK_RULES_FILE` | Блок-лист (IP/CIDR/домен/`*.mask`); drop + запись в лог |
| `TUNNEL_CIDRS` / `TUNNEL_CIDRS_FILE` | Цели (include) |
| `DIRECT_POLICY` | `direct` \| `drop` (include) |
| `GATEWAY_ENABLE` | L3 nftables |
| `UI_ENABLE` / `UI_TOKEN` | Web UI + API |
| `HOST_SOCKS_PORT` / `HOST_HEALTH_PORT` | Порты на хосте (bridge compose) |
| `SOCKS_LISTEN` / `HEALTH_LISTEN` | Слушатели в контейнере |
| `SOCKS_ALLOW_CIDRS` / `_FILE` | Allowlist source IP для SOCKS (пусто = все; при non-loopback — `Warn` на старте) |
| `METRICS_ENABLE` | `true` → Prometheus text на `/metrics` (health-порт) |
| `SSCONF_REFRESH_INTERVAL` | Период перечитывания `ssconf://` (default `2m`, `0` = выкл.) |
| `TUNNEL_PROBE_ADDR` | TCP-probe через туннель (default `1.1.1.1:443`, `off` = выкл.) |
| `TUNNEL_PROBE_INTERVAL` / `_TIMEOUT` / `_FAILS` | Период / дедлайн / порог фейлов до reconnect |
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` |

Полный список: [`deploy/compose/.env.example`](deploy/compose/.env.example).

### Prometheus metrics

Опциональный endpoint **без auth** на том же порту, что health/UI (`HEALTH_LISTEN` / `HOST_HEALTH_PORT`).

```bash
# deploy/compose/.env
METRICS_ENABLE=true

cd deploy/compose && docker compose up -d --force-recreate

curl -s http://127.0.0.1:${HOST_HEALTH_PORT:-8080}/metrics
# outline_gate_up 1
# outline_gate_connections_total{via="tunnel",result="ok"} …
```

Переменная пробрасывается в compose (`METRICS_ENABLE`). Не публикуйте health-порт в интернет без firewall — `/metrics` открыт как `/healthz`.

---

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

---

## Best practices

1. **Секреты** — `.env` / secrets / UI persist-файл; никогда в git и образ.
2. **UI_TOKEN** — длинный случайный; UI/API не в публичный интернет без TLS reverse-proxy.
3. **SOCKS `:1080`** — только LAN / localhost; auth SOCKS в v1 нет.
4. **L3** — всегда auto-bypass IP Outline-сервера; проверяйте после смены ключа.
5. **Домены на L3** — best-effort; для точного hostname-match используйте SOCKS.
6. **Обновления** — `docker compose up --build -d`; rules в volume `./config`.
7. **DNS** — настройте отдельно; L3 не заменяет DoH/DoT-политику.

---

## Документация

| Документ | Содержание |
|----------|------------|
| **[docs/DEPLOY.ru.md](docs/DEPLOY.ru.md)** | Развёртывание на другом хосте (пошагово, RU) |
| **[docs/OPERATIONS.ru.md](docs/OPERATIONS.ru.md)** | Полный справочник развёртывания и эксплуатации (RU) |
| [docs/architecture.md](docs/architecture.md) | Архитектура компонентов |
| [docs/deployment.md](docs/deployment.md) | Профили сети A/B/C |
| [docs/routing.md](docs/routing.md) | Режимы маршрутизации и bypass |
| [docs/images/](docs/images/) | Схемы и иллюстрации |
| [CHANGELOG.md](CHANGELOG.md) | История релизов (Keep a Changelog) |
| [Releases](https://github.com/unhexx/outline-gate/releases) | Бинарники и notes |

## Репозиторий

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

## Безопасность

- SOCKS без auth — только доверенная сеть (`SOCKS_ALLOW_CIDRS` рекомендуется)
- Не коммитьте `.env`, `*.runtime.txt` и реальные ключи
- В логах ключ редактируется (`ss://***@host:port`)
- API UI без токена → `401` (`/api/v1/version` публичный)
- Ограничения v0.6: TCP-first (UDP L3 неполный), IPv6 nft gap, domain-bypass на L3 — best-effort

## License

[MIT](LICENSE)
