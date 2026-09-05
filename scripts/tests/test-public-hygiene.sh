#!/usr/bin/env bash
# Cases for scripts/public-hygiene-check.sh. Each case is a throwaway git repo
# so the guard sees a real `git ls-files` set, which is what it scans.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GUARD="$(cd "$HERE/.." && pwd)/public-hygiene-check.sh"
TMPROOT="$(mktemp -d)"
trap 'rm -rf "$TMPROOT"' EXIT

fails=0
run=0

# new_repo <name> -> prints the repo path; caller writes files into it
new_repo() {
  local dir="$TMPROOT/$1"
  mkdir -p "$dir/scripts"
  cp "$GUARD" "$dir/scripts/public-hygiene-check.sh"
  git -C "$dir" init -q
  git -C "$dir" config user.email t@example.com
  git -C "$dir" config user.name t
  printf '%s\n' "$dir"
}

# expect <name> <expected-exit> <expected-skipped> [pattern-file]
expect() {
  local name="$1" want="$2" want_skipped="$3" patterns="${4:-/nonexistent/patterns}"
  local dir="$TMPROOT/$name" out code
  git -C "$dir" add -A >/dev/null 2>&1
  out=$(MADFAM_HYGIENE_PATTERNS="$patterns" bash "$GUARD" "$dir" 2>&1)
  code=$?
  run=$((run + 1))
  local skipped=0
  [[ "$out" == *"classes_skipped=1"* ]] && skipped=1
  if [[ "$code" != "$want" || "$skipped" != "$want_skipped" ]]; then
    printf 'FAIL %-28s exit=%s (want %s) classes_skipped=%s (want %s)\n' \
      "$name" "$code" "$want" "$skipped" "$want_skipped"
    printf '%s\n' "$out" | sed 's/^/     | /'
    fails=$((fails + 1))
  else
    printf 'ok   %-28s exit=%s classes_skipped=%s\n' "$name" "$code" "$skipped"
  fi
}

# 1. clean tree, no private pattern file -> pass, but the class is SKIPPED
d=$(new_repo clean); echo '# hello' > "$d/README.md"
expect clean 0 1

# 2. placeholder forms are not findings
d=$(new_repo placeholders)
{ echo 'token: ${NPM_TOKEN}'; echo 'auth: YOUR_TOKEN'; echo 'k: <REDACTED>'; echo 'fmt: %s'; } > "$d/notes.md"
expect placeholders 0 1

# 3. private ranges and documented public resolvers are not node identity
d=$(new_repo private-ip)
{ echo '10.0.0.1'; echo '172.18.0.5'; echo '127.0.0.1'; echo '1.1.1.1'; echo '8.8.8.8'; } > "$d/net.md"
expect private-ip 0 1

# 4. a public IPv4 literal in an ops file IS a finding
d=$(new_repo public-ip); echo 'endpoint: 198.18.7.9' > "$d/infra.yaml"
expect public-ip 1 1

# 5. IPv4-shaped strings outside the ops file set are not scanned for this class
d=$(new_repo ip-in-source); echo 'const p = "198.18.7.9"' > "$d/app.ts"
expect ip-in-source 0 1

# 6. a hardware SKU is a finding wherever it appears
d=$(new_repo hardware-sku); echo 'ordered an EX44 for the cluster' > "$d/CAPACITY.md"
expect hardware-sku 1 1

# 7. an npm registry auth value with a concrete secret is a finding
d=$(new_repo npm-auth); echo '//npm.example.com/:_auth=YWJjZGVmZ2hpamtsbW5vcA==' > "$d/.npmrc"
expect npm-auth 1 1

# 8. a private pattern file that IS readable checks the class (skipped=0)
printf '# comment\n\\bnode-zz-01\\b\n' > "$TMPROOT/patterns.txt"
d=$(new_repo private-clean); echo 'the control-plane node' > "$d/README.md"
expect private-clean 0 0 "$TMPROOT/patterns.txt"

# 9. ... and fails when a needle matches
d=$(new_repo private-hit); echo 'ssh node-zz-01' > "$d/README.md"
expect private-hit 1 0 "$TMPROOT/patterns.txt"

# 10. an empty tracked file set is UNDETERMINED, not clean
d=$(new_repo empty); rm -f "$d/scripts/public-hygiene-check.sh"
expect empty 2 0

printf '\npublic-hygiene tests: %s run, %s failed\n' "$run" "$fails"
exit $(( fails > 0 ? 1 : 0 ))
