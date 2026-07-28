#!/usr/bin/env bash
# Одношаговая установка / обновление outline-gate на текущем хосте.
#
# Использование:
#   cd deploy/compose
#   ./configure.sh          # один раз: ключ и параметры
#   ./install.sh            # сборка + запуск + проверка
#
# Флаги:
#   --configure   запустить configure.sh перед up (интерактивно)
#   --host        принудительно L3 host-профиль
#   --socks       принудительно bridge SOCKS-профиль
#   --no-build    не пересобирать образ
#   --down        остановить и выйти
#   --check       только health-check (без up)
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
SELF="$DIR/install.sh"
cd "$DIR"

DO_CONFIGURE=false
FORCE_PROFILE=""
NO_BUILD=false
DO_DOWN=false
CHECK_ONLY=false

for a in "$@"; do
  case "$a" in
    --configure) DO_CONFIGURE=true ;;
    --host) FORCE_PROFILE=host ;;
    --socks) FORCE_PROFILE=socks ;;
    --no-build) NO_BUILD=true ;;
    --down) DO_DOWN=true ;;
    --check) CHECK_ONLY=true ;;
    -h|--help)
      sed -n '2,20p' "$SELF"
      exit 0
      ;;
    *)
      echo "Неизвестный аргумент: $a (см. --help)" >&2
      exit 2
      ;;
  esac
done

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Нужна команда: $1" >&2
    exit 1
  }
}

need_cmd docker
docker compose version >/dev/null 2>&1 || {
  echo "Нужен Docker Compose v2 (docker compose)" >&2
  exit 1
}

env_get() {
  local key="$1" def="${2:-}"
  if [[ -f .env ]] && grep -qE "^${key}=" .env 2>/dev/null; then
    grep -E "^${key}=" .env | head -1 | cut -d= -f2-
  else
    printf '%s' "$def"
  fi
}

ensure_config_files() {
  mkdir -p config secrets
  if [[ ! -f config/bypass.rules.txt ]]; then
    if [[ -f config/bypass.rules.example.txt ]]; then
      cp config/bypass.rules.example.txt config/bypass.rules.txt
    else
      printf '# outline-gate user bypass rules (managed by UI/API)\n' > config/bypass.rules.txt
    fi
  fi
  [[ -f config/bypass.txt ]] || { [[ -f config/bypass.example.txt ]] && cp config/bypass.example.txt config/bypass.txt || touch config/bypass.txt; }
  [[ -f config/tunnel.txt ]] || { [[ -f config/tunnel.example.txt ]] && cp config/tunnel.example.txt config/tunnel.txt || touch config/tunnel.txt; }
  # stub key file so volume mount always succeeds
  if [[ ! -f secrets/outline_key.txt ]]; then
    printf '# unused when OUTLINE_ACCESS_KEY is set in .env\n' > secrets/outline_key.txt
    chmod 600 secrets/outline_key.txt || true
  fi
}

compose_files() {
  local profile
  profile="${FORCE_PROFILE:-$(env_get COMPOSE_PROFILE socks)}"
  case "$profile" in
    host|HOST|l3|L3|2)
      printf '%s' "-f docker-compose.host.yml"
      ;;
    *)
      printf '%s' "-f docker-compose.yml"
      ;;
  esac
}

health_port() {
  local profile
  profile="${FORCE_PROFILE:-$(env_get COMPOSE_PROFILE socks)}"
  case "$profile" in
    host|HOST|l3|L3|2) echo 8080 ;;
    *) env_get HOST_HEALTH_PORT 28080 ;;
  esac
}

socks_port() {
  local profile
  profile="${FORCE_PROFILE:-$(env_get COMPOSE_PROFILE socks)}"
  case "$profile" in
    host|HOST|l3|L3|2) echo 1080 ;;
    *) env_get HOST_SOCKS_PORT 1080 ;;
  esac
}

check_ready() {
  local port tries i
  port="$(health_port)"
  tries=30
  echo "Ожидание /readyz на :${port} ..."
  for i in $(seq 1 "$tries"); do
    if curl -sf "http://127.0.0.1:${port}/readyz" >/dev/null 2>&1; then
      echo "OK: $(curl -s "http://127.0.0.1:${port}/readyz")"
      return 0
    fi
    sleep 2
  done
  echo "WARN: /readyz не стал ready за $((tries * 2))s — смотрите: docker compose $(compose_files) logs --tail=80" >&2
  return 1
}

CF="$(compose_files)"
# shellcheck disable=SC2086
set -- $CF

if $DO_DOWN; then
  echo "Остановка: docker compose $* down"
  docker compose "$@" down
  exit 0
fi

if $CHECK_ONLY; then
  check_ready
  exit $?
fi

if $DO_CONFIGURE || [[ ! -f .env ]]; then
  if [[ ! -f .env ]]; then
    echo ".env отсутствует — запускаю configure.sh"
  fi
  if [[ -t 0 ]]; then
    chmod +x ./configure.sh
    ./configure.sh
    # re-resolve profile after configure
    CF="$(compose_files)"
    # shellcheck disable=SC2086
    set -- $CF
  else
    echo "Нет TTY и нет .env: скопируйте .env.example → .env и заполните OUTLINE_ACCESS_KEY" >&2
    exit 1
  fi
fi

ensure_config_files

# basic validation
key="$(env_get OUTLINE_ACCESS_KEY)"
key_file_host="$(env_get OUTLINE_KEY_HOST_PATH ./secrets/outline_key.txt)"
if [[ -z "$key" ]]; then
  if [[ ! -f "$key_file_host" ]] || ! grep -vE '^[[:space:]]*(#|$)' "$key_file_host" | head -1 | grep -qE '^(ss|ssconf)://'; then
    # also accept persist file written by UI
    if [[ ! -f config/outline_key.runtime.txt ]]; then
      echo "Нет ключа Outline: задайте OUTLINE_ACCESS_KEY в .env или secrets/outline_key.txt" >&2
      echo "  ./configure.sh" >&2
      exit 1
    fi
  fi
fi

if [[ "$(env_get UI_ENABLE false)" == "true" && -z "$(env_get UI_TOKEN)" ]]; then
  echo "UI_ENABLE=true, но UI_TOKEN пуст — задайте токен в .env" >&2
  exit 1
fi

echo "Профиль compose: docker compose $*"
if $NO_BUILD; then
  docker compose "$@" up -d
else
  docker compose "$@" up --build -d
fi

docker compose "$@" ps
check_ready || true

hp="$(health_port)"
sp="$(socks_port)"
echo
echo "=== Дальше ==="
echo "  Health:  curl -s http://127.0.0.1:${hp}/readyz"
echo "  SOCKS:   curl -s --socks5h 127.0.0.1:${sp} https://ifconfig.me"
if [[ "$(env_get UI_ENABLE false)" == "true" ]]; then
  echo "  Web UI:  http://127.0.0.1:${hp}/ui/"
fi
echo "  Logs:    docker compose $* logs -f --tail=100"
echo "  Stop:    ./install.sh --down"
echo
echo "Полная инструкция: docs/DEPLOY.ru.md"
