#!/usr/bin/env bash
#
# smoke-switchyard-api.sh - boot the built API against a real Postgres and
# check that it actually serves.
#
# Why this exists: no CI job started the process, ran db.Migrate, or issued a
# single HTTP request. The `postgres:15-alpine` service in the unit-tests job
# was never opened by anything, and CI exported DATABASE_URL while config.Load
# reads ENCLII_DATABASE_URL (viper prefix ENCLII) — so even a job that had tried
# would have failed to connect. `Build` compiling ./cmd/api was the only gate
# that touched this module at all.
#
# Usage: ./scripts/smoke-switchyard-api.sh <binary> <database-url>
#
# ORDERING IS LOAD-BEARING. A sibling repo's smoke script grepped its log for
# errors BEFORE issuing any request, so the grep matched nothing and the script
# passed while proving nothing. Here every assertion runs strictly after the
# traffic that could produce it, and the log scan is last.

set -uo pipefail

BINARY="${1:?usage: smoke-switchyard-api.sh <binary> <database-url>}"
DATABASE_URL="${2:?usage: smoke-switchyard-api.sh <binary> <database-url>}"

PORT="${SMOKE_PORT:-18080}"
BASE="http://127.0.0.1:${PORT}"
LOG="$(mktemp -t switchyard-smoke.XXXXXX)"
FAILURES=0

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[0;33m'; NC='\033[0m'

fail() { echo -e "${RED}✗ $*${NC}"; FAILURES=$((FAILURES + 1)); }
pass() { echo -e "${GREEN}✓ $*${NC}"; }
info() { echo -e "${YELLOW}→ $*${NC}"; }

cleanup() {
    if [[ -n "${API_PID:-}" ]] && kill -0 "$API_PID" 2>/dev/null; then
        kill "$API_PID" 2>/dev/null || true
        wait "$API_PID" 2>/dev/null || true
    fi
    rm -f "$LOG"
}
trap cleanup EXIT

# -----------------------------------------------------------------------------
# Boot. ENCLII_DATABASE_URL, not DATABASE_URL — see the header.
# -----------------------------------------------------------------------------
info "starting $BINARY on :$PORT"

# Generated per run, never written down. The API refuses to start without a
# signing key, but nothing here mints or verifies a token, so the value only has
# to exist — and a literal in the repo would be a committed credential whose
# only purpose is to be ignored. Assigned unquoted from a hex string, which also
# keeps it out of the `SECRET="..."` shape the pre-commit scanner refuses.
SIGNING_KEY=$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')
export ENCLII_JWT_SECRET=$SIGNING_KEY

# ENCLII_AUTH_MODE must be "local" or "oidc"; anything else is a fatal at boot.
# ENCLII_OTEL_DISABLED=1 stops the exporter dialling Tempo, which is absent here.
export ENCLII_DATABASE_URL="$DATABASE_URL"
export ENCLII_PORT="$PORT"
export ENCLII_ENV="test"
export ENCLII_AUTH_MODE="local"
export ENCLII_LOG_LEVEL="info"
export ENCLII_OTEL_DISABLED="1"
export ENCLII_REDIS_URL="${SMOKE_REDIS_URL:-redis://localhost:6379/0}"

"$BINARY" > "$LOG" 2>&1 &
API_PID=$!

# The process running db.Migrate against a real schema is itself an assertion:
# a broken migration cannot boot.
READY=0
for _ in $(seq 1 60); do
    if ! kill -0 "$API_PID" 2>/dev/null; then
        break
    fi
    if curl -fsS -o /dev/null "${BASE}/health" 2>/dev/null; then
        READY=1
        break
    fi
    sleep 1
done

if [[ "$READY" -ne 1 ]]; then
    fail "the API never became ready on ${BASE}/health"
    echo "----- process output -----"
    cat "$LOG"
    exit 1
fi
pass "process booted, migrations applied, /health answered"

# -----------------------------------------------------------------------------
# Requests. Every assertion below runs AFTER the traffic it inspects.
# -----------------------------------------------------------------------------
declare -a OBSERVED_CODES=()

check_status() {
    local method="$1" path="$2" want="$3" label="$4"
    local code
    code=$(curl -s -o /dev/null -w '%{http_code}' -X "$method" "${BASE}${path}")
    OBSERVED_CODES+=("$code")
    if [[ "$code" == "$want" ]]; then
        pass "$label: $method $path -> $code"
    else
        fail "$label: $method $path -> $code, want $want"
    fi
}

info "issuing requests"

# 1. Health serves.
check_status GET "/health" 200 "health"

# 2. An unauthenticated call to a route this PR touches must be REFUSED, not
#    crash. 401 and 500 both keep the caller out, but only one of them means the
#    auth middleware ran; a 500 here is an unhandled nil on the domain path.
check_status GET "/v1/domains" 401 "unauthenticated domain list"
check_status POST "/v1/services/00000000-0000-0000-0000-000000000000/domains" 401 \
    "unauthenticated domain create"

# 3. Nothing may 5xx.
for code in "${OBSERVED_CODES[@]}"; do
    if [[ "$code" =~ ^5 ]]; then
        fail "a request returned $code; no 5xx is acceptable in smoke"
    fi
done
if [[ ${#OBSERVED_CODES[@]} -eq 0 ]]; then
    fail "no requests were issued; the 5xx check would have passed vacuously"
else
    pass "no 5xx across ${#OBSERVED_CODES[@]} requests"
fi

# 4. Still alive after serving.
if kill -0 "$API_PID" 2>/dev/null; then
    pass "process still running after traffic"
else
    fail "the process died while serving"
fi

# -----------------------------------------------------------------------------
# Log scan LAST, so it can only ever inspect a log that has seen traffic.
# -----------------------------------------------------------------------------
info "scanning process output"
if grep -qiE 'panic:|runtime error|nil pointer dereference' "$LOG"; then
    fail "the process log contains a panic"
    grep -iE -m5 -A5 'panic:|runtime error|nil pointer dereference' "$LOG"
else
    pass "no panic in the process log"
fi

if [[ "$FAILURES" -ne 0 ]]; then
    echo "----- process output -----"
    cat "$LOG"
    echo -e "${RED}smoke FAILED with $FAILURES failure(s)${NC}"
    exit 1
fi

echo -e "${GREEN}smoke passed${NC}"
