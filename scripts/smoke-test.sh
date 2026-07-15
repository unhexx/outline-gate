#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
KEY_FILE="${OUTLINE_ACCESS_KEY_FILE:-$ROOT/deploy/compose/secrets/outline_key.txt}"
SOCKS="${SMOKE_SOCKS:-127.0.0.1:1080}"
HEALTH="${SMOKE_HEALTH:-http://127.0.0.1:8080}"
# Targets tried in order (some egress endpoints flap under load).
SMOKE_URLS="${SMOKE_URLS:-https://ifconfig.me https://icanhazip.com https://api.ipify.org}"
SMOKE_RETRIES="${SMOKE_RETRIES:-4}"

if [[ ! -f "$KEY_FILE" && -z "${OUTLINE_ACCESS_KEY:-}" ]]; then
  echo "Provide OUTLINE_ACCESS_KEY or $KEY_FILE" >&2
  exit 1
fi

echo "==> healthz"
curl -fsS "$HEALTH/healthz" | grep -q ok

echo "==> readyz (may be not ready if tunnel down)"
code=$(curl -s -o /tmp/og-ready.json -w '%{http_code}' "$HEALTH/readyz" || true)
echo "readyz HTTP $code: $(cat /tmp/og-ready.json 2>/dev/null || true)"

if [[ "$code" != "200" ]]; then
  echo "tunnel not ready; process is up. Fix access key / network and re-run." >&2
  exit 2
fi

echo "==> SOCKS egress check"
# Prefer hostname via SOCKS (remote DNS) and force IPv4: v1 is IPv4-only;
# plain --socks5 may try AAAA first and fail on IPv6-less paths.
ok=0
for attempt in $(seq 1 "$SMOKE_RETRIES"); do
  for url in $SMOKE_URLS; do
    if out=$(curl -4 -fsS --max-time 30 --socks5-hostname "$SOCKS" "$url" 2>/dev/null); then
      if [[ -n "${out// }" ]]; then
        printf '%s\n' "$out"
        ok=1
        break 2
      fi
    fi
  done
  echo "  attempt $attempt/$SMOKE_RETRIES failed, retry..." >&2
  sleep 2
done

if [[ "$ok" -ne 1 ]]; then
  echo "SOCKS egress failed after $SMOKE_RETRIES attempts" >&2
  exit 3
fi

echo "smoke OK"
