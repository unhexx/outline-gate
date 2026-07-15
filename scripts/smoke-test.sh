#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
KEY_FILE="${OUTLINE_ACCESS_KEY_FILE:-$ROOT/deploy/compose/secrets/outline_key.txt}"
SOCKS="${SMOKE_SOCKS:-127.0.0.1:1080}"
HEALTH="${SMOKE_HEALTH:-http://127.0.0.1:8080}"

if [[ ! -f "$KEY_FILE" && -z "${OUTLINE_ACCESS_KEY:-}" ]]; then
  echo "Provide OUTLINE_ACCESS_KEY or $KEY_FILE" >&2
  exit 1
fi

echo "==> healthz"
curl -fsS "$HEALTH/healthz" | grep -q ok

echo "==> readyz (may be not ready if tunnel down)"
code=$(curl -s -o /tmp/og-ready.json -w '%{http_code}' "$HEALTH/readyz" || true)
echo "readyz HTTP $code: $(cat /tmp/og-ready.json 2>/dev/null || true)"

if [[ "$code" == "200" ]]; then
  echo "==> SOCKS egress check"
  curl -fsS --max-time 30 --socks5 "$SOCKS" https://ifconfig.me
  echo
  echo "smoke OK"
else
  echo "tunnel not ready; process is up. Fix access key / network and re-run." >&2
  exit 2
fi
