#!/bin/bash
# =============================================================================
# Cloudflare Zone Settings Manager
# =============================================================================
#
# Manage zone-level settings across all Cloudflare zones programmatically.
#
# Usage:
#   ./scripts/cloudflare-zone-settings.sh <command> [options]
#
# Commands:
#   list                    List all zones and their IDs
#   get <setting>           Get a setting value across all zones
#   set <setting> <value>   Set a setting value across all zones
#   audit                   Audit security-critical settings across all zones
#
# Common settings:
#   always_use_https        Redirect HTTP to HTTPS (on/off)
#   min_tls_version         Minimum TLS version (1.0/1.1/1.2/1.3)
#   ssl                     SSL mode (off/flexible/full/strict)
#   security_level          Security level (off/low/medium/high/under_attack)
#   browser_check           Browser integrity check (on/off)
#
# Examples:
#   ./scripts/cloudflare-zone-settings.sh list
#   ./scripts/cloudflare-zone-settings.sh get always_use_https
#   ./scripts/cloudflare-zone-settings.sh set always_use_https on
#   ./scripts/cloudflare-zone-settings.sh set min_tls_version 1.2
#   ./scripts/cloudflare-zone-settings.sh audit
#
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Load credential library
source "${SCRIPT_DIR}/lib/cloudflare-credentials.sh"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()    { echo -e "${BLUE}[zone-settings]${NC} $1"; }
log_success() { echo -e "${GREEN}[zone-settings]${NC} $1"; }
log_warn()    { echo -e "${YELLOW}[zone-settings]${NC} $1"; }
log_error()   { echo -e "${RED}[zone-settings]${NC} $1"; }

# =============================================================================
# API helpers
# =============================================================================

cf_api() {
    local method="$1"
    local endpoint="$2"
    local data="${3:-}"

    local args=(
        -s -X "$method"
        "https://api.cloudflare.com/client/v4${endpoint}"
        -H "Authorization: Bearer ${CLOUDFLARE_API_TOKEN}"
        -H "Content-Type: application/json"
    )

    if [ -n "$data" ]; then
        args+=(--data "$data")
    fi

    curl "${args[@]}"
}

get_all_zones() {
    local page=1
    local all_zones="[]"

    while true; do
        local response
        response=$(cf_api GET "/zones?per_page=50&page=${page}")

        local success
        success=$(echo "$response" | python3 -c "import sys,json; print(json.load(sys.stdin).get('success', False))" 2>/dev/null)

        if [ "$success" != "True" ]; then
            log_error "Failed to fetch zones (page $page)"
            echo "$response" | python3 -m json.tool 2>/dev/null || echo "$response"
            return 1
        fi

        local page_zones
        page_zones=$(echo "$response" | python3 -c "
import sys, json
data = json.load(sys.stdin)
print(json.dumps(data.get('result', [])))
" 2>/dev/null)

        local count
        count=$(echo "$page_zones" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null)

        if [ "$count" = "0" ]; then
            break
        fi

        all_zones=$(python3 -c "
import sys, json
a = json.loads('$all_zones')
b = json.loads(sys.stdin.read())
print(json.dumps(a + b))
" <<< "$page_zones")

        local total_pages
        total_pages=$(echo "$response" | python3 -c "import sys,json; print(json.load(sys.stdin).get('result_info', {}).get('total_pages', 1))" 2>/dev/null)

        if [ "$page" -ge "$total_pages" ]; then
            break
        fi

        page=$((page + 1))
    done

    echo "$all_zones"
}

# =============================================================================
# Commands
# =============================================================================

cmd_list() {
    log_info "Fetching all zones..."
    local zones
    zones=$(get_all_zones) || return 1

    echo ""
    printf "%-20s %-36s %s\n" "Domain" "Zone ID" "Status"
    printf "%-20s %-36s %s\n" "------" "-------" "------"

    echo "$zones" | python3 -c "
import sys, json
zones = json.load(sys.stdin)
for z in sorted(zones, key=lambda x: x['name']):
    print(f\"{z['name']:<20s} {z['id']:<36s} {z['status']}\")
" 2>/dev/null
    echo ""
}

cmd_get() {
    local setting="$1"

    if [ -z "$setting" ]; then
        log_error "Usage: $0 get <setting>"
        return 1
    fi

    log_info "Fetching '${setting}' across all zones..."
    local zones
    zones=$(get_all_zones) || return 1

    echo ""
    printf "%-20s %s\n" "Domain" "Value"
    printf "%-20s %s\n" "------" "-----"

    echo "$zones" | python3 -c "
import sys, json
zones = json.load(sys.stdin)
for z in sorted(zones, key=lambda x: x['name']):
    print(z['id'] + ' ' + z['name'])
" 2>/dev/null | while read -r zone_id domain; do
        local response
        response=$(cf_api GET "/zones/${zone_id}/settings/${setting}")
        local value
        value=$(echo "$response" | python3 -c "import sys,json; print(json.load(sys.stdin).get('result', {}).get('value', 'N/A'))" 2>/dev/null)
        printf "%-20s %s\n" "$domain" "$value"
    done
    echo ""
}

cmd_set() {
    local setting="$1"
    local value="$2"

    if [ -z "$setting" ] || [ -z "$value" ]; then
        log_error "Usage: $0 set <setting> <value>"
        return 1
    fi

    log_info "Setting '${setting}' = '${value}' across all zones..."
    local zones
    zones=$(get_all_zones) || return 1

    local success_count=0
    local fail_count=0

    echo ""
    echo "$zones" | python3 -c "
import sys, json
zones = json.load(sys.stdin)
for z in sorted(zones, key=lambda x: x['name']):
    print(z['id'] + ' ' + z['name'])
" 2>/dev/null | while read -r zone_id domain; do
        local response
        response=$(cf_api PATCH "/zones/${zone_id}/settings/${setting}" "{\"value\":\"${value}\"}")
        local api_success
        api_success=$(echo "$response" | python3 -c "import sys,json; print(json.load(sys.stdin).get('success', False))" 2>/dev/null)

        if [ "$api_success" = "True" ]; then
            log_success "${domain}: ${setting} = ${value}"
        else
            local error_msg
            error_msg=$(echo "$response" | python3 -c "import sys,json; errs=json.load(sys.stdin).get('errors',[]); print(errs[0].get('message','unknown') if errs else 'unknown')" 2>/dev/null)
            log_error "${domain}: FAILED — ${error_msg}"
        fi
    done
    echo ""
}

cmd_audit() {
    log_info "Auditing security settings across all zones..."
    local zones
    zones=$(get_all_zones) || return 1

    local settings=("always_use_https" "min_tls_version" "ssl" "security_level" "browser_check")

    echo ""
    printf "%-20s" "Domain"
    for s in "${settings[@]}"; do
        printf " %-18s" "$s"
    done
    echo ""
    printf "%-20s" "------"
    for s in "${settings[@]}"; do
        printf " %-18s" "$(printf '%0.s-' $(seq 1 ${#s}))"
    done
    echo ""

    echo "$zones" | python3 -c "
import sys, json
zones = json.load(sys.stdin)
for z in sorted(zones, key=lambda x: x['name']):
    print(z['id'] + ' ' + z['name'])
" 2>/dev/null | while read -r zone_id domain; do
        printf "%-20s" "$domain"
        for setting in "${settings[@]}"; do
            local response
            response=$(cf_api GET "/zones/${zone_id}/settings/${setting}")
            local value
            value=$(echo "$response" | python3 -c "import sys,json; print(json.load(sys.stdin).get('result', {}).get('value', 'N/A'))" 2>/dev/null)

            # Color code based on expected values
            case "${setting}:${value}" in
                always_use_https:on|min_tls_version:1.2|min_tls_version:1.3|ssl:full|ssl:strict|browser_check:on)
                    printf " ${GREEN}%-18s${NC}" "$value"
                    ;;
                always_use_https:off|min_tls_version:1.0|ssl:off|ssl:flexible|browser_check:off)
                    printf " ${RED}%-18s${NC}" "$value"
                    ;;
                *)
                    printf " ${YELLOW}%-18s${NC}" "$value"
                    ;;
            esac
        done
        echo ""
    done

    echo ""
    echo "Legend: ${GREEN}green${NC}=secure  ${YELLOW}yellow${NC}=review  ${RED}red${NC}=action needed"
    echo ""
}

# =============================================================================
# Main
# =============================================================================

usage() {
    echo "Usage: $0 <command> [options]"
    echo ""
    echo "Commands:"
    echo "  list                    List all zones and their IDs"
    echo "  get <setting>           Get a setting value across all zones"
    echo "  set <setting> <value>   Set a setting value across all zones"
    echo "  audit                   Audit security-critical settings"
    echo ""
    echo "Common settings: always_use_https, min_tls_version, ssl, security_level, browser_check"
    echo ""
    echo "Examples:"
    echo "  $0 set always_use_https on"
    echo "  $0 set min_tls_version 1.2"
    echo "  $0 audit"
}

main() {
    local command="${1:-}"

    if [ -z "$command" ]; then
        usage
        exit 1
    fi

    # Load and validate credentials
    load_cloudflare_credentials || {
        log_error "Failed to load Cloudflare credentials"
        exit 1
    }
    validate_cloudflare_credentials || exit 1

    case "$command" in
        list)
            cmd_list
            ;;
        get)
            cmd_get "${2:-}"
            ;;
        set)
            cmd_set "${2:-}" "${3:-}"
            ;;
        audit)
            cmd_audit
            ;;
        -h|--help|help)
            usage
            ;;
        *)
            log_error "Unknown command: $command"
            usage
            exit 1
            ;;
    esac
}

main "$@"
