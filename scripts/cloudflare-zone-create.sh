#!/bin/bash
set -euo pipefail

# =============================================================================
# Cloudflare Zone Creation Script (kubectl-free)
# =============================================================================
#
# Creates Cloudflare zones and DNS records WITHOUT requiring kubectl access.
# Use this when you only need DNS setup, not tunnel routing configuration.
#
# For full tunnel routing, use provision-domain.sh (requires kubectl).
#
# Usage:
#   ./cloudflare-zone-create.sh --domain example-app.dev --subdomain links
#
# Credential Loading (in order of priority):
#   1. Environment variables (CLOUDFLARE_API_TOKEN, CLOUDFLARE_ACCOUNT_ID, TUNNEL_ID)
#   2. Local credential file (~/.enclii/credentials)
#
# =============================================================================

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Cloudflare API base
CF_API="https://api.cloudflare.com/client/v4"

# Defaults
DRY_RUN="${DRY_RUN:-false}"
SKIP_DNS="${SKIP_DNS:-false}"

# =============================================================================
# Helper Functions
# =============================================================================

log_info() { echo -e "${BLUE}ℹ${NC} $1"; }
log_success() { echo -e "${GREEN}✅${NC} $1"; }
log_warn() { echo -e "${YELLOW}⚠️${NC} $1"; }
log_error() { echo -e "${RED}❌${NC} $1"; }
log_step() { echo -e "${CYAN}▶${NC} $1"; }

cf_api() {
    local method="$1"
    local endpoint="$2"
    local data="${3:-}"

    local args=(-s -X "$method" \
        -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
        -H "Content-Type: application/json")

    if [ -n "$data" ]; then
        args+=(-d "$data")
    fi

    curl "${args[@]}" "$CF_API$endpoint"
}

check_cf_response() {
    local response="$1"
    local context="$2"

    if echo "$response" | jq -e '.success == true' >/dev/null 2>&1; then
        return 0
    else
        local errors
        errors=$(echo "$response" | jq -r '.errors[]?.message // "Unknown error"' 2>/dev/null)
        log_error "$context: $errors"
        return 1
    fi
}

usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Creates Cloudflare zone and DNS records (NO kubectl required).

Options:
  --domain DOMAIN         Root domain (e.g., example-app.dev)
  --subdomain SUBDOMAIN   Subdomain (e.g., links) - use "@" for apex
  --skip-dns              Only create zone, skip DNS record creation
  --dry-run               Preview changes without applying
  --status                Show current credential status
  --setup                 Interactive credential setup
  --test                  Test API credentials
  --help                  Show this help message

Examples:
  # Create zone and DNS record
  $0 --domain example-app.dev --subdomain links

  # Create zone only
  $0 --domain example-app.dev --subdomain @ --skip-dns

  # Test credentials
  $0 --test

  # Setup credentials interactively
  $0 --setup

Credential Sources (checked in order):
  1. Environment variables
  2. ~/.enclii/credentials file

Required API Token Permissions:
  - Zone:Read (list zones)
  - Zone:Edit (create zones)
  - DNS:Edit (create DNS records)
EOF
    exit 1
}

# =============================================================================
# Load Credential Library
# =============================================================================

if [ -f "$SCRIPT_DIR/lib/cloudflare-credentials.sh" ]; then
    source "$SCRIPT_DIR/lib/cloudflare-credentials.sh"
else
    log_error "Credential library not found: $SCRIPT_DIR/lib/cloudflare-credentials.sh"
    exit 1
fi

# =============================================================================
# Parse Arguments
# =============================================================================

DOMAIN=""
SUBDOMAIN=""
ACTION="provision"

while [[ $# -gt 0 ]]; do
    case $1 in
        --domain) DOMAIN="$2"; shift 2 ;;
        --subdomain) SUBDOMAIN="$2"; shift 2 ;;
        --skip-dns) SKIP_DNS="true"; shift ;;
        --dry-run) DRY_RUN="true"; shift ;;
        --status) ACTION="status"; shift ;;
        --setup) ACTION="setup"; shift ;;
        --test) ACTION="test"; shift ;;
        --help) usage ;;
        *) log_error "Unknown option: $1"; usage ;;
    esac
done

# =============================================================================
# Handle Non-Provision Actions
# =============================================================================

case "$ACTION" in
    status)
        load_cloudflare_credentials 2>/dev/null || true
        show_credential_status
        exit 0
        ;;
    setup)
        create_credential_file
        exit $?
        ;;
    test)
        if ! load_cloudflare_credentials; then
            log_error "Failed to load credentials"
            exit 1
        fi
        test_cloudflare_credentials
        exit $?
        ;;
esac

# =============================================================================
# Validate Arguments
# =============================================================================

if [ -z "$DOMAIN" ]; then
    log_error "Missing required argument: --domain"
    usage
fi

if [ -z "$SUBDOMAIN" ]; then
    log_error "Missing required argument: --subdomain"
    usage
fi

# =============================================================================
# Load and Validate Credentials
# =============================================================================

log_info "Loading Cloudflare credentials..."

if ! load_cloudflare_credentials; then
    log_error "Failed to load Cloudflare credentials"
    echo ""
    echo "Run '$0 --setup' to configure credentials interactively."
    echo "Or see 'docs/guides/CREDENTIAL_SETUP.md' for manual setup."
    exit 1
fi

if ! validate_cloudflare_credentials; then
    exit 1
fi

# Check for required tools
for tool in curl jq; do
    if ! command -v "$tool" &> /dev/null; then
        log_error "$tool is required but not installed"
        exit 1
    fi
done

# Construct full hostname
if [ "$SUBDOMAIN" = "@" ]; then
    FULL_HOSTNAME="$DOMAIN"
else
    FULL_HOSTNAME="${SUBDOMAIN}.${DOMAIN}"
fi

# =============================================================================
# Main Execution
# =============================================================================

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo -e "${CYAN}🌐 Cloudflare Zone Creation (kubectl-free)${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Configuration:"
echo "  Domain:     $DOMAIN"
echo "  Subdomain:  $SUBDOMAIN"
echo "  Hostname:   $FULL_HOSTNAME"
echo "  Account:    ${CLOUDFLARE_ACCOUNT_ID:0:8}..."
echo "  Tunnel:     ${TUNNEL_ID:0:8}..."
echo "  Dry Run:    $DRY_RUN"
echo "  Skip DNS:   $SKIP_DNS"
echo ""

if [ "$DRY_RUN" = "true" ]; then
    log_warn "DRY RUN MODE - No changes will be made"
    echo ""
fi

# =============================================================================
# Step 1: Zone Check/Create
# =============================================================================

log_step "Step 1: Checking Cloudflare Zone for $DOMAIN..."

ZONES_RESPONSE=$(cf_api GET "/zones?name=$DOMAIN&account.id=$CLOUDFLARE_ACCOUNT_ID")

if check_cf_response "$ZONES_RESPONSE" "Zone lookup"; then
    ZONE_COUNT=$(echo "$ZONES_RESPONSE" | jq '.result | length')

    if [ "$ZONE_COUNT" -gt 0 ]; then
        ZONE_ID=$(echo "$ZONES_RESPONSE" | jq -r '.result[0].id')
        ZONE_STATUS=$(echo "$ZONES_RESPONSE" | jq -r '.result[0].status')
        ZONE_NS=$(echo "$ZONES_RESPONSE" | jq -r '.result[0].name_servers | join(", ")')

        log_success "Zone exists: $ZONE_ID (status: $ZONE_STATUS)"
        echo "         Nameservers: $ZONE_NS"

        if [ "$ZONE_STATUS" != "active" ]; then
            log_warn "Zone is not active. Please update nameservers at your registrar."
            echo ""
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo -e "${YELLOW}MANUAL ACTION REQUIRED - UPDATE NAMESERVERS${NC}"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""
            echo "Set these nameservers at your domain registrar for $DOMAIN:"
            echo "$ZONES_RESPONSE" | jq -r '.result[0].name_servers[]' | while read -r ns; do
                echo "  → $ns"
            done
            echo ""
            echo "After setting nameservers, wait for propagation (up to 24-48 hours)"
            echo "Check status: dig NS $DOMAIN"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        fi
    else
        # Zone doesn't exist - create it
        log_info "Zone does not exist. Creating..."

        if [ "$DRY_RUN" = "true" ]; then
            log_info "[DRY RUN] Would create zone for $DOMAIN"
            ZONE_ID="dry-run-zone-id"
        else
            CREATE_ZONE_RESPONSE=$(cf_api POST "/zones" "{
                \"name\": \"$DOMAIN\",
                \"account\": {\"id\": \"$CLOUDFLARE_ACCOUNT_ID\"},
                \"jump_start\": false,
                \"type\": \"full\"
            }")

            if check_cf_response "$CREATE_ZONE_RESPONSE" "Zone creation"; then
                ZONE_ID=$(echo "$CREATE_ZONE_RESPONSE" | jq -r '.result.id')

                log_success "Zone created: $ZONE_ID"
                echo ""
                echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                echo -e "${YELLOW}🚨 MANUAL ACTION REQUIRED - UPDATE NAMESERVERS${NC}"
                echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                echo ""
                echo "Set these nameservers at your domain registrar for $DOMAIN:"
                echo "$CREATE_ZONE_RESPONSE" | jq -r '.result.name_servers[]' | while read -r ns; do
                    echo "  → $ns"
                done
                echo ""
                echo "Steps for common registrars:"
                echo "  Porkbun: https://porkbun.com/account/domains → $DOMAIN → Edit NS"
                echo "  GoDaddy: My Products → DNS → Nameservers → Change"
                echo "  Namecheap: Domain List → Manage → Nameservers → Custom DNS"
                echo ""
                echo "After setting nameservers, wait for propagation (up to 24-48 hours)"
                echo "Check status: dig NS $DOMAIN"
                echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            else
                exit 1
            fi
        fi
    fi
else
    exit 1
fi

echo ""

# =============================================================================
# Step 2: DNS Record Creation
# =============================================================================

if [ "$SKIP_DNS" = "true" ]; then
    log_info "Skipping DNS record creation (--skip-dns)"
else
    log_step "Step 2: Creating DNS CNAME record for $FULL_HOSTNAME..."

    # Tunnel CNAME target
    TUNNEL_CNAME="${TUNNEL_ID}.cfargotunnel.com"

    # Check if record already exists
    DNS_NAME=$([ "$SUBDOMAIN" = "@" ] && echo "$DOMAIN" || echo "$SUBDOMAIN")
    EXISTING_DNS=$(cf_api GET "/zones/$ZONE_ID/dns_records?type=CNAME&name=$FULL_HOSTNAME")

    if check_cf_response "$EXISTING_DNS" "DNS lookup"; then
        EXISTING_COUNT=$(echo "$EXISTING_DNS" | jq '.result | length')

        if [ "$EXISTING_COUNT" -gt 0 ]; then
            EXISTING_RECORD_ID=$(echo "$EXISTING_DNS" | jq -r '.result[0].id')
            EXISTING_CONTENT=$(echo "$EXISTING_DNS" | jq -r '.result[0].content')

            if [ "$EXISTING_CONTENT" = "$TUNNEL_CNAME" ]; then
                log_success "DNS record already exists and is correct"
            else
                log_warn "DNS record exists but points to $EXISTING_CONTENT"
                log_info "Updating to point to tunnel..."

                if [ "$DRY_RUN" = "true" ]; then
                    log_info "[DRY RUN] Would update DNS record $EXISTING_RECORD_ID"
                else
                    UPDATE_DNS_RESPONSE=$(cf_api PATCH "/zones/$ZONE_ID/dns_records/$EXISTING_RECORD_ID" "{
                        \"content\": \"$TUNNEL_CNAME\",
                        \"proxied\": true
                    }")

                    if check_cf_response "$UPDATE_DNS_RESPONSE" "DNS update"; then
                        log_success "DNS record updated"
                    else
                        exit 1
                    fi
                fi
            fi
        else
            # Create new record
            if [ "$DRY_RUN" = "true" ]; then
                log_info "[DRY RUN] Would create CNAME: $FULL_HOSTNAME -> $TUNNEL_CNAME"
            else
                CREATE_DNS_RESPONSE=$(cf_api POST "/zones/$ZONE_ID/dns_records" "{
                    \"type\": \"CNAME\",
                    \"name\": \"$DNS_NAME\",
                    \"content\": \"$TUNNEL_CNAME\",
                    \"proxied\": true,
                    \"ttl\": 1,
                    \"comment\": \"Enclii managed - kubectl-free provisioning\"
                }")

                if check_cf_response "$CREATE_DNS_RESPONSE" "DNS creation"; then
                    log_success "DNS record created: $FULL_HOSTNAME -> $TUNNEL_CNAME (proxied)"
                else
                    exit 1
                fi
            fi
        fi
    fi
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo -e "${GREEN}✅ Zone Creation Complete${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Created:"
echo "  Zone:     $DOMAIN (ID: ${ZONE_ID:0:12}...)"
if [ "$SKIP_DNS" != "true" ]; then
    echo "  DNS:      $FULL_HOSTNAME -> ${TUNNEL_ID}.cfargotunnel.com"
fi
echo ""
echo "Next Steps:"
echo "  1. Update nameservers at your domain registrar (if zone was new)"
echo "  2. Wait for DNS propagation (check: dig $FULL_HOSTNAME)"
if [ "$SKIP_DNS" != "true" ]; then
    echo "  3. Configure tunnel routing with: provision-domain.sh (requires kubectl)"
fi
echo ""
echo "Verify with:"
echo "  dig CNAME $FULL_HOSTNAME"
echo "  curl -I https://$FULL_HOSTNAME (after propagation + tunnel config)"
echo ""
