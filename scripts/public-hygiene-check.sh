#!/usr/bin/env bash
set -euo pipefail

ROOT="${1:-.}"

FORBIDDEN_INFRA_FACTS='37[.]27[.]235[.]104|95[.]217[.]198[.]239|77[.]42[.]89[.]211|89[.]167[.]39[.]247'
FORBIDDEN_PLACEHOLDERS='CHANGEME[^A-Za-z0-9_-]|REPLACE_WITH_[A-Z0-9_]+'
SELF_PATH="$ROOT/scripts/public-hygiene-check.sh"

exclude_paths=(
  -path "$ROOT/.git" -o
  -path "$ROOT/node_modules" -o
  -path "$ROOT/.next" -o
  -path "$ROOT/dist" -o
  -path "$ROOT/build" -o
  -path "$ROOT/coverage"
)

echo "Checking public repository hygiene..."

if find "$ROOT" \( "${exclude_paths[@]}" \) -prune -o -type f ! -path "$SELF_PATH" -print0 \
  | xargs -0 grep -nE "$FORBIDDEN_INFRA_FACTS"; then
  echo "ERROR: public repo contains hard-coded production infrastructure facts."
  echo "Move sensitive inventory to internal-devops and use placeholders here."
  exit 1
fi

if find "$ROOT" \( "${exclude_paths[@]}" \) -prune -o -type f ! -path "$SELF_PATH" -print0 \
  | xargs -0 grep -nE "$FORBIDDEN_PLACEHOLDERS"; then
  echo "ERROR: public repo contains unresolved secret placeholders."
  echo "Use explicit local-only example values or documented placeholder syntax."
  exit 1
fi

echo "Public hygiene check passed."
