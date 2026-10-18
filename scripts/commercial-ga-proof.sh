#!/usr/bin/env bash
# Enclii commercial GA proof harness.
#
# Public mode checks health, landing/pricing visibility, signup page/API shell,
# and optional Dhanam checkout reachability. Authenticated mode also checks
# project billing cost, budgets, and throttles. The script never prints bearer
# tokens or secret values.
#
# Verdicts (read proof, internal-devops docs/monetization-engine.md §5.3: "I
# read nothing" must never look like "all clear"):
#
#   PASS   the thing was read and is what a paying buyer needs.
#   FAIL   the thing is broken (4xx/5xx where it must answer, no answer, or an
#          answer we cannot place). The script exits 1.
#   WARN   an optional proof input is missing (FAIL under STRICT=true).
#   CHECK  read successfully, and it is a KNOWN non-pass state that only the
#          owner can change (e.g. hosted checkout switched off in production,
#          so checkout lands on the sales booking page). Never counted as a
#          pass, never a failure: it does not change the exit code, so a
#          standing known state cannot keep a critical CI page open. It is
#          printed as CHECK, raised as a ::warning:: annotation, written into
#          the job summary, and sets "verdict": "not_proven" in the JSON.

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
# Space-separated hosts (host or host:port). A checkout that ends on one of the
# CHECKOUT_HOSTS with HTTP 2xx is a PASS; one that ends on a SALES_BOOKING_HOSTS
# page is a CHECK (Dhanam's /checkout falls back to the booking page while
# HOSTED_CHECKOUT_ENABLED is off, or when it does not know the plan). Anywhere
# else is a FAIL.
CHECKOUT_HOSTS="${CHECKOUT_HOSTS:-app.dhan.am}"
SALES_BOOKING_HOSTS="${SALES_BOOKING_HOSTS:-kalya.app}"

PASS_COUNT=0
FAIL_COUNT=0
WARN_COUNT=0
CHECK_COUNT=0
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
    check)
      CHECK_COUNT=$((CHECK_COUNT + 1))
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

  # Surface non-passes on the run page without changing the exit code.
  if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
    # Workflow-command escaping: % first, then the title's property separators.
    local title="${name//%/%25}"
    title="${title//,/%2C}"
    title="${title//:/%3A}"
    local message="${detail//%/%25}"
    case "$status" in
      check) printf "::warning title=CHECK %s::%s\n" "$title" "$message" ;;
      warn) printf "::warning title=WARN %s::%s\n" "$title" "$message" ;;
      fail) printf "::error title=FAIL %s::%s\n" "$title" "$message" ;;
    esac
  fi
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

# Like http_status, but prints "<code> <final url>" after following redirects.
http_status_final() {
  local url="$1"
  curl -sS -L --max-time "$TIMEOUT" -o "$BODY_FILE" -w "%{http_code} %{url_effective}" "$url" || true
}

# url_host_in <url> <space-separated hosts>: true when the URL's host (or
# host:port) is one of the hosts or a subdomain of one.
url_host_in() {
  python3 - "$1" "$2" <<'PY'
import sys
from urllib.parse import urlsplit

url, hosts = sys.argv[1], sys.argv[2].split()
parts = urlsplit(url)
netloc = (parts.netloc or "").lower().rsplit("@", 1)[-1]
host = (parts.hostname or "").lower()
for h in (x.lower() for x in hosts):
    target = netloc if ":" in h else host
    if target == h or target.endswith("." + h):
        sys.exit(0)
sys.exit(1)
PY
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
  python3 - "$SUMMARY_PATH" "$CHECKS_FILE" "$PASS_COUNT" "$FAIL_COUNT" "$WARN_COUNT" "$CHECK_COUNT" "$API_URL" "$APP_URL" "$LANDING_URL" "$STRICT" "${GITHUB_STEP_SUMMARY:-}" <<'PY'
import json
import sys
from datetime import datetime, timezone

(summary_path, checks_path, passed, failed, warned, checked,
 api_url, app_url, landing_url, strict, step_summary) = sys.argv[1:]
checks = []
with open(checks_path, "r", encoding="utf-8") as f:
    for line in f:
        if line.strip():
            checks.append(json.loads(line))

failed_n, warned_n, checked_n = int(failed), int(warned), int(checked)
# "proven" only when every check passed. Zero checks read is not proven either.
if failed_n:
    verdict = "failed"
elif warned_n or checked_n or not checks:
    verdict = "not_proven"
else:
    verdict = "proven"

payload = {
    "checked_at": datetime.now(timezone.utc).isoformat(),
    "api_url": api_url,
    "app_url": app_url,
    "landing_url": landing_url,
    "strict": strict == "true",
    "verdict": verdict,
    "listed": len(checks),
    "passed": int(passed),
    "failed": failed_n,
    "warnings": warned_n,
    "checks_pending": checked_n,
    "checks": checks,
}
with open(summary_path, "w", encoding="utf-8") as f:
    json.dump(payload, f, indent=2)
    f.write("\n")

if step_summary:
    def cell(text):
        return str(text).replace("|", "\\|").replace("\n", " ")
    lines = [
        "## Commercial GA proof: " + verdict.upper().replace("_", " "),
        "",
        f"listed={len(checks)} passed={passed} failed={failed_n} "
        f"warnings={warned_n} checks={checked_n}",
        "",
    ]
    if checked_n:
        lines += [
            "> CHECK rows are known non-pass states that only an owner action can",
            "> change. They do not fail this job, and they are not a pass.",
            "",
        ]
    lines += ["| Verdict | Check | Detail |", "|---|---|---|"]
    for c in checks:
        lines.append(f"| {c['status'].upper()} | {cell(c['name'])} | {cell(c['detail'])} |")
    with open(step_summary, "a", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")
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

# A buyer must be able to load the signup page. Redirects are followed, so the
# final answer must be 2xx; a 404, an auth wall or a 5xx is a FAIL.
code="$(http_status "${APP_URL%/}/signup")"
if [[ "$code" =~ ^2[0-9][0-9]$ ]]; then
  append_check pass "signup page shell" "HTTP $code"
else
  append_check fail "signup page shell" "expected HTTP 2xx, got ${code:-000}"
fi

# POST /v1/signup is the only route at /v1/signup, so a GET there is a 404
# whether signup works or not. Probe the read-only status route instead, with
# an id that is not a UUID: when signup is enabled the handler answers 400
# "invalid signup id" before touching the database; when it is disabled (or the
# route is gone) the answer is 404. Nothing is created.
code="$(http_status "${API_URL%/}/v1/signup/ga-proof/status")"
if [ "$code" = "400" ] && body_contains 'invalid signup id'; then
  append_check pass "signup api live" "HTTP 400 invalid signup id (handler reached, signup enabled)"
elif [ "$code" = "404" ]; then
  append_check fail "signup api live" "HTTP 404: self-serve signup is disabled or unrouted"
else
  append_check fail "signup api live" "expected HTTP 400 invalid signup id, got ${code:-000}"
fi

if [ -n "$DHANAM_CHECKOUT_URL" ]; then
  # Where the buyer ENDS, not whether something answered: Dhanam's /checkout
  # 302s to the sales booking page while hosted checkout is off, and that page
  # answers 200.
  read -r code final_url <<<"$(http_status_final "$DHANAM_CHECKOUT_URL")"
  code="${code:-000}"
  final_url="${final_url:--}"
  if [[ ! "$code" =~ ^2[0-9][0-9]$ ]]; then
    append_check fail "Dhanam checkout reachability" "HTTP $code at $final_url"
  elif url_host_in "$final_url" "$SALES_BOOKING_HOSTS"; then
    append_check check "checkout routes to sales booking (hosted checkout off in production)" \
      "landed on $final_url (HTTP $code); owner action: HOSTED_CHECKOUT_ENABLED=true on dhanam-web, or the plan is missing from Dhanam's catalog"
  elif url_host_in "$final_url" "$CHECKOUT_HOSTS"; then
    append_check pass "Dhanam checkout reachability" "hosted checkout page served at $final_url (HTTP $code); proves reachability, not a completed purchase"
  else
    append_check fail "Dhanam checkout reachability" "landed on unexpected host: $final_url (HTTP $code)"
  fi
else
  warn_or_fail "Dhanam checkout reachability" "set DHANAM_CHECKOUT_URL to prove paid checkout"
fi

if [ -n "$TOKEN" ] && [ -n "$PROJECT_SLUG" ]; then
  code="$(http_json_auth "${API_URL%/}/v1/projects/${PROJECT_SLUG}/billing/cost?period=mtd")"
  if [ "$code" = "200" ] && body_contains 'cost|total|period'; then
    append_check pass "billing cost endpoint" "HTTP 200"
    # The endpoint answering is not spend being measured: total_cents is 0
    # both for a real zero and when the Waybill aggregator never ran.
    metering="$(python3 - "$BODY_FILE" <<'PY'
import json
import sys

try:
    with open(sys.argv[1], encoding="utf-8") as f:
        body = json.load(f)
except Exception:
    print("unreadable")
    sys.exit(0)
m = body.get("metering") if isinstance(body, dict) else None
if not isinstance(m, dict):
    print("unreported")
else:
    print(str(m.get("status") or "unreported"))
PY
)"
    case "$metering" in
      ok)
        append_check pass "billing metering" "metering.status ok (spend is measured)" ;;
      not_running|stale|unknown)
        append_check check "billing metering ${metering} (spend unknown)" \
          "metering.status=${metering}; the Waybill aggregator must run before spend or budgets mean anything" ;;
      unreported)
        append_check check "billing metering not reported (spend unverified)" \
          "this API build does not report metering.status, so total_cents cannot be told apart from no data" ;;
      *)
        append_check fail "billing metering" "unreadable cost body (${metering})" ;;
    esac
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

if [ -n "$TOKEN" ]; then
  code="$(http_json_auth "${API_URL%/}/v1/admin/db/schema")"
  if [ "$code" = "200" ] && body_contains '"healthy"[[:space:]]*:[[:space:]]*true'; then
    append_check pass "db schema healthy" "migration verify OK"
  elif [ "$code" = "200" ]; then
    warn_or_fail "db schema healthy" "HTTP 200 but healthy!=true (check pending/dirty/columns)"
  elif [ "$code" = "403" ] || [ "$code" = "401" ]; then
    append_check warn "db schema healthy" "admin token required for GET /v1/admin/db/schema"
  else
    append_check fail "db schema healthy" "expected HTTP 200, got ${code:-000}"
  fi

  code="$(http_status "${API_URL%/}/v1/dashboard/stats")"
  if [ "$code" = "401" ] || [ "$code" = "403" ]; then
    append_check pass "dashboard stats auth gate" "HTTP $code"
  else
    append_check fail "dashboard stats auth gate" "expected 401/403 without auth, got ${code:-000}"
  fi

  code="$(curl -sS --max-time "$TIMEOUT" \
    -H "Accept: application/json" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $TOKEN" \
    -d '{"dry_run":true}' \
    -o "$BODY_FILE" -w "%{http_code}" \
    "${API_URL%/}/v1/providers/resend/credentials" || true)"
  if [ "$code" = "200" ] && body_contains 'apiKeyPresent|configured'; then
    append_check pass "resend credentials read" "HTTP 200"
  elif [ "$code" = "403" ] || [ "$code" = "401" ]; then
    append_check warn "resend credentials read" "admin token required for providers.resend.credentials"
  else
    warn_or_fail "resend credentials read" "expected HTTP 200 JSON, got ${code:-000}"
  fi
fi

write_summary
printf "Summary written to %s\n" "$SUMMARY_PATH"
printf "passed=%s failed=%s warnings=%s checks=%s\n" "$PASS_COUNT" "$FAIL_COUNT" "$WARN_COUNT" "$CHECK_COUNT"
if [ "$CHECK_COUNT" -gt 0 ]; then
  printf "NOT PROVEN: %s CHECK state(s) above are known non-pass states (not failures, not passes).\n" "$CHECK_COUNT"
fi

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi

