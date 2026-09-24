#!/usr/bin/env bash
# Unit tests for the signer-identity logic in build-publish.yml's pin job
# ("Resolve trusted signer identity").
#
# The function library is NOT copied here: it is extracted verbatim from the
# workflow, between the `# >>> selfref-lib` and `# <<< selfref-lib` markers,
# so these tests cannot drift from what callers actually run. Network calls
# (api_get, list_release_tags, curl) are replaced with stubs.
#
# Also asserts the `on.workflow_call.secrets` contract: every `secrets.X` the
# workflow reads is declared, and nothing undeclared is read.
#
# Run: bash tests/scripts/test_build_publish_selfref.sh   (needs bash, jq, grep -E)
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WF="${ROOT}/.github/workflows/build-publish.yml"

lib=$(sed -n '/# >>> selfref-lib/,/# <<< selfref-lib/p' "$WF" | sed 's/^          //')
if [ -z "$lib" ] || ! printf '%s\n' "$lib" | grep -q 'resolve_identity_regexp()'; then
  echo "FAIL: could not extract selfref-lib from $WF" >&2
  exit 1
fi
# eval, not `source <(...)`: process-substitution sourcing is a no-op on
# bash 3.2 (macOS), which would silently define nothing.
eval "$lib"

fail=0
pass=0
ok()  { pass=$((pass + 1)); echo "  ok   $1"; }
bad() { fail=$((fail + 1)); echo "  FAIL $1"; }

SHA_MAIN=1111111111111111111111111111111111111111
SHA_TAG=2222222222222222222222222222222222222222
SHA_PR=3333333333333333333333333333333333333333
SHA_GONE=4444444444444444444444444444444444444444
SHA_API_DOWN=5555555555555555555555555555555555555555
SHA_DIVERGED=6666666666666666666666666666666666666666
SHA_IDENTICAL=7777777777777777777777777777777777777777
SHA_TAG_API_DOWN=8888888888888888888888888888888888888888
TAG_OBJECT=9999999999999999999999999999999999999999

# --- stubs ------------------------------------------------------------------
api_get() {
  case "$1" in
    "repos/madfam-org/enclii/compare/main...${SHA_MAIN}")      echo '{"status":"behind"}' ;;
    "repos/madfam-org/enclii/compare/main...${SHA_IDENTICAL}") echo '{"status":"identical"}' ;;
    "repos/madfam-org/enclii/compare/main...${SHA_TAG}")       echo '{"status":"ahead"}' ;;
    "repos/madfam-org/enclii/compare/main...${SHA_PR}")        echo '{"status":"ahead"}' ;;
    "repos/madfam-org/enclii/compare/main...${SHA_DIVERGED}")  echo '{"status":"diverged"}' ;;
    "repos/madfam-org/enclii/compare/main...${SHA_GONE}")      return 4 ;;
    *) return 5 ;;
  esac
}
list_release_tags() {
  # Annotated tag: the tag object SHA, then the peeled commit (`^{}`).
  printf '%s refs/tags/v1.0.0-alpha.11\n' "$TAG_OBJECT"
  printf '%s refs/tags/v1.0.0-alpha.11^{}\n' "$SHA_TAG"
  # Lightweight tag: the commit directly.
  printf '%s refs/tags/v0.9.0\n' "$SHA_TAG_API_DOWN"
}

SELF="madfam-org/enclii/.github/workflows/build-publish.yml"
SHA_ID_PREFIX='^https://github\.com/madfam-org/enclii/\.github/workflows/build-publish\.yml@'

expect_regexp() { # name jwr expected
  local got rc=0
  got=$(resolve_identity_regexp "$2" 2>/dev/null) || rc=$?
  if [ "$rc" -eq 0 ] && [ "$got" = "$3" ]; then ok "$1"; else bad "$1 (rc=$rc got=$got)"; fi
}
expect_refusal() { # name jwr stderr-substring
  local err rc=0
  err=$(resolve_identity_regexp "$2" 2>&1 >/dev/null) || rc=$?
  if [ "$rc" -ne 0 ] && printf '%s' "$err" | grep -qF -- "$3"; then ok "$1"; else bad "$1 (rc=$rc err=$err)"; fi
}

echo "resolve_identity_regexp"
expect_regexp "no claim -> legacy regexp" "" "$LEGACY_IDENTITY_REGEXP"
expect_regexp "@refs/heads/main -> legacy regexp (unchanged)" "${SELF}@refs/heads/main" "$LEGACY_IDENTITY_REGEXP"
expect_regexp "@refs/tags/v1.0.0-alpha.10 -> legacy regexp (unchanged)" "${SELF}@refs/tags/v1.0.0-alpha.10" "$LEGACY_IDENTITY_REGEXP"
expect_regexp "short sha is not a SHA pin -> legacy regexp" "${SELF}@0e24810" "$LEGACY_IDENTITY_REGEXP"
expect_regexp "uppercase hex is not a SHA pin -> legacy regexp" "${SELF}@ABCDEF1111111111111111111111111111111111" "$LEGACY_IDENTITY_REGEXP"
expect_regexp "SHA behind main -> legacy + that SHA" "${SELF}@${SHA_MAIN}" "${LEGACY_IDENTITY_REGEXP}|${SHA_ID_PREFIX}${SHA_MAIN}\$"
expect_regexp "SHA identical to main -> legacy + that SHA" "${SELF}@${SHA_IDENTICAL}" "${LEGACY_IDENTITY_REGEXP}|${SHA_ID_PREFIX}${SHA_IDENTICAL}\$"
expect_regexp "SHA off main but = annotated v* tag commit -> accepted" "${SELF}@${SHA_TAG}" "${LEGACY_IDENTITY_REGEXP}|${SHA_ID_PREFIX}${SHA_TAG}\$"
expect_regexp "API down but SHA = lightweight v* tag -> accepted" "${SELF}@${SHA_TAG_API_DOWN}" "${LEGACY_IDENTITY_REGEXP}|${SHA_ID_PREFIX}${SHA_TAG_API_DOWN}\$"
expect_refusal "unmerged PR head (ahead) -> refused" "${SELF}@${SHA_PR}" "NOT reachable from madfam-org/enclii main"
expect_refusal "diverged (fork) commit -> refused" "${SELF}@${SHA_DIVERGED}" "NOT reachable from madfam-org/enclii main"
expect_refusal "unknown SHA (404) -> refused" "${SELF}@${SHA_GONE}" "NOT reachable from madfam-org/enclii main"
expect_refusal "API down and not a tag -> refused, says why" "${SELF}@${SHA_API_DOWN}" "Could not determine"
expect_refusal "fork repo path with a SHA -> refused" "someone/enclii/.github/workflows/build-publish.yml@${SHA_MAIN}" "not from ${SELF}"
expect_refusal "other madfam-org workflow with a SHA -> refused" "madfam-org/enclii/.github/workflows/other.yml@${SHA_MAIN}" "not from ${SELF}"

echo "release_tag_for"
if [ "$(release_tag_for "$SHA_TAG")" = "v1.0.0-alpha.11" ]; then ok "annotated tag peeled to its name"; else bad "annotated tag name"; fi
if ! release_tag_for "$SHA_PR" >/dev/null; then ok "non-tag SHA -> non-zero"; else bad "non-tag SHA matched a tag"; fi

echo "identity regexp semantics (grep -E; the patterns use only ERE/RE2-common syntax)"
matches() { printf '%s\n' "$2" | grep -Eq -- "$1"; }
wid() { printf 'https://github.com/%s' "$1"; }
widened=$(resolve_identity_regexp "${SELF}@${SHA_MAIN}" 2>/dev/null)
if ! matches "$LEGACY_IDENTITY_REGEXP" "$(wid "${SELF}@${SHA_MAIN}")"; then ok "legacy regexp rejects a SHA identity (the original failure)"; else bad "legacy regexp unexpectedly accepts SHA identity"; fi
for id in "${SELF}@refs/heads/main" "${SELF}@refs/tags/v1.0.0-alpha.10" "madfam-org/nauta/.github/workflows/build-deploy.yml@refs/heads/main" "${SELF}@${SHA_MAIN}"; do
  if matches "$widened" "$(wid "$id")"; then ok "widened accepts $id"; else bad "widened rejects $id"; fi
done
for id in "${SELF}@${SHA_PR}" "madfam-org/other/.github/workflows/build-publish.yml@${SHA_MAIN}" "${SELF}@${SHA_MAIN}x" "evil/enclii/.github/workflows/build-publish.yml@${SHA_MAIN}" "${SELF}@refs/heads/feature"; do
  if ! matches "$widened" "$(wid "$id")"; then ok "widened rejects $id"; else bad "widened accepts $id"; fi
done

echo "fetch_job_workflow_ref / jwt_claims"
b64url() { printf '%s' "$1" | base64 | tr -d '=\n' | tr '/+' '_-'; }
# The claim value is chosen so its base64 carries '/', '+' and needs padding.
claims='{"job_workflow_ref":"madfam-org/enclii/.github/workflows/build-publish.yml@1111111111111111111111111111111111111111","x":"???>>>"}'
TEST_JWT="$(b64url '{"alg":"RS256"}').$(b64url "$claims").sig"
curl() { printf '{"value":"%s"}' "$TEST_JWT"; }  # not $jwt: the lib's `local jwt` would shadow it
got=$(ACTIONS_ID_TOKEN_REQUEST_URL='https://example.invalid/token?x=1' ACTIONS_ID_TOKEN_REQUEST_TOKEN=t fetch_job_workflow_ref)
if [ "$got" = "${SELF}@${SHA_MAIN}" ]; then ok "claim read from a base64url JWT"; else bad "claim decode got '$got'"; fi
if ! (unset ACTIONS_ID_TOKEN_REQUEST_URL; fetch_job_workflow_ref >/dev/null); then ok "no OIDC env -> non-zero (caller falls back to legacy regexp)"; else bad "no OIDC env returned success"; fi
unset -f curl

echo "workflow_call.secrets contract"
used=$(grep -o 'secrets\.[A-Z_][A-Z0-9_]*' "$WF" | sed 's/^secrets\.//' | sort -u)
declared=$(awk '
  /^    secrets:$/ { in_s = 1; next }
  in_s && /^      [A-Z_][A-Z0-9_]*:$/ { sub(/^ +/, ""); sub(/:$/, ""); print; next }
  in_s && /^    [^ ]/ { in_s = 0 }
  in_s && /^[^ ]/ { in_s = 0 }
' "$WF" | sort -u)
if [ -n "$declared" ] && [ "$used" = "$declared" ]; then
  ok "declared secrets == secrets read ($(echo "$declared" | tr '\n' ' '))"
else
  bad "secrets mismatch. read: $(echo "$used" | tr '\n' ' ')| declared: $(echo "$declared" | tr '\n' ' ')"
fi
if ! awk '/^    secrets:$/{s=1;next} s&&/^    [^ ]/{s=0} s&&/^[^ ]/{s=0} s' "$WF" | grep -q 'required: true'; then
  ok "every declared secret is optional (required: false)"
else
  bad "a declared secret is required: true (would break callers)"
fi

echo
echo "passed=${pass} failed=${fail}"
[ "$fail" -eq 0 ]
