#!/bin/bash
# Production Health Check Script
# Run: ./scripts/health-check.sh
# Cron: */5 * * * * /path/to/health-check.sh

set -euo pipefail

# Source shared logging library
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/logging.sh
source "$SCRIPT_DIR/lib/logging.sh"
# shellcheck source=lib/courier.sh
source "$SCRIPT_DIR/lib/courier.sh"

# Configuration
#
# ALERTS GO THROUGH ANGELIA COURIER, and through nothing else. The direct
# chat-webhook POST this script used to make was removed 2026-09-05 by
# internal-devops decisions/2026-09-05-third-party-messaging-via-angelia-courier.md
# (item M3): no script in this estate holds a webhook URL or a bot token for a
# messaging network, and its env var is gone with it. Courier owns the adapter
# and the delivery ledger; the envelope this script sends is channel-agnostic,
# so the day the estate moves from Telegram to Matrix nothing here changes.
#
# COURIER_API_KEY is `courier_producer_key_enclii_ops` from `secret/angelia`
# (intake target `angelia/courier-producer-keys`), read cross-path. With it
# unset the script still runs and still prints its alerts — it just does not
# send them, which is exactly what it did before with no webhook configured.
# See scripts/lib/courier.sh for the full env contract.
COURIER_SCRIPT_ID="health-check"
ENDPOINTS=(
    "https://enclii.dev|Landing Page"
    "https://app.enclii.dev|Dashboard"
    "https://api.enclii.dev/health|API Health"
    "https://docs.enclii.dev|Documentation"
    "https://auth.madfam.io/.well-known/openid-configuration|OIDC Discovery"
)

# Results
FAILED=()
PASSED=()

check_endpoint() {
    local url="${1%%|*}"
    local name="${1##*|}"

    local http_code
    http_code=$(curl -sL -o /dev/null -w "%{http_code}" --max-time 10 "$url" 2>/dev/null || echo "000")

    if [[ "$http_code" =~ ^2[0-9][0-9]$ ]] || [[ "$http_code" == "302" ]]; then
        PASSED+=("$name: $http_code")
        echo -e "${GREEN}✓${NC} $name ($url): $http_code"
        return 0
    else
        FAILED+=("$name: $http_code ($url)")
        echo -e "${RED}✗${NC} $name ($url): $http_code"
        return 1
    fi
}

send_alert() {
    local check="$1"
    local message="$2"

    # Print FIRST, send second. The printed alert is the record that survives a
    # Courier outage, and this script's own stdout is what the cron mail and
    # the log file carry. `|| true`: whether a delivery succeeded must never
    # change this script's exit code — that code answers "is production
    # healthy", not "did the notification arrive".
    echo -e "${RED}ALERT:${NC} $message"
    courier_send "$check" "$message" || true
}

main() {
    echo "=========================================="
    echo "Enclii Production Health Check"
    echo "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
    echo "=========================================="

    for endpoint in "${ENDPOINTS[@]}"; do
        check_endpoint "$endpoint" || true
    done

    echo ""
    echo "=========================================="
    echo "Summary: ${#PASSED[@]} passed, ${#FAILED[@]} failed"
    echo "=========================================="

    if [[ ${#FAILED[@]} -gt 0 ]]; then
        # Real newlines, not literal backslash-n: the envelope builder escapes
        # them into the JSON body, so what arrives is a multi-line message
        # rather than the two characters `\n`.
        local alert_msg="PRODUCTION ALERT: ${#FAILED[@]} service(s) failing"
        for failure in "${FAILED[@]}"; do
            alert_msg+=$'\n'"- $failure"
        done
        # One idempotency key per hour per check name, so a persistent outage
        # messages once an hour instead of twelve times.
        send_alert "endpoints" "$alert_msg"
        exit 1
    fi

    echo -e "${GREEN}All services healthy${NC}"
    exit 0
}

main "$@"
