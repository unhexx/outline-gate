#!/usr/bin/env bash
# Подготовка remote origin для git.aservice24.ru (не выполняет push без --push).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

REMOTE_NAME="${REMOTE_NAME:-origin}"
# Пример: git@git.aservice24.ru:infra/outline-gate.git
DEFAULT_URL="git@git.aservice24.ru:outline-gate/outline-gate.git"
REMOTE_URL="${1:-${GIT_REMOTE_URL:-$DEFAULT_URL}}"
DO_PUSH=false
for a in "$@"; do
  [[ "$a" == "--push" ]] && DO_PUSH=true
done
# if first arg is --push, url from env/default
if [[ "${1:-}" == "--push" ]]; then
  REMOTE_URL="${GIT_REMOTE_URL:-$DEFAULT_URL}"
fi

echo "Repository: $ROOT"
echo "Remote:     $REMOTE_NAME -> $REMOTE_URL"

if git remote get-url "$REMOTE_NAME" >/dev/null 2>&1; then
  current=$(git remote get-url "$REMOTE_NAME")
  if [[ "$current" != "$REMOTE_URL" ]]; then
    echo "Updating $REMOTE_NAME from $current"
    git remote set-url "$REMOTE_NAME" "$REMOTE_URL"
  else
    echo "Remote already set."
  fi
else
  git remote add "$REMOTE_NAME" "$REMOTE_URL"
  echo "Added remote $REMOTE_NAME"
fi

git remote -v
echo
echo "Branch: $(git branch --show-current 2>/dev/null || echo master)"
echo
echo "Next:"
echo "  # создайте пустой repo на git.aservice24.ru, затем:"
echo "  git push -u $REMOTE_NAME master"
echo
echo "Или: GIT_REMOTE_URL='git@git.aservice24.ru:GROUP/outline-gate.git' $0 --push"

if $DO_PUSH; then
  git push -u "$REMOTE_NAME" HEAD
fi
