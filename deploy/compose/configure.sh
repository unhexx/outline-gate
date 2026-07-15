#!/usr/bin/env bash
# Интерактивная настройка .env и файла ключа для docker compose.
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$DIR"

ENV_FILE="$DIR/.env"
EXAMPLE="$DIR/.env.example"
SECRETS_DIR="$DIR/secrets"
KEY_FILE="$SECRETS_DIR/outline_key.txt"

mkdir -p "$SECRETS_DIR" "$DIR/config"

if [[ ! -f "$ENV_FILE" ]]; then
  if [[ -f "$EXAMPLE" ]]; then
    cp "$EXAMPLE" "$ENV_FILE"
    echo "Создан $ENV_FILE из .env.example"
  else
    touch "$ENV_FILE"
  fi
fi

# --- helpers ---
set_env() {
  local key="$1" val="$2"
  if grep -qE "^${key}=" "$ENV_FILE" 2>/dev/null; then
    # escape for sed
    local esc
    esc=$(printf '%s' "$val" | sed 's/[&|\\]/\\&/g')
    if sed --version >/dev/null 2>&1; then
      sed -i -E "s|^${key}=.*|${key}=${esc}|" "$ENV_FILE"
    else
      sed -i '' -E "s|^${key}=.*|${key}=${esc}|" "$ENV_FILE"
    fi
  else
    printf '%s=%s\n' "$key" "$val" >> "$ENV_FILE"
  fi
}

prompt() {
  local var="$1" msg="$2" def="${3:-}"
  local cur=""
  if grep -qE "^${var}=" "$ENV_FILE" 2>/dev/null; then
    cur=$(grep -E "^${var}=" "$ENV_FILE" | head -1 | cut -d= -f2-)
  fi
  local show="$def"
  [[ -n "$cur" ]] && show="$cur"
  local ans
  if [[ -n "$show" ]]; then
    read -r -p "$msg [$show]: " ans || true
  else
    read -r -p "$msg: " ans || true
  fi
  if [[ -z "$ans" ]]; then
    ans="$show"
  fi
  printf '%s' "$ans"
}

echo "=== outline-gate: настройка ==="
echo

# Key
KEY_METHOD=$(prompt KEY_METHOD "Ключ: (1) ввести ss:// или ssconf:// в .env  (2) сохранить в secrets/outline_key.txt" "1")
case "$KEY_METHOD" in
  2)
    if [[ -t 0 ]]; then
      read -r -p "Вставьте Outline access key (ss://... или ssconf://...): " ACCESS_KEY
    else
      echo "Нет TTY — задайте OUTLINE_ACCESS_KEY в .env вручную" >&2
      exit 1
    fi
    if [[ -z "${ACCESS_KEY// }" ]]; then
      echo "Пустой ключ" >&2
      exit 1
    fi
    printf '%s\n' "$ACCESS_KEY" > "$KEY_FILE"
    chmod 600 "$KEY_FILE"
    set_env OUTLINE_ACCESS_KEY ""
    set_env OUTLINE_ACCESS_KEY_FILE "/run/secrets/outline_key"
    set_env OUTLINE_KEY_HOST_PATH "./secrets/outline_key.txt"
    echo "Ключ записан в $KEY_FILE"
    ;;
  *)
    if [[ -t 0 ]]; then
      read -r -p "Вставьте Outline access key (ss://... или ssconf://...): " ACCESS_KEY
    else
      echo "Нет TTY — задайте OUTLINE_ACCESS_KEY в .env вручную" >&2
      exit 1
    fi
    if [[ -z "${ACCESS_KEY// }" ]]; then
      echo "Пустой ключ" >&2
      exit 1
    fi
    set_env OUTLINE_ACCESS_KEY "$ACCESS_KEY"
    # Заглушка файла, чтобы volume mount не падал
    if [[ ! -f "$KEY_FILE" ]]; then
      printf '# unused when OUTLINE_ACCESS_KEY is set\n' > "$KEY_FILE"
      chmod 600 "$KEY_FILE"
    fi
    set_env OUTLINE_ACCESS_KEY_FILE ""
    echo "Ключ записан в .env (файл не коммитьте)"
    ;;
esac

MODE=$(prompt ROUTING_MODE "ROUTING_MODE (exclude|include)" "exclude")
set_env ROUTING_MODE "$MODE"

if [[ "$MODE" == "include" ]]; then
  TC=$(prompt TUNNEL_CIDRS "TUNNEL_CIDRS (через запятую, напр. 8.8.8.8/32,1.1.1.0/24)" "")
  set_env TUNNEL_CIDRS "$TC"
  if [[ -n "$TC" ]]; then
    # также в файл для наглядности
    echo "$TC" | tr ',' '\n' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | grep -v '^$' > "$DIR/config/tunnel.txt" || true
  fi
fi

BC=$(prompt BYPASS_CIDRS "Доп. BYPASS_CIDRS (через запятую, Enter = пропуск)" "")
[[ -n "$BC" ]] && set_env BYPASS_CIDRS "$BC"

GW=$(prompt GATEWAY_ENABLE "GATEWAY_ENABLE (true для L3 host-профиля, false для SOCKS)" "false")
set_env GATEWAY_ENABLE "$GW"

DP=$(prompt DIRECT_POLICY "DIRECT_POLICY (direct|drop)" "direct")
set_env DIRECT_POLICY "$DP"

LL=$(prompt LOG_LEVEL "LOG_LEVEL (debug|info|warn|error)" "info")
set_env LOG_LEVEL "$LL"

echo
echo "Готово. Файлы:"
echo "  $ENV_FILE"
echo "  $KEY_FILE (если использовали файл ключа)"
echo
echo "Запуск SOCKS (bridge):"
echo "  docker compose up --build -d"
echo
echo "Запуск L3-шлюза (host network):"
echo "  # убедитесь GATEWAY_ENABLE=true в .env"
echo "  docker compose -f docker-compose.host.yml up --build -d"
echo
echo "Проверка:"
echo "  curl -s http://127.0.0.1:8080/readyz"
echo "  curl --socks5 127.0.0.1:1080 https://ifconfig.me"
