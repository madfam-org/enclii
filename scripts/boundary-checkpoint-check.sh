#!/usr/bin/env bash
set -euo pipefail

ROOT="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
cd "$ROOT"

changed_files() {
  if [[ -n "${BOUNDARY_CHECK_FILES:-}" ]]; then
    printf '%s\n' ${BOUNDARY_CHECK_FILES}
    return
  fi

  if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    return
  fi

  local base="${BOUNDARY_BASE_REF:-${GITHUB_BASE_REF:-main}}"
  local branch
  branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
  if [[ "$branch" == "$base" ]] && git rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    git diff --name-only --diff-filter=ACMR HEAD~1..HEAD
    return
  fi

  if git rev-parse --verify "origin/${base}" >/dev/null 2>&1; then
    git diff --name-only --diff-filter=ACMR "origin/${base}...HEAD"
    return
  fi

  if git rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    git diff --name-only --diff-filter=ACMR HEAD~1..HEAD
    return
  fi

  git ls-files
}

is_boundary_surface() {
  case "$1" in
    README.md|ROADMAP.md|AI_CONTEXT.md|AGENTS.md|ECOSYSTEM.md|CHANGELOG.md|CONTRIBUTING.md|SECURITY.md)
      return 0
      ;;
    docs/PUBLIC_REPO_BOUNDARY.md|docs/ECOSYSTEM_STATUS.md|docs/INFRASTRUCTURE_STATUS.md)
      return 0
      ;;
    docs/*ROADMAP*.md|docs/*roadmap*.md|docs/*STATUS*.md|docs/*status*.md|docs/production/*.md|docs/runbooks/*.md)
      return 0
      ;;
  esac
  return 1
}

has_boundary_marker() {
  grep -Eiq 'boundary checkpoint|repository boundary|public repository boundary|repo-boundary contract|PUBLIC_REPO_BOUNDARY|repo-boundary-contract' "$1"
}

status=0
checked=0

while IFS= read -r file; do
  [[ -n "$file" ]] || continue
  [[ -f "$file" ]] || continue
  is_boundary_surface "$file" || continue
  checked=$((checked + 1))

  if ! has_boundary_marker "$file"; then
    printf '[boundary-checkpoint] missing checkpoint marker: %s\n' "$file" >&2
    status=1
  fi
done < <(changed_files)

if [[ "$status" -ne 0 ]]; then
  cat >&2 <<'MSG'

Boundary checkpoint check failed.
Add a short boundary checkpoint to each changed public-facing roadmap/status/doc surface:
- date and owner
- public-safe summary
- private sink for omitted operational detail
- policy pointer to docs/PUBLIC_REPO_BOUNDARY.md or repo-boundary-contract
MSG
fi

printf 'Boundary checkpoint check inspected %s high-risk doc surface(s).\n' "$checked"
exit "$status"
