#!/usr/bin/env bash
# Установка / обновление outline-gate (1–2 шага).
#
# Из корня репозитория или из deploy/compose:
#   bash install.sh 'ss://...'              # SOCKS (bridge); без +x / noexec OK
#   bash install.sh --host 'ssconf://...'
#   OUTLINE_ACCESS_KEY='ss://...' bash install.sh
#   bash install.sh --configure             # интерактив (configure.sh)
#
# Прочее:
#   bash install.sh --down | --check | --no-build | --socks | --host
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
SELF="$DIR/install.sh"
cd "$DIR"

DO_CONFIGURE=false
FORCE_PROFILE=""
NO_BUILD=false
DO_DOWN=false
CHECK_ONLY=false
CLI_KEY=""

for a in "$@"; do
  case "$a" in
    --configure) DO_CONFIGURE=true ;;
    --host) FORCE_PROFILE=host ;;
    --socks) FORCE_PROFILE=socks ;;
    --no-build) NO_BUILD=true ;;
    --down) DO_DOWN=true ;;
    --check) CHECK_ONLY=true ;;
    -h|--help)
      sed -n '2,16p' "$SELF"
      exit 0
      ;;
    ss://*|ssconf://*)
      CLI_KEY="$a"
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

set_env() {
  local key="$1" val="$2"
  if [[ ! -f .env ]]; then
    printf '%s=%s\n' "$key" "$val" > .env
    chmod 600 .env || true
    return
  fi
  if grep -qE "^${key}=" .env 2>/dev/null; then
    local esc
    esc=$(printf '%s' "$val" | sed 's/[&|\\]/\\&/g')
    if sed --version >/dev/null 2>&1; then
      sed -i -E "s|^${key}=.*|${key}=${esc}|" .env
    else
      sed -i '' -E "s|^${key}=.*|${key}=${esc}|" .env
    fi
  else
    printf '%s=%s\n' "$key" "$val" >> .env
  fi
}

ensure_env_file() {
  if [[ -f .env ]]; then
    return 0
  fi
  if [[ -f .env.example ]]; then
    cp .env.example .env
    chmod 600 .env || true
    echo "Создан .env из .env.example"
  else
    touch .env
    chmod 600 .env || true
  fi
}

# Неинтерактивный bootstrap: ключ из argv / env → .env + дефолты сети под daemon.json.example
bootstrap_from_key() {
  local key="$1"
  ensure_env_file
  set_env OUTLINE_ACCESS_KEY "$key"

  # Профиль
  if [[ -n "$FORCE_PROFILE" ]]; then
    set_env COMPOSE_PROFILE "$FORCE_PROFILE"
  else
    [[ -z "$(env_get COMPOSE_PROFILE)" ]] && set_env COMPOSE_PROFILE "socks"
  fi
  case "$(env_get COMPOSE_PROFILE socks)" in
    host|HOST|l3|L3)
      set_env GATEWAY_ENABLE "true"
      ;;
    *)
      [[ -z "$(env_get GATEWAY_ENABLE)" ]] && set_env GATEWAY_ENABLE "false"
      # Явный IPAM: bip 192.168.100/24, pool 192.168.101/24 → outline-gate 192.168.102/24
      [[ -z "$(env_get COMPOSE_SUBNET)" ]] && set_env COMPOSE_SUBNET "192.168.102.0/24"
      [[ -z "$(env_get COMPOSE_GATEWAY)" ]] && set_env COMPOSE_GATEWAY "192.168.102.1"
      [[ -z "$(env_get HOST_SOCKS_PORT)" ]] && set_env HOST_SOCKS_PORT "1080"
      [[ -z "$(env_get HOST_HEALTH_PORT)" ]] && set_env HOST_HEALTH_PORT "28080"
      ;;
  esac
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

# --- network helpers: явная сеть, не зависит от default-address-pools ---
ensure_bridge_network_defaults() {
  local profile
  profile="${FORCE_PROFILE:-$(env_get COMPOSE_PROFILE socks)}"
  case "$profile" in
    host|HOST|l3|L3|2) return 0 ;;
  esac
  ensure_env_file
  [[ -z "$(env_get COMPOSE_SUBNET)" ]] && set_env COMPOSE_SUBNET "192.168.102.0/24"
  [[ -z "$(env_get COMPOSE_GATEWAY)" ]] && set_env COMPOSE_GATEWAY "192.168.102.1"
}

compose_up() {
  # shellcheck disable=SC2086
  if $NO_BUILD; then
    docker compose "$@" up -d
  else
    docker compose "$@" up --build -d
  fi
}

# --- main ---
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

# Ключ: argv → env → .env / secrets
if [[ -n "$CLI_KEY" ]]; then
  bootstrap_from_key "$CLI_KEY"
elif [[ -n "${OUTLINE_ACCESS_KEY:-}" ]]; then
  bootstrap_from_key "$OUTLINE_ACCESS_KEY"
elif $DO_CONFIGURE || [[ ! -f .env ]]; then
  if [[ ! -f .env ]]; then
    echo ".env отсутствует — запускаю configure.sh"
  fi
  if [[ -t 0 ]]; then
    # bash, не ./ — не требует +x / noexec
    bash ./configure.sh
    CF="$(compose_files)"
    # shellcheck disable=SC2086
    set -- $CF
  else
    echo "Нет TTY и нет ключа." >&2
    echo "  bash install.sh 'ss://...'   или   OUTLINE_ACCESS_KEY=ss://... bash install.sh" >&2
    exit 1
  fi
fi

ensure_bridge_network_defaults
ensure_config_files

# re-resolve profile after env may have changed
CF="$(compose_files)"
# shellcheck disable=SC2086
set -- $CF

key="$(env_get OUTLINE_ACCESS_KEY)"
key_file_host="$(env_get OUTLINE_KEY_HOST_PATH ./secrets/outline_key.txt)"
if [[ -z "$key" ]]; then
  if [[ ! -f "$key_file_host" ]] || ! grep -vE '^[[:space:]]*(#|$)' "$key_file_host" | head -1 | grep -qE '^(ss|ssconf)://'; then
    if [[ ! -f config/outline_key.runtime.txt ]]; then
      echo "Нет ключа Outline. Один из вариантов:" >&2
      echo "  bash install.sh 'ss://...'" >&2
      echo "  OUTLINE_ACCESS_KEY=ss://... bash install.sh" >&2
      echo "  bash install.sh --configure" >&2
      exit 1
    fi
  fi
fi

if [[ "$(env_get UI_ENABLE false)" == "true" && -z "$(env_get UI_TOKEN)" ]]; then
  echo "UI_ENABLE=true, но UI_TOKEN пуст — задайте токен в .env" >&2
  exit 1
fi

echo "Профиль compose: docker compose $*"
echo "Сеть (bridge): COMPOSE_SUBNET=$(env_get COMPOSE_SUBNET 192.168.102.0/24) GATEWAY=$(env_get COMPOSE_GATEWAY 192.168.102.1)"

if ! compose_up "$@"; then
  echo >&2
  echo "Ошибка docker compose up." >&2
  echo "Если «all predefined address pools have been fully subnetted»:" >&2
  echo "  — bridge-профиль уже использует явный COMPOSE_SUBNET (не пул Docker);" >&2
  echo "  — проверьте .env: COMPOSE_SUBNET/COMPOSE_GATEWAY не пересекаются с bip/LAN;" >&2
  echo "  — пример daemon: deploy/docker/daemon.json.example" >&2
  echo "  — или: docker network prune  (удалит неиспользуемые сети)" >&2
  exit 1
fi

docker compose "$@" ps
check_ready || true

hp="$(health_port)"
sp="$(socks_port)"
echo
echo "=== Готово ==="
echo "  Health:  curl -s http://127.0.0.1:${hp}/readyz"
echo "  SOCKS:   curl -s --socks5h 127.0.0.1:${sp} https://ifconfig.me"
if [[ "$(env_get UI_ENABLE false)" == "true" ]]; then
  echo "  Web UI:  http://127.0.0.1:${hp}/ui/"
fi
echo "  Logs:    docker compose $* logs -f --tail=100"
echo "  Stop:    bash install.sh --down"
echo
echo "Подробнее: docs/DEPLOY.ru.md"
