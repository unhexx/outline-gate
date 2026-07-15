#!/usr/bin/env bash
# Optional privileged netns e2e harness (manual). Skips if no root / no key.
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "run as root for netns e2e" >&2
  exit 1
fi

if [[ -z "${OUTLINE_ACCESS_KEY:-}" ]]; then
  echo "OUTLINE_ACCESS_KEY required" >&2
  exit 1
fi

echo "e2e-netns: placeholder — start outline-gate in netns and curl via SOCKS"
echo "See docs/deployment.md for manual verification matrix."
