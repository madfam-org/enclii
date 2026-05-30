#!/usr/bin/env bash
# O-16 signup API wizard smoke (no email token required).
#
# Exercises the public signup surface after ENCLII_SIGNUP_ENABLED=true.
# Full verify → GitHub → provision requires the verification email token.
#
# Usage:
#   ./scripts/ga-signup-api-smoke.sh
#   API_URL=https://api.enclii.dev ./scripts/ga-signup-api-smoke.sh

set -euo pipefail

API_URL="${API_URL:-https://api.enclii.dev}"
APP_URL="${APP_URL:-https://app.enclii.dev}"
TIMEOUT="${TIMEOUT:-15}"

pass=0
fail=0

check() {
  local name="$1"
  local ok="$2"
  local detail="${3:-}"
  if [ "$ok" = "1" ]; then
    pass=$((pass + 1))
    printf "PASS %s" "$name"
  else
    fail=$((fail + 1))
    printf "FAIL %s" "$name"
  fi
  if [ -n "$detail" ]; then
    printf " - %s" "$detail"
  fi
  printf "\n"
}

http_code() {
  curl -sS --max-time "$TIMEOUT" -o /dev/null -w "%{http_code}" "$@"
}

body_file="$(mktemp)"
trap 'rm -f "$body_file"' EXIT

printf "Signup API wizard smoke\nAPI_URL=%s\nAPP_URL=%s\n\n" "$API_URL" "$APP_URL"

code="$(http_code -X POST -H 'Content-Type: application/json' -d '{"email":"not-an-email"}' "${API_URL%/}/v1/signup")"
check "POST /v1/signup invalid email" "$([ "$code" = "400" ] && echo 1 || echo 0)" "HTTP $code"

ts="$(date +%s)"
code="$(curl -sS --max-time "$TIMEOUT" -X POST -H 'Content-Type: application/json' \
  -d "{\"email\":\"ga-api-smoke+${ts}@example.com\",\"company_name\":\"GA API Smoke\"}" \
  -o "$body_file" -w "%{http_code}" "${API_URL%/}/v1/signup")"
signup_id="$(python3 - "$body_file" <<'PY'
import json, sys
try:
    with open(sys.argv[1]) as f:
        print(json.load(f).get("signup_id", ""))
except Exception:
    print("")
PY
)"
check "POST /v1/signup valid email" "$([ "$code" = "201" ] && [ -n "$signup_id" ] && echo 1 || echo 0)" "HTTP $code signup_id=${signup_id:-none}"

if [ -z "$signup_id" ]; then
  printf "\npassed=%s failed=%s\n" "$pass" "$fail"
  exit 1
fi

code="$(http_code "${API_URL%/}/v1/signup/${signup_id}/status")"
check "GET /v1/signup/:id/status" "$([ "$code" = "200" ] && echo 1 || echo 0)" "HTTP $code"

code="$(http_code -X POST -H 'Content-Type: application/json' -d '{"token":"invalid"}' \
  "${API_URL%/}/v1/signup/${signup_id}/verify")"
check "POST /v1/signup/:id/verify bad token" "$([ "$code" = "400" ] && echo 1 || echo 0)" "HTTP $code"

code="$(http_code "${API_URL%/}/v1/signup/${signup_id}/github/authorize")"
# 409 = wrong state (expected); 429 = rate limit (also acceptable for smoke)
check "GET /v1/signup/:id/github/authorize before verified" \
  "$([ "$code" = "409" ] || [ "$code" = "429" ] && echo 1 || echo 0)" "HTTP $code"

code="$(http_code "${APP_URL%/}/signup")"
check "GET app signup page" "$([ "$code" = "200" ] && echo 1 || echo 0)" "HTTP $code"

code="$(http_code "${APP_URL%/}/signup/verify?signup_id=${signup_id}&token=bad")"
check "GET app signup verify shell" "$([ "$code" = "200" ] && echo 1 || echo 0)" "HTTP $code"

printf "\npassed=%s failed=%s\n" "$pass" "$fail"
if [ "$fail" -gt 0 ]; then
  exit 1
fi
