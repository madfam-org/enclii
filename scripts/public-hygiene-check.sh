#!/usr/bin/env bash
set -euo pipefail

ROOT="${1:-.}"

FORBIDDEN_INFRA_FACTS='37[.]27[.]235[.]104|95[.]217[.]198[.]239|77[.]42[.]89[.]211|89[.]167[.]39[.]247'
FORBIDDEN_PLACEHOLDERS='CHANGEME[^A-Za-z0-9_-]|REPLACE_WITH_[A-Z0-9_]+'
SELF_PATH="$ROOT/scripts/public-hygiene-check.sh"

run_repo_grep() {
  local pattern="$1"

  if git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    (
      cd "$ROOT"
      git ls-files -z -- ':(exclude)scripts/public-hygiene-check.sh' |
        xargs -0 grep -nE "$pattern"
    )
    return
  fi

  local exclude_paths=(
    -path "$ROOT/.git" -o
    -path "*/node_modules" -o
    -path "*/.next" -o
    -path "*/dist" -o
    -path "*/build" -o
    -path "*/coverage"
  )

  find "$ROOT" \( "${exclude_paths[@]}" \) -prune -o -type f ! -path "$SELF_PATH" -print0 |
    xargs -0 grep -nE "$pattern"
}

echo "Checking public repository hygiene..."

if run_repo_grep "$FORBIDDEN_INFRA_FACTS"; then
  echo "ERROR: public repo contains hard-coded production infrastructure facts."
  echo "Move sensitive inventory to internal-devops and use placeholders here."
  exit 1
fi

if run_repo_grep "$FORBIDDEN_PLACEHOLDERS"; then
  echo "ERROR: public repo contains unresolved secret placeholders."
  echo "Use explicit local-only example values or documented placeholder syntax."
  exit 1
fi

echo "Public hygiene check passed."
