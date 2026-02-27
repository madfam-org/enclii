#!/bin/bash
set -euo pipefail

# =============================================================================
# Cloudflare Controller - Domain Provisioning Script
# Automates Zone, DNS, and Tunnel Ingress configuration
# =============================================================================
#
# Usage:
#   ./provision-domain.sh --domain example-app.dev --subdomain links \
#                         --service my-service --namespace my-app-production
#
# Credential Loading (in order of priority):
#   1. Environment variables (CLOUDFLARE_API_TOKEN, CLOUDFLARE_ACCOUNT_ID, TUNNEL_ID)
#   2. Local credential file (~/.enclii/credentials)
#   3. Kubernetes secret (enclii-cloudflare-credentials)
#
# Optional:
#   KUBECONFIG            - Path to kubeconfig (defaults to ~/.kube/config)
#   DRY_RUN               - Set to "true" to preview changes without applying
#
# NOTE: For kubectl-free zone creation, use cloudflare-zone-create.sh instead.
#
# =============================================================================

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
CLOUDFLARED_NAMESPACE="cloudflare-tunnel"
CLOUDFLARED_CONFIGMAP="cloudflared-config"
CLOUDFLARED_DEPLOYMENT="cloudflared"

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# =============================================================================
# Load Credential Library
# =============================================================================

if [ -f "$SCRIPT_DIR/lib/cloudflare-credentials.sh" ]; then
    source "$SCRIPT_DIR/lib/cloudflare-credentials.sh"
    CREDENTIALS_LIB_LOADED=true
else
    CREDENTIALS_LIB_LOADED=false
fi

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
        local errors=$(echo "$response" | jq -r '.errors[]?.message // "Unknown error"' 2>/dev/null)
        log_error "$context: $errors"
        return 1
    fi
}

usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Options:
  --domain DOMAIN         Root domain (e.g., example-app.dev)
  --subdomain SUBDOMAIN   Subdomain (e.g., links) - use "@" for apex
  --service SERVICE       K8s service name (e.g., my-service)
  --namespace NAMESPACE   K8s namespace (e.g., my-app-production)
  --port PORT             Service port (default: 80)
  --dry-run               Preview changes without applying
  --help                  Show this help message

Environment Variables:
  CLOUDFLARE_API_TOKEN    Cloudflare API token (required)
  CLOUDFLARE_ACCOUNT_ID   Cloudflare account ID (required)
  TUNNEL_ID               Cloudflare Tunnel UUID (required)

Example:
  export CLOUDFLARE_API_TOKEN="your-token"
  export CLOUDFLARE_ACCOUNT_ID="your-account-id"
  export TUNNEL_ID="your-tunnel-uuid"
  
  $0 --domain example-app.dev --subdomain links --service my-service --namespace my-app-production
EOF
    exit 1
}

# =============================================================================
# Parse Arguments
# =============================================================================

DOMAIN=""
SUBDOMAIN=""
SERVICE=""
NAMESPACE=""
SERVICE_PORT="80"

while [[ $# -gt 0 ]]; do
    case $1 in
        --domain) DOMAIN="$2"; shift 2 ;;
        --subdomain) SUBDOMAIN="$2"; shift 2 ;;
        --service) SERVICE="$2"; shift 2 ;;
        --namespace) NAMESPACE="$2"; shift 2 ;;
        --port) SERVICE_PORT="$2"; shift 2 ;;
        --dry-run) DRY_RUN="true"; shift ;;
        --help) usage ;;
        *) log_error "Unknown option: $1"; usage ;;
    esac
done

# Validate required arguments
if [ -z "$DOMAIN" ] || [ -z "$SUBDOMAIN" ] || [ -z "$SERVICE" ] || [ -z "$NAMESPACE" ]; then
    log_error "Missing required arguments"
    usage
fi

# Load and validate Cloudflare credentials
if [ "$CREDENTIALS_LIB_LOADED" = "true" ]; then
    log_info "Loading Cloudflare credentials..."
    if ! load_cloudflare_credentials; then
        log_error "Failed to load Cloudflare credentials"
        echo ""
        echo "Run 'scripts/cloudflare-zone-create.sh --setup' to configure credentials."
        echo "Or see 'docs/guides/CREDENTIAL_SETUP.md' for manual setup."
        exit 1
    fi
    if ! validate_cloudflare_credentials; then
        exit 1
    fi
else
    # Fallback: validate environment variables directly
    for var in CLOUDFLARE_API_TOKEN CLOUDFLARE_ACCOUNT_ID TUNNEL_ID; do
        if [ -z "${!var:-}" ]; then
            log_error "Environment variable $var is required"
            exit 1
        fi
    done
fi

# Check for required tools
for tool in curl jq kubectl yq; do
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

# Construct K8s service URL
K8S_SERVICE_URL="http://${SERVICE}.${NAMESPACE}.svc.cluster.local:${SERVICE_PORT}"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo -e "${CYAN}🌐 Cloudflare Domain Provisioning${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Configuration:"
echo "  Domain:     $DOMAIN"
echo "  Subdomain:  $SUBDOMAIN"
echo "  Hostname:   $FULL_HOSTNAME"
echo "  Service:    $K8S_SERVICE_URL"
echo "  Tunnel ID:  ${TUNNEL_ID:0:8}..."
echo "  Dry Run:    $DRY_RUN"
echo ""

if [ "$DRY_RUN" = "true" ]; then
    log_warn "DRY RUN MODE - No changes will be made"
    echo ""
fi

# =============================================================================
# Step 1: Zone Check/Create
# =============================================================================

log_step "Step 1: Checking Cloudflare Zone for $DOMAIN..."

# List zones to find existing
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
            echo -e "${YELLOW}MANUAL ACTION REQUIRED - PORKBUN NAMESERVERS${NC}"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""
            echo "Set these nameservers at Porkbun for $DOMAIN:"
            echo "$ZONES_RESPONSE" | jq -r '.result[0].name_servers[]' | while read ns; do
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
                ZONE_NS=$(echo "$CREATE_ZONE_RESPONSE" | jq -r '.result.name_servers | join(", ")')
                
                log_success "Zone created: $ZONE_ID"
                echo ""
                echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                echo -e "${YELLOW}🚨 MANUAL ACTION REQUIRED - PORKBUN NAMESERVERS${NC}"
                echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                echo ""
                echo "Set these nameservers at Porkbun for $DOMAIN:"
                echo "$CREATE_ZONE_RESPONSE" | jq -r '.result.name_servers[]' | while read ns; do
                    echo "  → $ns"
                done
                echo ""
                echo "Steps:"
                echo "  1. Log into Porkbun: https://porkbun.com/account/domains"
                echo "  2. Click on $DOMAIN → DNS"
                echo "  3. Click 'Edit Nameservers'"
                echo "  4. Replace with Cloudflare nameservers above"
                echo "  5. Save and wait for propagation (up to 24-48h)"
                echo ""
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
                \"comment\": \"Enclii managed - $SERVICE in $NAMESPACE\"
            }")
            
            if check_cf_response "$CREATE_DNS_RESPONSE" "DNS creation"; then
                log_success "DNS record created: $FULL_HOSTNAME -> $TUNNEL_CNAME (proxied)"
            else
                exit 1
            fi
        fi
    fi
fi

echo ""

# =============================================================================
# Step 3: Cloudflared ConfigMap Update
# =============================================================================

log_step "Step 3: Updating Cloudflared ingress configuration..."

# Fetch current ConfigMap
log_info "Fetching current ConfigMap..."
CURRENT_CONFIG=$(kubectl get configmap "$CLOUDFLARED_CONFIGMAP" -n "$CLOUDFLARED_NAMESPACE" -o json)

if [ $? -ne 0 ]; then
    log_error "Failed to fetch ConfigMap. Is kubectl configured correctly?"
    exit 1
fi

# Extract current config.yaml content
CURRENT_YAML=$(echo "$CURRENT_CONFIG" | jq -r '.data["config.yaml"]')

# Check if hostname already exists
if echo "$CURRENT_YAML" | grep -q "hostname: $FULL_HOSTNAME"; then
    log_warn "Ingress rule for $FULL_HOSTNAME already exists in ConfigMap"
    log_info "Skipping ConfigMap update"
else
    # Build the new ingress rule
    NEW_RULE="      # ============================================
      # Client Services: $NAMESPACE
      # ============================================

      # $SERVICE ($FULL_HOSTNAME)
      - hostname: $FULL_HOSTNAME
        service: $K8S_SERVICE_URL
        originRequest:
          connectTimeout: 10s
          httpHostHeader: $FULL_HOSTNAME"

    # Find the catch-all rule and insert before it
    # The catch-all is "- service: http_status:404"
    
    if [ "$DRY_RUN" = "true" ]; then
        log_info "[DRY RUN] Would add ingress rule:"
        echo "$NEW_RULE" | sed 's/^/         /'
    else
        # Use sed to insert the new rule before the catch-all
        UPDATED_YAML=$(echo "$CURRENT_YAML" | sed "/^      - service: http_status:404$/i\\
$NEW_RULE
")
        
        # Create the patch JSON
        PATCH_JSON=$(jq -n --arg yaml "$UPDATED_YAML" '{
            "data": {
                "config.yaml": $yaml
            }
        }')
        
        # Apply the patch
        echo "$PATCH_JSON" | kubectl patch configmap "$CLOUDFLARED_CONFIGMAP" \
            -n "$CLOUDFLARED_NAMESPACE" \
            --type merge \
            --patch-file /dev/stdin
        
        if [ $? -eq 0 ]; then
            log_success "ConfigMap updated with new ingress rule"
        else
            log_error "Failed to update ConfigMap"
            exit 1
        fi
    fi
fi

echo ""

# =============================================================================
# Step 4: Restart Cloudflared Deployment
# =============================================================================

log_step "Step 4: Restarting Cloudflared deployment to apply changes..."

if [ "$DRY_RUN" = "true" ]; then
    log_info "[DRY RUN] Would restart deployment $CLOUDFLARED_DEPLOYMENT"
else
    # Trigger rolling restart by updating an annotation
    kubectl rollout restart deployment "$CLOUDFLARED_DEPLOYMENT" -n "$CLOUDFLARED_NAMESPACE"
    
    if [ $? -eq 0 ]; then
        log_success "Cloudflared restart initiated"
        
        # Wait for rollout
        log_info "Waiting for rollout to complete..."
        kubectl rollout status deployment "$CLOUDFLARED_DEPLOYMENT" -n "$CLOUDFLARED_NAMESPACE" --timeout=120s
        
        if [ $? -eq 0 ]; then
            log_success "Cloudflared rollout complete"
        else
            log_warn "Rollout taking longer than expected. Check with: kubectl get pods -n $CLOUDFLARED_NAMESPACE"
        fi
    else
        log_error "Failed to restart deployment"
        exit 1
    fi
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo -e "${GREEN}✅ Domain Provisioning Complete${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Domain:    https://$FULL_HOSTNAME"
echo "Routes to: $K8S_SERVICE_URL"
echo ""
echo "Verify with:"
echo "  curl -I https://$FULL_HOSTNAME"
echo "  kubectl logs -n $CLOUDFLARED_NAMESPACE -l app=cloudflared --tail=20"
echo ""
