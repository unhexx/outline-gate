# Развёртывание outline-gate на другом хосте

**Релиз:** [v0.4.0](https://github.com/unhexx/outline-gate/releases/tag/v0.4.0) · эксплуатация: [OPERATIONS.ru.md](OPERATIONS.ru.md)

| Ресурс | URL |
|--------|-----|
| GitHub | https://github.com/unhexx/outline-gate |
| Internal (aservice) | https://git.aservice24.ru/scm/expert/outline-gate.git |
| Ветка | `main` / `master` |

---

## Быстрая установка (2 шага)

Требуется: Docker Engine + Compose v2, ключ Outline (`ss://` или `ssconf://`).

```bash
# 1) код
git clone https://github.com/unhexx/outline-gate.git
cd outline-gate

# 2) запуск (создаст .env, сеть 192.168.102.0/24, build+up, /readyz)
./install.sh 'ss://YOUR_KEY_HERE'
```

Проверка:

```bash
curl -s http://127.0.0.1:28080/readyz
curl -s --socks5h 127.0.0.1:1080 https://ifconfig.me
```

L3-шлюз (host network):

```bash
./install.sh --host 'ss://YOUR_KEY_HERE'
```

Обновление на уже установленном хосте:

```bash
git pull
./install.sh                  # ключ уже в deploy/compose/.env
# или: ./install.sh --no-build
```

---

## Docker daemon на хосте (рекомендуется)

На узлах с **узким** `default-address-pools` (один `/24`) auto-сети Docker быстро заканчиваются
(`all predefined address pools have been fully subnetted`).

Рекомендуемый `/etc/docker/daemon.json` (см. `deploy/docker/daemon.json.example`):

```json
{
  "bip": "192.168.100.1/24",
  "fixed-cidr": "192.168.100.0/24",
  "default-address-pools": [
    { "base": "192.168.101.0/24", "size": 24 }
  ]
}
```

| Диапазон | Назначение |
|----------|------------|
| `192.168.100.0/24` | docker0 (`bip` / `fixed-cidr`) |
| `192.168.101.0/24` | auto-сети других compose-проектов |
| `192.168.102.0/24` | **outline-gate** — явный IPAM в `docker-compose.yml` (не берёт из pool) |

После правки daemon: `sudo systemctl restart docker`.

Переопределение подсети outline-gate: `COMPOSE_SUBNET` / `COMPOSE_GATEWAY` в `deploy/compose/.env`.

---

## Что получите

1. Контейнер **outline-gate** (`ss://` / `ssconf://`).
2. **SOCKS5** на `:1080` (bridge) или host network.
3. Опционально **L3-шлюз** (`./install.sh --host`).
4. Опционально **Web UI** — после install: `UI_ENABLE=true` + `UI_TOKEN` в `.env`, затем `./install.sh --no-build`.

Секреты **не** в git.

---

## Требования

| Требование | Минимум |
|------------|---------|
| ОС | Linux x86_64 |
| Docker | Engine 20+ + **Compose v2** |
| Порты | `1080` (SOCKS), `28080` (health; host-профиль — `8080`) |
| Ключ | `ss://...` или `ssconf://...` |

```bash
docker --version && docker compose version
```

---

## Профили

| Профиль | Compose | Команда |
|---------|---------|---------|
| **socks** (bridge) | `docker-compose.yml` | `./install.sh 'ss://...'` |
| **host** (L3) | `docker-compose.host.yml` | `./install.sh --host 'ss://...'` |

---

## Интерактив / ручная настройка

```bash
./install.sh --configure          # wizard → .env → up
# или
cd deploy/compose && cp .env.example .env && $EDITOR .env && ./install.sh
```

Файлы runtime:

| Путь | Назначение |
|------|------------|
| `deploy/compose/.env` | env (ключ, порты, `COMPOSE_SUBNET`) |
| `secrets/outline_key.txt` | опционально ключ-файл |
| `config/bypass.rules.txt` | правила UI |
| `config/outline_key.runtime.txt` | ключ после замены в UI |

Флаги:

```bash
./install.sh --configure | --host | --socks | --check | --down | --no-build
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
| `all predefined address pools have been fully subnetted` | outline-gate **не** использует pool: сеть `outline-gate_net` = `192.168.102.0/24` (явный IPAM). Обновите код (`git pull`) и `./install.sh`. Конфликт подсети → смените `COMPOSE_SUBNET`/`COMPOSE_GATEWAY`. Рекомендуемый daemon: `deploy/docker/daemon.json.example`. Host-профиль bridge не создаёт. |
| Permission denied на `config/*` | файлы от root из контейнера: `docker run --rm -v $PWD/config:/c alpine chown -R $(id -u):$(id -g) /c` |

---

## Чеклист «новый хост»

- [ ] Docker + Compose v2 (опц. `daemon.json` из `deploy/docker/daemon.json.example`)
- [ ] `git clone` + `./install.sh 'ss://...'`
- [ ] `curl` `/readyz` + SOCKS `ifconfig.me`
- [ ] Firewall: только LAN на 1080/UI
- [ ] (опц.) `./install.sh --host` для L3
- [ ] (опц.) бэкап `.env` + `config/bypass.rules.txt`

---

## Связанные документы

- [OPERATIONS.ru.md](OPERATIONS.ru.md) — полный справочник env, API, эксплуатация  
- [deployment.md](deployment.md) — профили A/B/C (EN)  
- [architecture.md](architecture.md) — компоненты  
- [routing.md](routing.md) — exclude / include  
- [README.md](../README.md) — обзор продукта  
