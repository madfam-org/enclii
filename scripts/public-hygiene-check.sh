#!/usr/bin/env bash
set -euo pipefail

# Public-repo hygiene guard for enclii.
#
# Scans every tracked TEXT file (not just docs) for material that must not live
# in a public repository: unresolved secret placeholders, public IPv4 literals,
# Cloudflare tunnel UUIDs, hardware SKUs that identify the estate's topology,
# and — through a private pattern file — production node hostnames.
#
# WHY THE NODE HOSTNAMES ARE NOT IN THIS FILE
# ===========================================
# They are the exact strings that must not appear in this repo; shipping them
# here would publish the answer key. Hashing them was considered and rejected:
# `<prefix>-<role>-NN` is a dozen guesses and IPv4 is 2^32, so a hash buys
# obfuscation while implying secrecy, and a control that implies more
# protection than it gives is worse than none.
#
# So the needles come from a private file, read through
#   ${MADFAM_HYGIENE_PATTERNS:-../internal-devops/security/public-hygiene-private-patterns.txt}
# one extended regular expression per line, `#` comments and blank lines
# ignored. When that file is not readable this script prints
#   node-identity class SKIPPED — private pattern file not available
# and emits `classes_skipped=1`. That is the point of the arrangement: a green
# public run must never imply the class was checked when the needles were not
# available to check it with. The enforcing half is
# `internal-devops/scripts/check-public-repo-node-identity.py`, which reads
# node identity from `internal-devops/infrastructure/nodes.md` directly.
#
# Policy: docs/PUBLIC_REPO_BOUNDARY.md and the repo-boundary contract in
# madfam-org/internal-devops.
#
# EXIT CODES
#   0  scanned, clean
#   1  a forbidden pattern matched
#   2  UNDETERMINED — the tracked file set could not be established. Fails CI
#      exactly like exit 1: a scan that read nothing proves nothing.

ROOT="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
cd "$ROOT"

SELF='scripts/public-hygiene-check.sh'
status=0
classes_skipped=0

# Every tracked text file. `grep -I` drops binaries. The old scan was a `find`
# over *.md/*.mdx/*.txt/README*, which is why .yml, .sh, .ts and .json were
# never read at all.
scan_files() {
  git ls-files -z -- ":(exclude)${SELF}" |
    xargs -0 -r grep -IlZ '' 2>/dev/null | tr '\0' '\n'
}

FILES="$(scan_files || true)"
if [[ -z "$FILES" ]]; then
  echo '[public-hygiene] UNDETERMINED — could not establish the tracked file set' >&2
  echo 'public_hygiene=UNDETERMINED files=0 classes_skipped=0' >&2
  exit 2
fi
FILE_COUNT="$(printf '%s\n' "$FILES" | wc -l | tr -d ' ')"

# Placeholder forms that are deliberately non-secret. A match on one of these
# is an example, not a leak.
PLACEHOLDER='\$\{|YOUR_|REDACTED|CHANGEME|PLACEHOLDER|<[A-Z_]+>|%s|EXAMPLE'

check_pattern() {
  local label="$1" pattern="$2" filter="${3:-}"
  local matches
  matches=$(printf '%s\n' "$FILES" | xargs -r grep -nE -e "$pattern" 2>/dev/null || true)
  if [[ -n "$filter" && -n "$matches" ]]; then
    matches=$(printf '%s\n' "$matches" | grep -vE "$filter" || true)
  fi
  if [[ -n "$matches" ]]; then
    printf '\n[public-hygiene] %s\n' "$label" >&2
    printf '%s\n' "$matches" >&2
    status=1
  fi
}

check_pattern 'Stripe live/test secret key pattern' 'sk_(live|test)_[A-Za-z0-9_]{16,}'
check_pattern 'GitHub token pattern' 'gh[pousr]_[A-Za-z0-9_]{20,}'
check_pattern 'AWS access key pattern' 'AKIA[0-9A-Z]{16}'
check_pattern 'Private key marker' '-----BEGIN [A-Z ]*PRIVATE KEY-----'
check_pattern 'npm registry auth with a concrete value' ':_auth(Token)?=[A-Za-z0-9+/=_.-]{16,}' "$PLACEHOLDER"
check_pattern 'Unresolved secret placeholder' 'CHANGEME[^A-Za-z0-9_-]|REPLACE_WITH_[A-Z0-9_]+'
# NOT CHECKED HERE: Cloudflare tunnel identifiers (UUID shape). Measured
# 2026-09-04 over this tree, a bare UUID pattern is dominated by RFC-4122
# example ids in CLI docs and by test fixtures, and narrowing it to
# tunnel-context lines returns live findings that belong to the tunnel-identifier
# lane (2026-07-16 exposure class 2), not this one. Merging a guard that turns
# public CI red while pointing at a live identifier helps nobody, so the gap is
# STATED in docs/PUBLIC_REPO_BOUNDARY.md rather than silently assumed covered.

# Hardware class strings. On their own these identify the estate's topology
# (2026-07-16 exposure class 1): public docs get a CLASS ("dedicated
# bare-metal", "cloud compute instance"), never a provider SKU.
check_pattern 'Server hardware SKU (use a hardware class instead)' '\b(EX44|AX41|CCX[0-9]*)\b'

# Public IPv4 literals, over the OPS file set only (docs, manifests, workflows,
# scripts, env samples — not application source or tests). Measured 2026-09-04:
# over the whole tree a dotted-quad pattern is dominated by SVG path data,
# semver-like strings and mock fixtures; over the ops set it returns 0. Each
# octet is range-checked, and private, loopback, link-local, TEST-NET,
# broadcast/unspecified ranges plus the well-known public resolvers documented
# in networking guides are excluded — none of those are node identity.
OPS_FILES="$(git ls-files -z -- '*.md' '*.mdx' '*.txt' '*.yml' '*.yaml' '*.sh' \
    '*.tf' '*.conf' '*.env*' '.npmrc' '*/.npmrc' |
  xargs -0 -r grep -IlZ '' 2>/dev/null | tr '\0' '\n' |
  grep -vE '(^|/)(tests?|__tests__|e2e)/' || true)"
OCTET='(25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])'
NON_PUBLIC_IP='^(10\.|127\.|169\.254\.|192\.168\.|172\.(1[6-9]|2[0-9]|3[01])\.|192\.0\.2\.|198\.51\.100\.|203\.0\.113\.|0\.0\.0\.0|255\.|1\.1\.1\.1$|1\.0\.0\.1$|8\.8\.8\.8$|8\.8\.4\.4$|9\.9\.9\.9$)'
ip_matches=$(printf '%s\n' "$OPS_FILES" |
  xargs -r grep -nEo "\\b($OCTET\\.){3}$OCTET\\b" 2>/dev/null |
  awk -F: -v re="$NON_PUBLIC_IP" '{ ip=$NF; if (ip !~ re) print }' || true)
if [[ -n "$ip_matches" ]]; then
  printf '\n[public-hygiene] Public IPv4 literal\n' >&2
  printf '%s\n' "$ip_matches" >&2
  status=1
fi

# --- node identity class (private needles) ----------------------------------
PATTERN_FILE="${MADFAM_HYGIENE_PATTERNS:-../internal-devops/security/public-hygiene-private-patterns.txt}"
if [[ -r "$PATTERN_FILE" ]]; then
  while IFS= read -r pattern; do
    [[ -z "$pattern" || "$pattern" == \#* ]] && continue
    hits=$(printf '%s\n' "$FILES" | xargs -r grep -nE -e "$pattern" 2>/dev/null || true)
    if [[ -n "$hits" ]]; then
      # The pattern itself is private; report the class and the file:line only.
      printf '\n[public-hygiene] node identity (private pattern class)\n' >&2
      printf '%s\n' "$hits" | cut -d: -f1,2 >&2
      status=1
    fi
  done < "$PATTERN_FILE"
else
  classes_skipped=1
  echo 'node-identity class SKIPPED — private pattern file not available' >&2
  echo "  set MADFAM_HYGIENE_PATTERNS to internal-devops/security/public-hygiene-private-patterns.txt" >&2
fi

echo "public_hygiene=$([[ $status -eq 0 ]] && echo OK || echo FAIL) files=${FILE_COUNT} classes_skipped=${classes_skipped}"

if [[ "$status" -ne 0 ]]; then
  cat >&2 <<'MSG'

Public hygiene check failed. Rotate first if any value may have been live, then
replace the public reference with a non-secret placeholder or a ROLE, or move
the detail to internal-devops. Deleting the line from HEAD does not remove it
from git history — see docs/PUBLIC_REPO_BOUNDARY.md.
MSG
fi

exit "$status"
