#!/usr/bin/env bash
# Enclii commercial GA proof harness.
#
# Public mode checks health, landing/pricing visibility, signup page/API shell,
# and optional Dhanam checkout reachability. Authenticated mode also checks
# project billing cost, budgets, and throttles. The script never prints bearer
# tokens or secret values.

set -euo pipefail

API_URL="${API_URL:-https://api.enclii.dev}"
APP_URL="${APP_URL:-https://app.enclii.dev}"
LANDING_URL="${LANDING_URL:-https://enclii.dev}"
TIMEOUT="${TIMEOUT:-10}"
STRICT="${ENCLII_COMMERCIAL_GA_STRICT:-false}"
SUMMARY_PATH="${SUMMARY_PATH:-enclii-commercial-ga-proof.json}"

TOKEN="${ENCLII_SYNTHETICS_TOKEN:-${ENCLII_API_TOKEN:-}}"
PROJECT_SLUG="${ENCLII_SYNTHETIC_PROJECT_SLUG:-}"
DHANAM_CHECKOUT_URL="${DHANAM_CHECKOUT_URL:-}"

PASS_COUNT=0
FAIL_COUNT=0
WARN_COUNT=0
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
    pass)
      PASS_COUNT=$((PASS_COUNT + 1))
      ;;
    fail)
      FAIL_COUNT=$((FAIL_COUNT + 1))
      ;;
    warn)
      WARN_COUNT=$((WARN_COUNT + 1))
      ;;
    *)
      FAIL_COUNT=$((FAIL_COUNT + 1))
      detail="invalid status: $status"
      status="fail"
      ;;
  esac

  python3 - "$CHECKS_FILE" "$status" "$name" "$detail" <<'PY'
import json
import sys

path, status, name, detail = sys.argv[1:]
with open(path, "a", encoding="utf-8") as f:
    f.write(json.dumps({"status": status, "name": name, "detail": detail}) + "\n")
PY

  printf "%s %s" "$(printf "%s" "$status" | tr '[:lower:]' '[:upper:]')" "$name"
  if [ -n "$detail" ]; then
    printf " - %s" "$detail"
  fi
  printf "\n"
}

warn_or_fail() {
  local name="$1"
  local detail="$2"
  if [ "$STRICT" = "true" ]; then
    append_check fail "$name" "$detail"
  else
    append_check warn "$name" "$detail"
  fi
}

http_status() {
  local url="$1"
  local method="${2:-GET}"
  curl -sS -L --max-time "$TIMEOUT" -o "$BODY_FILE" -w "%{http_code}" -X "$method" "$url" || true
}

http_json_auth() {
  local url="$1"
  local code
  code="$(curl -sS --max-time "$TIMEOUT" -H "Accept: application/json" -H "Authorization: Bearer $TOKEN" -o "$BODY_FILE" -w "%{http_code}" "$url" || true)"
  printf "%s" "$code"
}

body_contains() {
  local pattern="$1"
  grep -Eiq "$pattern" "$BODY_FILE"
}

write_summary() {
  python3 - "$SUMMARY_PATH" "$CHECKS_FILE" "$PASS_COUNT" "$FAIL_COUNT" "$WARN_COUNT" "$API_URL" "$APP_URL" "$LANDING_URL" "$STRICT" <<'PY'
import json
import sys
from datetime import datetime, timezone

summary_path, checks_path, passed, failed, warned, api_url, app_url, landing_url, strict = sys.argv[1:]
checks = []
with open(checks_path, "r", encoding="utf-8") as f:
    for line in f:
        if line.strip():
            checks.append(json.loads(line))

payload = {
    "checked_at": datetime.now(timezone.utc).isoformat(),
    "api_url": api_url,
    "app_url": app_url,
    "landing_url": landing_url,
    "strict": strict == "true",
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

printf "Enclii commercial GA proof\n"
printf "API_URL=%s\n" "$API_URL"
printf "APP_URL=%s\n" "$APP_URL"
printf "LANDING_URL=%s\n" "$LANDING_URL"
printf "STRICT=%s\n" "$STRICT"

code="$(http_status "${API_URL%/}/health/public")"
if [ "$code" = "200" ] && body_contains '"ok"[[:space:]]*:[[:space:]]*true'; then
  append_check pass "public api health" "HTTP 200"
else
  code="$(http_status "${API_URL%/}/health")"
  if [ "$code" = "200" ]; then
    append_check pass "api health fallback" "HTTP 200"
  else
    append_check fail "api health" "expected HTTP 200, got ${code:-000}"
  fi
fi

code="$(http_status "$LANDING_URL")"
if [ "$code" = "200" ] && body_contains 'pricing|Essentials|Pro|Sovereign|\\$20'; then
  append_check pass "landing pricing visibility" "pricing marker present"
else
  warn_or_fail "landing pricing visibility" "expected pricing marker on landing, got HTTP ${code:-000}"
fi

code="$(http_status "${APP_URL%/}/signup")"
if [[ "$code" =~ ^[23][0-9][0-9]$ ]] || [ "$code" = "401" ] || [ "$code" = "403" ]; then
  append_check pass "signup page shell" "HTTP $code"
elif [[ "$code" =~ ^5[0-9][0-9]$ ]]; then
  append_check fail "signup page shell" "HTTP $code"
else
  warn_or_fail "signup page shell" "unexpected HTTP ${code:-000}"
fi

code="$(http_status "${API_URL%/}/v1/signup" "GET")"
if [[ "$code" =~ ^[234][0-9][0-9]$ ]]; then
  append_check pass "signup api shell" "HTTP $code"
elif [[ "$code" =~ ^5[0-9][0-9]$ ]]; then
  append_check fail "signup api shell" "HTTP $code"
else
  warn_or_fail "signup api shell" "unexpected HTTP ${code:-000}"
fi

if [ -n "$DHANAM_CHECKOUT_URL" ]; then
  code="$(http_status "$DHANAM_CHECKOUT_URL")"
  if [[ "$code" =~ ^[23][0-9][0-9]$ ]]; then
    append_check pass "Dhanam checkout reachability" "HTTP $code"
  elif [[ "$code" =~ ^5[0-9][0-9]$ ]]; then
    append_check fail "Dhanam checkout reachability" "HTTP $code"
  else
    warn_or_fail "Dhanam checkout reachability" "unexpected HTTP ${code:-000}"
  fi
else
  warn_or_fail "Dhanam checkout reachability" "set DHANAM_CHECKOUT_URL to prove paid checkout"
fi

if [ -n "$TOKEN" ] && [ -n "$PROJECT_SLUG" ]; then
  code="$(http_json_auth "${API_URL%/}/v1/projects/${PROJECT_SLUG}/billing/cost?period=mtd")"
  if [ "$code" = "200" ] && body_contains 'cost|total|period'; then
    append_check pass "billing cost endpoint" "HTTP 200"
  else
    append_check fail "billing cost endpoint" "expected HTTP 200 JSON, got ${code:-000}"
  fi

  code="$(http_json_auth "${API_URL%/}/v1/projects/${PROJECT_SLUG}/billing/budgets")"
  if [ "$code" = "200" ] && body_contains 'budgets'; then
    append_check pass "billing budgets endpoint" "HTTP 200"
  else
    append_check fail "billing budgets endpoint" "expected HTTP 200 JSON, got ${code:-000}"
  fi

  code="$(http_json_auth "${API_URL%/}/v1/projects/${PROJECT_SLUG}/billing/throttles")"
  if [ "$code" = "200" ] && body_contains 'throttles'; then
    append_check pass "billing throttles endpoint" "HTTP 200"
  else
    append_check fail "billing throttles endpoint" "expected HTTP 200 JSON, got ${code:-000}"
  fi
else
  warn_or_fail "authenticated billing endpoints" "set ENCLII_SYNTHETICS_TOKEN and ENCLII_SYNTHETIC_PROJECT_SLUG"
fi

write_summary
printf "Summary written to %s\n" "$SUMMARY_PATH"
printf "passed=%s failed=%s warnings=%s\n" "$PASS_COUNT" "$FAIL_COUNT" "$WARN_COUNT"

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi

