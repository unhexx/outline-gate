#!/usr/bin/env bash
# Точка входа из корня репозитория.
#   ./install.sh 'ss://...'           # 1 шаг после clone
#   ./install.sh --host 'ss://...'    # L3 / host network
#   ./install.sh --configure          # интерактив
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
exec "$ROOT/deploy/compose/install.sh" "$@"
