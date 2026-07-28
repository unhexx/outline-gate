#!/usr/bin/env bash
# Интерактивная настройка .env, файла ключа и config/* для docker compose.
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$DIR"

ENV_FILE="$DIR/.env"
EXAMPLE="$DIR/.env.example"
SECRETS_DIR="$DIR/secrets"
KEY_FILE="$SECRETS_DIR/outline_key.txt"
CONFIG_DIR="$DIR/config"

mkdir -p "$SECRETS_DIR" "$CONFIG_DIR"

if [[ ! -f "$ENV_FILE" ]]; then
  if [[ -f "$EXAMPLE" ]]; then
    cp "$EXAMPLE" "$ENV_FILE"
    echo "Создан $ENV_FILE из .env.example"
  else
    touch "$ENV_FILE"
  fi
fi

# Runtime config files: create from examples only if missing (never overwrite local UI rules)
bootstrap_config() {
  local dest="$1" example="$2" header="${3:-}"
  if [[ -f "$dest" ]]; then
    return 0
  fi
  if [[ -f "$example" ]]; then
    cp "$example" "$dest"
    echo "Создан $dest из $(basename "$example")"
  elif [[ -n "$header" ]]; then
    printf '%s\n' "$header" > "$dest"
    echo "Создан $dest"
  else
    touch "$dest"
  fi
}
bootstrap_config "$CONFIG_DIR/bypass.rules.txt" "$CONFIG_DIR/bypass.rules.example.txt" \
  "# outline-gate user bypass rules (managed by UI/API)"
bootstrap_config "$CONFIG_DIR/bypass.txt" "$CONFIG_DIR/bypass.example.txt"
bootstrap_config "$CONFIG_DIR/tunnel.txt" "$CONFIG_DIR/tunnel.example.txt"

# --- helpers ---
set_env() {
  local key="$1" val="$2"
  if grep -qE "^${key}=" "$ENV_FILE" 2>/dev/null; then
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

gen_token() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24
  elif [[ -r /dev/urandom ]]; then
    head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n'
  else
    date +%s%N
  fi
}

echo "=== outline-gate: настройка ==="
echo

# Profile
PROFILE=$(prompt COMPOSE_PROFILE "Профиль: (1) socks — bridge SOCKS  (2) host — L3 host network" "1")
case "$PROFILE" in
  2|host|HOST|l3|L3)
    set_env COMPOSE_PROFILE "host"
    set_env GATEWAY_ENABLE "true"
    echo "Профиль: host (L3), GATEWAY_ENABLE=true"
    ;;
  *)
    set_env COMPOSE_PROFILE "socks"
    set_env GATEWAY_ENABLE "false"
    echo "Профиль: socks (bridge), GATEWAY_ENABLE=false"
    ;;
esac

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
    echo "$TC" | tr ',' '\n' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | grep -v '^$' > "$DIR/config/tunnel.txt" || true
  fi
fi

BC=$(prompt BYPASS_CIDRS "Доп. BYPASS_CIDRS (через запятую, Enter = пропуск)" "")
[[ -n "$BC" ]] && set_env BYPASS_CIDRS "$BC"

DP=$(prompt DIRECT_POLICY "DIRECT_POLICY (direct|drop)" "direct")
set_env DIRECT_POLICY "$DP"

# Web UI
UI_EN=$(prompt UI_ENABLE "UI_ENABLE (true|false) — Web UI bypass/ключ" "true")
set_env UI_ENABLE "$UI_EN"
if [[ "$UI_EN" == "true" ]]; then
  cur_tok=""
  if grep -qE "^UI_TOKEN=" "$ENV_FILE" 2>/dev/null; then
    cur_tok=$(grep -E "^UI_TOKEN=" "$ENV_FILE" | head -1 | cut -d= -f2-)
  fi
  if [[ -z "$cur_tok" || "$cur_tok" == "change-me-to-a-long-random-string" ]]; then
    DEF_TOK=$(gen_token)
  else
    DEF_TOK="$cur_tok"
  fi
  UT=$(prompt UI_TOKEN "UI_TOKEN (секрет для API/UI)" "$DEF_TOK")
  set_env UI_TOKEN "$UT"
fi

# Ports (bridge)
if grep -qE '^COMPOSE_PROFILE=host$' "$ENV_FILE" 2>/dev/null; then
  :
else
  HSP=$(prompt HOST_SOCKS_PORT "HOST_SOCKS_PORT (SOCKS на хосте)" "1080")
  set_env HOST_SOCKS_PORT "$HSP"
  HHP=$(prompt HOST_HEALTH_PORT "HOST_HEALTH_PORT (health+UI на хосте)" "28080")
  set_env HOST_HEALTH_PORT "$HHP"
fi

SAC=$(prompt SOCKS_ALLOW_CIDRS "SOCKS_ALLOW_CIDRS (LAN CSV, Enter = все)" "")
set_env SOCKS_ALLOW_CIDRS "$SAC"

LL=$(prompt LOG_LEVEL "LOG_LEVEL (debug|info|warn|error)" "info")
set_env LOG_LEVEL "$LL"

echo
echo "Готово. Файлы:"
echo "  $ENV_FILE"
echo "  $KEY_FILE (если использовали файл ключа)"
echo "  $CONFIG_DIR/bypass.rules.txt"
echo
echo "Запуск (рекомендуется install.sh):"
echo "  ./install.sh"
echo
echo "Или вручную:"
if grep -qE '^COMPOSE_PROFILE=host$' "$ENV_FILE" 2>/dev/null; then
  echo "  docker compose -f docker-compose.host.yml up --build -d"
else
  echo "  docker compose up --build -d"
fi
echo
echo "Проверка:"
if grep -qE '^COMPOSE_PROFILE=host$' "$ENV_FILE" 2>/dev/null; then
  echo "  curl -s http://127.0.0.1:8080/readyz"
  echo "  curl --socks5h 127.0.0.1:1080 https://ifconfig.me"
else
  hp=$(grep -E '^HOST_HEALTH_PORT=' "$ENV_FILE" | head -1 | cut -d= -f2-)
  sp=$(grep -E '^HOST_SOCKS_PORT=' "$ENV_FILE" | head -1 | cut -d= -f2-)
  hp=${hp:-28080}
  sp=${sp:-1080}
  echo "  curl -s http://127.0.0.1:${hp}/readyz"
  echo "  curl --socks5h 127.0.0.1:${sp} https://ifconfig.me"
  if grep -qE '^UI_ENABLE=true$' "$ENV_FILE" 2>/dev/null; then
    echo "  UI: http://127.0.0.1:${hp}/ui/"
  fi
fi
