#!/usr/bin/env bash
# Точка входа из корня репозитория.
#   bash install.sh 'ss://...'        # работает без +x и на noexec-разделах
#   ./install.sh 'ss://...'
#   ./install.sh --host 'ss://...'
#   ./install.sh --configure
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
TARGET="$ROOT/deploy/compose/install.sh"
if [[ ! -f "$TARGET" ]]; then
  echo "Не найден $TARGET — запускайте из корня репозитория outline-gate" >&2
  exit 1
fi
if [[ ! -r "$TARGET" ]]; then
  echo "Нет права на чтение: $TARGET (chown/chmod u+r)" >&2
  exit 1
fi
# Всегда через bash: не требует +x на скрипте и не ломается на noexec mount.
exec bash "$TARGET" "$@"
