#!/bin/sh
set -e

# A previous process may have left outline_gate rules in this netns or, when
# /host/netns is mounted, in the host netns. Container restart does not drop them.
if [ -e /host/netns ] && command -v nsenter >/dev/null 2>&1 && command -v nft >/dev/null 2>&1; then
  printf 'delete table inet outline_gate\n' | nsenter --net=/host/netns nft -f - >/dev/null 2>&1 || true
fi
if command -v nft >/dev/null 2>&1; then
  printf 'delete table inet outline_gate\n' | nft -f - >/dev/null 2>&1 || true
fi

# Enable IPv4 forwarding when possible (host network / privileged).
if [ -w /proc/sys/net/ipv4/ip_forward ] 2>/dev/null; then
  # ignore errors (read-only sysctl in many containers)
  sh -c 'echo 1 > /proc/sys/net/ipv4/ip_forward' 2>/dev/null || true
fi

# Resolve access key: env wins; else non-empty key file.
if [ -z "${OUTLINE_ACCESS_KEY:-}" ]; then
  if [ -n "${OUTLINE_ACCESS_KEY_FILE:-}" ] && [ -f "$OUTLINE_ACCESS_KEY_FILE" ]; then
    # strip comments/blank — first non-empty non-# line
    KEY_LINE=$(grep -v '^[[:space:]]*#' "$OUTLINE_ACCESS_KEY_FILE" | grep -v '^[[:space:]]*$' | head -1 || true)
    if [ -n "$KEY_LINE" ]; then
      export OUTLINE_ACCESS_KEY="$KEY_LINE"
      # clear file path so config uses env (avoids empty/comment-only confusion)
      unset OUTLINE_ACCESS_KEY_FILE
    fi
  fi
fi

if [ -z "${OUTLINE_ACCESS_KEY:-}" ]; then
  echo "outline-gate: missing access key." >&2
  echo "Set OUTLINE_ACCESS_KEY or provide a non-empty OUTLINE_ACCESS_KEY_FILE (ss://... or ssconf://...)." >&2
  echo "Example: docker compose + deploy/compose/configure.sh" >&2
  exit 1
fi

case "$OUTLINE_ACCESS_KEY" in
  ss://*|ssconf://*) ;;
  *)
    echo "outline-gate: key must start with ss:// or ssconf://" >&2
    exit 1
    ;;
esac

exec "$@"
