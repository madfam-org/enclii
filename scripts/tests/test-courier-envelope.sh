#!/usr/bin/env bash
# Cases for the Angelia Courier sender in scripts/lib/courier.sh and its two
# callers, scripts/health-check.sh and scripts/ops/janua-healthcheck.sh.
#
# WHAT THIS PINS, and why each one is worth a test:
#
#   1. THE ENVELOPE SHAPE. Courier validates the envelope with a zod schema at
#      the edge (angelia apps/api/src/courier/envelope.ts) and refuses anything
#      that does not match — including a `body` carrying an extra field, which
#      `.strict()` rejects. A malformed envelope is a 400 at the exact moment
#      production is on fire, so the shape is asserted field by field here
#      rather than discovered during an incident.
#
#   2. THE KEY NEVER APPEARS IN STDOUT. The producer key is handed to curl on
#      STDIN through `--config -`, so it is absent from argv too — this test's
#      stub curl dumps its own arguments precisely so a regression to
#      `-H "X-Internal-API-Key: $KEY"` would be caught here.
#
#   3. UNCONFIGURED PRINTS AND SKIPS. With no key the scripts must still run
#      and still print their alerts. That was the old behaviour with no webhook
#      set, and a health check that refuses to run without a notification
#      credential is a health check nobody can run by hand.
#
# The stub `curl` is a real file on PATH. Nothing here reaches the network, and
# `COURIER_URL` points at an unroutable placeholder so a broken stub fails
# loudly instead of silently sending somewhere.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
TMPROOT="$(mktemp -d)"
trap 'rm -rf "$TMPROOT"' EXIT

fails=0
run=0

# A recognisable, obviously-fake producer key. If this string ever shows up in
# a script's stdout, the leak assertion below fails and names it.
FAKE_KEY="TESTONLY-c0ffee-not-a-real-key"

STUB_BIN="$TMPROOT/bin"
mkdir -p "$STUB_BIN"
cat > "$STUB_BIN/curl" <<'STUB'
#!/usr/bin/env bash
# Stub curl: records what it was asked to send, sends nothing.
# argv goes to one file, the request body to another, stdin to a third.
#
# Stdin is read ONLY when `--config -` asked for it. health-check.sh also calls
# curl to probe endpoints, with no stdin redirect at all; an unconditional
# `cat` there blocks the whole suite forever on the terminal.
: > "$COURIER_TEST_ARGS"
for a in "$@"; do printf '%s\n' "$a" >> "$COURIER_TEST_ARGS"; done
prev=""
wants_stdin=0
body_src=""
for a in "$@"; do
  [[ "$prev" == "--config" && "$a" == "-" ]] && wants_stdin=1
  [[ "$prev" == "--data-binary" && "$a" == @* ]] && body_src="${a#@}"
  prev="$a"
done
if [[ "$wants_stdin" == 1 ]]; then
  cat > "$COURIER_TEST_STDIN"
else
  : > "$COURIER_TEST_STDIN"
fi
if [[ -n "$body_src" ]]; then
  cat "$body_src" > "$COURIER_TEST_BODY"
else
  : > "$COURIER_TEST_BODY"
fi
exit 0
STUB
chmod +x "$STUB_BIN/curl"

check() {
  local name="$1" cond="$2" detail="${3:-}"
  run=$((run + 1))
  if [[ "$cond" == "ok" ]]; then
    printf 'ok   %s\n' "$name"
  else
    printf 'FAIL %s\n' "$name"
    [[ -n "$detail" ]] && printf '%s\n' "$detail" | sed 's/^/     | /'
    fails=$((fails + 1))
  fi
}

# assert_contains <name> <haystack> <needle>
assert_contains() {
  local name="$1" hay="$2" needle="$3"
  if [[ "$hay" == *"$needle"* ]]; then
    check "$name" ok
  else
    check "$name" bad "expected to find: $needle"$'\n'"in: $hay"
  fi
}

assert_not_contains() {
  local name="$1" hay="$2" needle="$3"
  if [[ "$hay" != *"$needle"* ]]; then
    check "$name" ok
  else
    check "$name" bad "must NOT contain: $needle"$'\n'"in: $hay"
  fi
}

# ---------------------------------------------------------------------------
# 1. The envelope the library builds
# ---------------------------------------------------------------------------
export COURIER_TEST_ARGS="$TMPROOT/args.txt"
export COURIER_TEST_BODY="$TMPROOT/body.json"
export COURIER_TEST_STDIN="$TMPROOT/stdin.txt"

out="$(
  PATH="$STUB_BIN:$PATH" \
  COURIER_URL="http://courier.invalid" \
  COURIER_API_KEY="$FAKE_KEY" \
  COURIER_CHANNEL="telegram" \
  COURIER_RECIPIENT="recipient-ref" \
  COURIER_TENANT="madfam" \
  COURIER_SCRIPT_ID="unit" \
  bash -c 'source "'"$REPO"'/scripts/lib/courier.sh"; courier_send "endpoints" "line one
line \"two\""' 2>&1
)"
rc=$?
check "library send returns 0" "$([[ $rc -eq 0 ]] && echo ok || echo bad)" "$out"

body="$(cat "$COURIER_TEST_BODY" 2>/dev/null || true)"
args="$(cat "$COURIER_TEST_ARGS" 2>/dev/null || true)"
stdin_seen="$(cat "$COURIER_TEST_STDIN" 2>/dev/null || true)"

assert_contains 'posts to /v1/courier/send' "$args" 'http://courier.invalid/v1/courier/send'
assert_contains 'tenant asserted'           "$body" '"tenant":"madfam"'
assert_contains 'channel is telegram'       "$body" '"channel":"telegram"'
assert_contains 'recipient kind matches channel' "$body" '"recipient":{"kind":"telegram_chat","ref":"recipient-ref"}'
assert_contains 'body is a raw envelope'    "$body" '"body":{"kind":"raw","text":"line one\nline \"two\""}'
assert_contains 'idempotency key is scoped to script, check and hour' \
  "$body" "\"idempotency_key\":\"healthcheck:unit:endpoints:$(date -u '+%Y-%m-%dT%H')\""
assert_contains 'priority is urgent'        "$body" '"priority":"urgent"'
assert_contains 'consent class is operational' "$body" '"consent":{"class":"operational"}'

# The key travels on stdin, never in argv, and never in stdout.
assert_contains     'key reaches curl on stdin'     "$stdin_seen" "X-Internal-API-Key: $FAKE_KEY"
assert_not_contains 'key is absent from curl argv'  "$args"       "$FAKE_KEY"
assert_not_contains 'key is absent from stdout'     "$out"        "$FAKE_KEY"

# ---------------------------------------------------------------------------
# 2. slack channel maps to the slack recipient kind
# ---------------------------------------------------------------------------
PATH="$STUB_BIN:$PATH" \
COURIER_URL="http://courier.invalid" \
COURIER_API_KEY="$FAKE_KEY" \
COURIER_CHANNEL="slack" \
COURIER_RECIPIENT="channel-ref" \
COURIER_SCRIPT_ID="unit" \
bash -c 'source "'"$REPO"'/scripts/lib/courier.sh"; courier_send "c" "t"' >/dev/null 2>&1
assert_contains 'slack channel uses slack_channel kind' \
  "$(cat "$COURIER_TEST_BODY")" '"recipient":{"kind":"slack_channel","ref":"channel-ref"}'

# ---------------------------------------------------------------------------
# 3. Unconfigured: print the alert, send nothing
# ---------------------------------------------------------------------------
: > "$COURIER_TEST_BODY"
out="$(
  PATH="$STUB_BIN:$PATH" \
  COURIER_API_KEY="" COURIER_RECIPIENT="r" COURIER_SCRIPT_ID="unit" \
  bash -c 'source "'"$REPO"'/scripts/lib/courier.sh"; courier_send "c" "t"' 2>&1
)"
rc=$?
check "no key -> nonzero return" "$([[ $rc -ne 0 ]] && echo ok || echo bad)" "$out"
check "no key -> nothing sent" \
  "$([[ ! -s "$COURIER_TEST_BODY" ]] && echo ok || echo bad)" "$(cat "$COURIER_TEST_BODY")"
assert_contains 'no key -> says so' "$out" 'COURIER_API_KEY not set'

: > "$COURIER_TEST_BODY"
out="$(
  PATH="$STUB_BIN:$PATH" \
  COURIER_API_KEY="$FAKE_KEY" COURIER_RECIPIENT="" COURIER_SCRIPT_ID="unit" \
  bash -c 'source "'"$REPO"'/scripts/lib/courier.sh"; courier_send "c" "t"' 2>&1
)"
check "no recipient -> nothing sent" \
  "$([[ ! -s "$COURIER_TEST_BODY" ]] && echo ok || echo bad)" "$(cat "$COURIER_TEST_BODY")"
assert_not_contains 'no recipient -> key still absent from stdout' "$out" "$FAKE_KEY"

# ---------------------------------------------------------------------------
# 4. health-check.sh end to end against the stub
# ---------------------------------------------------------------------------
# Every endpoint fails (the stub curl writes nothing, so the http_code capture
# is empty), which is what drives the alert path. The script exits 1 on a
# failing endpoint — that is the health verdict, not a delivery verdict.
: > "$COURIER_TEST_BODY"
out="$(
  PATH="$STUB_BIN:$PATH" \
  COURIER_URL="http://courier.invalid" \
  COURIER_API_KEY="$FAKE_KEY" \
  COURIER_RECIPIENT="recipient-ref" \
  bash "$REPO/scripts/health-check.sh" 2>&1
)"
assert_contains     'health-check prints the alert'  "$out"  'ALERT:'
assert_not_contains 'health-check never echoes the key' "$out" "$FAKE_KEY"
assert_contains     'health-check sends a Courier envelope' \
  "$(cat "$COURIER_TEST_BODY")" '"consent":{"class":"operational"}'
assert_contains     'health-check scopes the key to its own script id' \
  "$(cat "$COURIER_TEST_BODY")" '"idempotency_key":"healthcheck:health-check:endpoints:'

# ---------------------------------------------------------------------------
# 5. janua-healthcheck.sh loads and holds no webhook
# ---------------------------------------------------------------------------
# It cannot be run end to end here (it ssh's into a cluster), so this asserts
# the two things that are checkable without one: it parses, and its send path
# is the shared library rather than a hand-rolled POST.
if bash -n "$REPO/scripts/ops/janua-healthcheck.sh" 2>/dev/null; then
  check "janua-healthcheck parses" ok
else
  check "janua-healthcheck parses" bad
fi
janua_src="$(cat "$REPO/scripts/ops/janua-healthcheck.sh")"
assert_contains     'janua-healthcheck sources the Courier library' "$janua_src" 'lib/courier.sh'
assert_contains     'janua-healthcheck calls courier_send'          "$janua_src" 'courier_send '
assert_not_contains 'janua-healthcheck holds no webhook variable'   "$janua_src" 'WEBHOOK'

printf '\n%s case(s), %s failure(s)\n' "$run" "$fails"
[[ "$fails" -eq 0 ]] || exit 1
