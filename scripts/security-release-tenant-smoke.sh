#!/usr/bin/env bash
# SECURITY_RELEASE_PR step 3 — tenant isolation smoke (O-3 manual).
#
# Verifies a non-admin bearer cannot read another tenant's junction (404 NOT_FOUND).
# Cron-job cross-tenant check is optional when ENCLII_CROSS_TENANT_CRON_ID is set.
#
# Usage:
#   export ENCLII_USER_TOKEN=...          # non-admin Janua/Enclii API token
#   export ENCLII_CROSS_TENANT_JUNCTION_ID=092a8580-2948-4264-b9aa-7f27e70813d4
#   ./scripts/security-release-tenant-smoke.sh
#
# Optional:
#   ENCLII_CROSS_TENANT_CRON_ID=...       # UUID in another project (if any exist)
#   API_URL=https://api.enclii.dev

set -euo pipefail

API_URL="${API_URL:-https://api.enclii.dev}"
TIMEOUT="${TIMEOUT:-15}"
JUNCTION_ID="${ENCLII_CROSS_TENANT_JUNCTION_ID:-092a8580-2948-4264-b9aa-7f27e70813d4}"
CRON_ID="${ENCLII_CROSS_TENANT_CRON_ID:-}"

PASS=0
FAIL=0
WARN=0

append() {
  local status="$1" name="$2" detail="${3:-}"
  case "$status" in
    pass) PASS=$((PASS + 1)) ;;
    warn) WARN=$((WARN + 1)) ;;
    *) FAIL=$((FAIL + 1)); status="fail" ;;
  esac
  printf '[%s] %s' "$status" "$name"
  [ -n "$detail" ] && printf ' — %s' "$detail"
  printf '\n'
}

auth_code() {
  local method="$1" url="$2"
  curl -sS -L --max-time "$TIMEOUT" -o /tmp/tenant-smoke-body.json -w "%{http_code}" \
    -X "$method" -H "Authorization: Bearer ${ENCLII_USER_TOKEN}" "$url" || true
}

json_code() {
  python3 - <<'PY' /tmp/tenant-smoke-body.json 2>/dev/null || echo ""
import json, sys
try:
    with open(sys.argv[1], encoding="utf-8") as f:
        body = json.load(f)
    err = body.get("error") or {}
    if isinstance(err, dict):
        print(err.get("code", ""))
    else:
        print("")
except Exception:
    print("")
PY
}

echo "=== Security release tenant smoke (O-3 step 3) ==="
echo "API_URL=$API_URL"

if [ -z "${ENCLII_USER_TOKEN:-}" ]; then
  append warn "tenant token configured" "set ENCLII_USER_TOKEN (non-admin) to run"
  echo "passed=$PASS failed=$FAIL warnings=$WARN"
  exit 0
fi

code="$(auth_code GET "${API_URL%/}/v1/projects")"
if [ "$code" = "200" ]; then
  append pass "project list authenticated" "HTTP 200"
elif [ "$code" = "401" ] || [ "$code" = "403" ]; then
  append fail "project list authenticated" "HTTP $code — token invalid or expired"
  echo "passed=$PASS failed=$FAIL warnings=$WARN"
  exit 1
else
  append fail "project list authenticated" "unexpected HTTP ${code:-000}"
fi

code="$(auth_code GET "${API_URL%/}/v1/junctions/${JUNCTION_ID}")"
err_code="$(json_code)"
if [ "$code" = "404" ] && [ "$err_code" = "NOT_FOUND" ]; then
  append pass "cross-tenant junction denied" "HTTP 404 NOT_FOUND"
elif [ "$code" = "200" ]; then
  append fail "cross-tenant junction denied" "HTTP 200 — IDOR: junction readable across tenants"
else
  append fail "cross-tenant junction denied" "expected 404 NOT_FOUND, got HTTP ${code:-000} code=${err_code:-unknown}"
fi

if [ -n "$CRON_ID" ]; then
  code="$(auth_code GET "${API_URL%/}/v1/cron-jobs/${CRON_ID}")"
  err_code="$(json_code)"
  if [ "$code" = "404" ] && [ "$err_code" = "NOT_FOUND" ]; then
    append pass "cross-tenant cron denied" "HTTP 404 NOT_FOUND"
  elif [ "$code" = "200" ]; then
    append fail "cross-tenant cron denied" "HTTP 200 — IDOR"
  else
    append fail "cross-tenant cron denied" "expected 404 NOT_FOUND, got HTTP ${code:-000}"
  fi
else
  append warn "cross-tenant cron denied" "no cron_jobs in prod; set ENCLII_CROSS_TENANT_CRON_ID when available"
fi

echo "passed=$PASS failed=$FAIL warnings=$WARN"
[ "$FAIL" -eq 0 ] || exit 1
