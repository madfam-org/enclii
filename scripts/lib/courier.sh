#!/bin/bash
# =============================================================================
# Angelia Courier sender — the estate's ONE door for third-party messaging
# =============================================================================
#
# internal-devops decisions/2026-09-05-third-party-messaging-via-angelia-courier.md
# (item M3): no script in this estate holds a bot token or an incoming-webhook
# URL for Slack, Telegram, WhatsApp or any successor network. A script that
# needs to reach a person POSTs a channel-agnostic ENVELOPE to Angelia Courier
# (`POST /v1/courier/send`, angelia docs/adr/0010-courier-outbound-comms-substrate.md)
# and Courier owns the adapter, the retry policy, quiet hours and the delivery
# ledger. A message that bypassed Courier has no ledger row, and a message with
# no record is a policy violation, not merely an unlogged one.
#
# WHY THIS IS A LIBRARY AND NOT COPY-PASTE. ADR-0010's whole complaint is "N
# tokens, N retry loops, N silent failure modes". Two scripts with two
# hand-rolled envelope builders is that complaint in miniature. One
# implementation, one place to fix, one place a test can stub.
#
# Usage:
#   SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
#   source "$SCRIPT_DIR/lib/courier.sh"
#   COURIER_SCRIPT_ID=my-script
#   courier_send "<check-name>" "<message text>"
#
# Environment:
#   COURIER_URL        Courier base URL.        Default: https://api.angelia.run
#   COURIER_API_KEY    Producer key. REQUIRED to send; empty = print and skip.
#                      Sourced from secret/angelia:courier_producer_key_enclii_ops
#                      (intake target `angelia/courier-producer-keys`).
#   COURIER_CHANNEL    telegram | slack | whatsapp | matrix. Default: telegram
#   COURIER_RECIPIENT  Channel-scoped recipient reference. REQUIRED to send.
#                      A chat/channel id is configuration, not a credential —
#                      but it is still never committed to a public repo.
#   COURIER_TENANT     Tenant assertion, verified by Courier. Default: madfam
#
# THE KEY IS NEVER PRINTED and never appears in this process's argv: it is
# handed to curl through `--config -` on stdin, not through `-H` on the command
# line, so it cannot be read out of `ps` by another user on the host. Nothing
# here echoes it, and no error path includes it.
# =============================================================================

if [[ -n "${_ENCLII_COURIER_LOADED:-}" ]]; then
    return 0
fi
_ENCLII_COURIER_LOADED=1

# Escape a string for embedding in a JSON string literal. Pure bash on
# purpose: these scripts run from cron on operator hosts where python3 or jq
# may not be installed, and a notifier that fails to load is a notifier that
# does not notify.
_courier_json_escape() {
    local s="$1"
    s="${s//\\/\\\\}"
    s="${s//\"/\\\"}"
    s="${s//$'\n'/\\n}"
    s="${s//$'\r'/\\r}"
    s="${s//$'\t'/\\t}"
    printf '%s' "$s"
}

# Map a Courier channel to the recipient kind it accepts. Courier enforces this
# pairing at the edge (envelope.ts RECIPIENT_KINDS_BY_CHANNEL) and refuses a
# mismatch, so getting it wrong here is a 400, not a misdelivery.
_courier_recipient_kind() {
    case "$1" in
        telegram) printf 'telegram_chat' ;;
        slack)    printf 'slack_channel' ;;
        whatsapp) printf 'e164' ;;
        matrix)   printf 'matrix_room' ;;
        *)        return 1 ;;
    esac
}

# courier_send <check-name> <text>
#
# Returns 0 when Courier took custody, 1 when it did not. Callers must NOT let
# that return code decide their own exit status: whether a health check found a
# problem and whether the notification about it was delivered are two different
# facts, and collapsing them turns a Courier outage into a false red on every
# check. Same discipline as angelia's own alarms hook.
courier_send() {
    local check="$1" text="$2"

    local url="${COURIER_URL:-https://api.angelia.run}"
    local channel="${COURIER_CHANNEL:-telegram}"
    local tenant="${COURIER_TENANT:-madfam}"
    local recipient="${COURIER_RECIPIENT:-}"
    local script_id="${COURIER_SCRIPT_ID:-$(basename "${BASH_SOURCE[1]:-unknown}" .sh)}"

    # Unconfigured means "print and skip", exactly as the old webhook path did
    # when its URL was unset. It is deliberately not an error: these scripts
    # must stay runnable by hand, on a laptop, with no credential at all.
    if [[ -z "${COURIER_API_KEY:-}" ]]; then
        echo "[courier] COURIER_API_KEY not set — alert printed, not sent." >&2
        return 1
    fi
    if [[ -z "$recipient" ]]; then
        echo "[courier] COURIER_RECIPIENT not set — alert printed, not sent." >&2
        return 1
    fi

    local kind
    if ! kind="$(_courier_recipient_kind "$channel")"; then
        echo "[courier] unknown COURIER_CHANNEL '$channel' — alert printed, not sent." >&2
        return 1
    fi

    # Hourly idempotency window, matching the Courier receiver's own
    # `alarm:<alertname>:<hour>` key. These scripts run every 5 minutes from
    # cron; without a coarse window one unchanging outage would send twelve
    # messages an hour. Re-sending the same key returns the FIRST delivery and
    # sends nothing, so the condition messages once per hour while it lasts.
    local hour
    hour="$(date -u '+%Y-%m-%dT%H')"

    local body_file
    body_file="$(mktemp)" || return 1
    # The body is not secret (the key is not in it) but the file is short-lived
    # and private anyway — an alert body can name a failing internal endpoint.
    chmod 600 "$body_file"

    printf '{"tenant":"%s","channel":"%s","recipient":{"kind":"%s","ref":"%s"},"body":{"kind":"raw","text":"%s"},"idempotency_key":"healthcheck:%s:%s:%s","priority":"urgent","consent":{"class":"operational"}}' \
        "$(_courier_json_escape "$tenant")" \
        "$(_courier_json_escape "$channel")" \
        "$kind" \
        "$(_courier_json_escape "$recipient")" \
        "$(_courier_json_escape "$text")" \
        "$(_courier_json_escape "$script_id")" \
        "$(_courier_json_escape "$check")" \
        "$hour" \
        > "$body_file"

    # `--config -`: the producer key reaches curl on STDIN, so it is absent
    # from argv and from anything that reads /proc/<pid>/cmdline. Courier
    # authenticates producers with X-Internal-API-Key (ADR-0010 § Producer
    # authentication), mirroring Janua's service-to-service pattern.
    local rc=0
    printf 'header = "X-Internal-API-Key: %s"\n' "$COURIER_API_KEY" |
        curl --config - \
            -sS -o /dev/null \
            -X POST "${url%/}/v1/courier/send" \
            -H 'Content-Type: application/json' \
            --max-time 10 \
            --data-binary "@${body_file}" || rc=$?

    rm -f "$body_file"

    if [[ $rc -ne 0 ]]; then
        # Loud on stderr, never fatal, and never quoting the response — a
        # Courier error body is a log line and must not become a place a
        # credential could surface.
        echo "[courier] send failed (curl exit $rc) for check '$check'." >&2
        return 1
    fi
    return 0
}
