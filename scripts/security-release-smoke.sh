#!/usr/bin/env bash
# Automatable SECURITY_RELEASE_PR checks (O-3 Gate 1).
#
# Verifies production fail-closed auth without exposing secrets. Optional
# ENCLII_ROUNDHOUSE_SMOKE_KEY proves the shared secret is accepted (returns
# 400 missing body/param, not 401).
#
# Usage:
#   ./scripts/security-release-smoke.sh
#   API_URL=https://api.enclii.dev ./scripts/security-release-smoke.sh
#   ENCLII_ROUNDHOUSE_SMOKE_KEY=... ./scripts/security-release-smoke.sh

set -euo pipefail

API_URL="${API_URL:-https://api.enclii.dev}"
TIMEOUT="${TIMEOUT:-10}"
SUMMARY_PATH="${SUMMARY_PATH:-security-release-smoke.json}"

PASS=0
FAIL=0
WARN=0
CHECKS_FILE="$(mktemp)"
BODY_FILE="$(mktemp)"

cleanup() {
  rm -f "$CHECKS_FILE" "$BODY_FILE"
}
trap cleanup EXIT

append_check() {
  local status="$1"
  local name="$2"
  local detail="${3:-}"
  case "$status" in
    pass) PASS=$((PASS + 1)) ;;
    warn) WARN=$((WARN + 1)) ;;
    *) FAIL=$((FAIL + 1)); status="fail" ;;
  esac
  python3 - "$CHECKS_FILE" "$status" "$name" "$detail" <<'PY'
import json, sys
path, status, name, detail = sys.argv[1:]
with open(path, "a", encoding="utf-8") as f:
    f.write(json.dumps({"status": status, "name": name, "detail": detail}) + "\n")
PY
  printf "[%s] %s" "$status" "$name"
  [ -n "$detail" ] && printf " — %s" "$detail"
  printf "\n"
}

http_code() {
  local method="$1"
  local url="$2"
  local auth="${3:-}"
  if [ -n "$auth" ]; then
    curl -sS -L --max-time "$TIMEOUT" -o "$BODY_FILE" -w "%{http_code}" \
      -X "$method" -H "Authorization: Bearer ${auth}" "$url" || true
  else
    curl -sS -L --max-time "$TIMEOUT" -o "$BODY_FILE" -w "%{http_code}" \
      -X "$method" "$url" || true
  fi
}

write_summary() {
  python3 - "$SUMMARY_PATH" "$CHECKS_FILE" "$PASS" "$FAIL" "$WARN" "$API_URL" <<'PY'
import json, sys
from datetime import datetime, timezone
summary_path, checks_path, passed, failed, warned, api_url = sys.argv[1:]
checks = []
with open(checks_path, "r", encoding="utf-8") as f:
    for line in f:
        if line.strip():
            checks.append(json.loads(line))
payload = {
    "checked_at": datetime.now(timezone.utc).isoformat(),
    "api_url": api_url,
    "passed": int(passed),
    "failed": int(failed),
    "warnings": int(warned),
    "checks": checks,
}
with open(summary_path, "w", encoding="utf-8") as f:
    json.dump(payload, f, indent=2)
    f.write("\n")
PY
}

echo "=== Security release smoke (O-3 automatable) ==="
echo "API_URL=$API_URL"

code="$(http_code GET "${API_URL%/}/v1/dashboard/stats")"
if [ "$code" = "401" ] || [ "$code" = "403" ]; then
  append_check pass "dashboard stats auth gate" "HTTP $code unauthenticated"
else
  append_check fail "dashboard stats auth gate" "expected 401/403, got ${code:-000}"
fi

code="$(http_code POST "${API_URL%/}/v1/callbacks/build-complete")"
if [ "$code" = "401" ]; then
  append_check pass "build callback rejects missing bearer" "HTTP 401"
elif [ "$code" = "503" ]; then
  append_check fail "build callback rejects missing bearer" "HTTP 503 — ENCLII_ROUNDHOUSE_API_KEY not configured on API"
else
  append_check fail "build callback rejects missing bearer" "expected 401, got ${code:-000} (400 implies auth bypass or stale deploy)"
fi

code="$(http_code POST "${API_URL%/}/v1/callbacks/build-complete" "invalid-smoke-key")"
if [ "$code" = "401" ]; then
  append_check pass "build callback rejects invalid bearer" "HTTP 401"
else
  append_check fail "build callback rejects invalid bearer" "expected 401, got ${code:-000}"
fi

code="$(http_code GET "${API_URL%/}/v1/services?git_repo=https://github.com/madfam-org/enclii")"
if [ "$code" = "401" ]; then
  append_check pass "git_repo lookup rejects missing bearer" "HTTP 401"
elif [ "$code" = "200" ]; then
  append_check fail "git_repo lookup rejects missing bearer" "HTTP 200 — internal read is public; verify ENCLII_ROUNDHOUSE_API_KEY + deploy SHA"
else
  append_check fail "git_repo lookup rejects missing bearer" "expected 401, got ${code:-000}"
fi

if [ -n "${ENCLII_ROUNDHOUSE_SMOKE_KEY:-}" ]; then
  code="$(http_code GET "${API_URL%/}/v1/services?git_repo=https://github.com/madfam-org/enclii" "$ENCLII_ROUNDHOUSE_SMOKE_KEY")"
  if [ "$code" = "200" ] || [ "$code" = "400" ]; then
    append_check pass "roundhouse bearer accepted" "HTTP $code (key configured and accepted)"
  elif [ "$code" = "401" ]; then
    append_check fail "roundhouse bearer accepted" "HTTP 401 — key mismatch between Roundhouse and API"
  else
    append_check warn "roundhouse bearer accepted" "unexpected HTTP ${code:-000}"
  fi
else
  append_check warn "roundhouse bearer accepted" "set ENCLII_ROUNDHOUSE_SMOKE_KEY to verify shared secret"
fi

if [ -n "${ENCLII_API_TOKEN:-}" ] && command -v enclii >/dev/null 2>&1; then
  export ENCLII_API_ENDPOINT="${ENCLII_API_ENDPOINT:-$API_URL}"
  export ENCLII_API_TOKEN
  if enclii db schema --json 2>/dev/null | python3 -c "import json,sys; p=json.load(sys.stdin); sys.exit(0 if p.get('healthy') else 1)"; then
    append_check pass "db schema healthy" "migration 030 verify OK"
  else
    append_check fail "db schema healthy" "enclii db schema unhealthy or unreachable"
  fi
else
  append_check warn "db schema healthy" "set ENCLII_API_TOKEN for admin schema verify (O-2)"
fi

append_check warn "tenant isolation manual" "non-admin cron/junction IDOR smoke still required (SECURITY_RELEASE_PR step 3)"

write_summary
echo "Summary written to $SUMMARY_PATH"
echo "passed=$PASS failed=$FAIL warnings=$WARN"
echo
echo "Manual sign-off still required: SECURITY_RELEASE_PR steps 2–3 (Roundhouse client config + tenant smoke)."

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
