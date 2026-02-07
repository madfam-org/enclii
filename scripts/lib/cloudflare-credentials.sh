#!/bin/bash
# =============================================================================
# Cloudflare Credential Loading Library
# =============================================================================
#
# Provides secure credential loading from multiple sources:
#   1. Environment variables (highest priority)
#   2. Local credential file (~/.enclii/credentials)
#   3. Kubernetes secret (enclii-cloudflare-credentials)
#
# Usage:
#   source scripts/lib/cloudflare-credentials.sh
#   load_cloudflare_credentials
#   validate_cloudflare_credentials
#
# =============================================================================

# Credential file location
ENCLII_CREDENTIALS_FILE="${ENCLII_CREDENTIALS_FILE:-$HOME/.enclii/credentials}"

# Required credential variables
CLOUDFLARE_CREDENTIAL_VARS=(
    "CLOUDFLARE_API_TOKEN"
    "CLOUDFLARE_ACCOUNT_ID"
    "TUNNEL_ID"
)

# Colors for output
_CF_RED='\033[0;31m'
_CF_GREEN='\033[0;32m'
_CF_YELLOW='\033[1;33m'
_CF_BLUE='\033[0;34m'
_CF_NC='\033[0m'

_cf_log_info() { echo -e "${_CF_BLUE}[credentials]${_CF_NC} $1"; }
_cf_log_success() { echo -e "${_CF_GREEN}[credentials]${_CF_NC} $1"; }
_cf_log_warn() { echo -e "${_CF_YELLOW}[credentials]${_CF_NC} $1"; }
_cf_log_error() { echo -e "${_CF_RED}[credentials]${_CF_NC} $1"; }

# =============================================================================
# load_from_env - Check if credentials are already in environment
# =============================================================================
load_from_env() {
    local all_present=true

    for var in "${CLOUDFLARE_CREDENTIAL_VARS[@]}"; do
        if [ -z "${!var:-}" ]; then
            all_present=false
            break
        fi
    done

    if [ "$all_present" = "true" ]; then
        _cf_log_info "Credentials loaded from environment variables"
        return 0
    fi

    return 1
}

# =============================================================================
# load_from_file - Load credentials from ~/.enclii/credentials
# =============================================================================
load_from_file() {
    if [ ! -f "$ENCLII_CREDENTIALS_FILE" ]; then
        return 1
    fi

    _cf_log_info "Loading credentials from $ENCLII_CREDENTIALS_FILE"

    local in_cloudflare_section=false

    while IFS= read -r line || [ -n "$line" ]; do
        # Skip empty lines and comments
        [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue

        # Check for section headers
        if [[ "$line" =~ ^\[([^\]]+)\] ]]; then
            section="${BASH_REMATCH[1]}"
            if [ "$section" = "cloudflare" ]; then
                in_cloudflare_section=true
            else
                in_cloudflare_section=false
            fi
            continue
        fi

        # Parse key=value in cloudflare section
        if [ "$in_cloudflare_section" = "true" ]; then
            if [[ "$line" =~ ^([^=]+)=(.*)$ ]]; then
                key=$(echo "${BASH_REMATCH[1]}" | tr -d ' ')
                value=$(echo "${BASH_REMATCH[2]}" | tr -d ' ' | tr -d '"' | tr -d "'")

                case "$key" in
                    api_token|api-token)
                        export CLOUDFLARE_API_TOKEN="$value"
                        ;;
                    account_id|account-id)
                        export CLOUDFLARE_ACCOUNT_ID="$value"
                        ;;
                    tunnel_id|tunnel-id)
                        export TUNNEL_ID="$value"
                        ;;
                esac
            fi
        fi
    done < "$ENCLII_CREDENTIALS_FILE"

    return 0
}

# =============================================================================
# load_from_kubernetes - Load credentials from K8s secret
# =============================================================================
load_from_kubernetes() {
    local secret_name="${CLOUDFLARE_SECRET_NAME:-enclii-cloudflare-credentials}"
    local secret_namespace="${CLOUDFLARE_SECRET_NAMESPACE:-enclii}"

    # Check if kubectl is available
    if ! command -v kubectl &> /dev/null; then
        return 1
    fi

    # Check if we can connect to the cluster
    if ! kubectl cluster-info &> /dev/null 2>&1; then
        return 1
    fi

    # Check if secret exists
    if ! kubectl get secret "$secret_name" -n "$secret_namespace" &> /dev/null 2>&1; then
        return 1
    fi

    _cf_log_info "Loading credentials from Kubernetes secret $secret_namespace/$secret_name"

    # Load each credential
    local api_token
    api_token=$(kubectl get secret "$secret_name" -n "$secret_namespace" \
        -o jsonpath='{.data.api-token}' 2>/dev/null | base64 -d 2>/dev/null)
    [ -n "$api_token" ] && export CLOUDFLARE_API_TOKEN="$api_token"

    local account_id
    account_id=$(kubectl get secret "$secret_name" -n "$secret_namespace" \
        -o jsonpath='{.data.account-id}' 2>/dev/null | base64 -d 2>/dev/null)
    [ -n "$account_id" ] && export CLOUDFLARE_ACCOUNT_ID="$account_id"

    local tunnel_id
    tunnel_id=$(kubectl get secret "$secret_name" -n "$secret_namespace" \
        -o jsonpath='{.data.tunnel-id}' 2>/dev/null | base64 -d 2>/dev/null)
    [ -n "$tunnel_id" ] && export TUNNEL_ID="$tunnel_id"

    return 0
}

# =============================================================================
# load_cloudflare_credentials - Main credential loading function
# =============================================================================
# Tries sources in order: env → file → kubernetes
# =============================================================================
load_cloudflare_credentials() {
    # Try environment variables first
    if load_from_env; then
        return 0
    fi

    # Try local credential file
    if load_from_file; then
        # Verify all variables are now set
        if load_from_env; then
            return 0
        fi
    fi

    # Try Kubernetes secret
    if load_from_kubernetes; then
        if load_from_env; then
            return 0
        fi
    fi

    return 1
}

# =============================================================================
# validate_cloudflare_credentials - Check all required credentials are present
# =============================================================================
validate_cloudflare_credentials() {
    local missing=()

    for var in "${CLOUDFLARE_CREDENTIAL_VARS[@]}"; do
        if [ -z "${!var:-}" ]; then
            missing+=("$var")
        fi
    done

    if [ ${#missing[@]} -gt 0 ]; then
        _cf_log_error "Missing Cloudflare credentials:"
        for var in "${missing[@]}"; do
            echo "  - $var"
        done
        echo ""
        echo "To configure credentials, either:"
        echo ""
        echo "1. Set environment variables:"
        echo "   export CLOUDFLARE_API_TOKEN='your-token'"
        echo "   export CLOUDFLARE_ACCOUNT_ID='your-account-id'"
        echo "   export TUNNEL_ID='your-tunnel-id'"
        echo ""
        echo "2. Create ~/.enclii/credentials:"
        echo "   [cloudflare]"
        echo "   api_token = your-token"
        echo "   account_id = your-account-id"
        echo "   tunnel_id = your-tunnel-id"
        echo ""
        echo "See docs/guides/CREDENTIAL_SETUP.md for details."
        return 1
    fi

    _cf_log_success "All Cloudflare credentials validated"
    return 0
}

# =============================================================================
# test_cloudflare_credentials - Test credentials against Cloudflare API
# =============================================================================
# Uses zones endpoint instead of /user/tokens/verify because account-level
# tokens (used for domain provisioning) don't work with the user tokens API.
# =============================================================================
test_cloudflare_credentials() {
    _cf_log_info "Testing Cloudflare API credentials..."

    # Test 1: Zone access (validates Zone:Read permission)
    local zones_response
    zones_response=$(curl -sf -X GET "https://api.cloudflare.com/client/v4/zones?per_page=1" \
        -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
        -H "Content-Type: application/json" 2>&1)

    if ! echo "$zones_response" | jq -e '.success == true' >/dev/null 2>&1; then
        local error_msg
        error_msg=$(echo "$zones_response" | jq -r '.errors[0].message // "Zone access failed"' 2>/dev/null)
        _cf_log_error "API token verification failed: $error_msg"
        return 1
    fi

    local zone_count
    zone_count=$(echo "$zones_response" | jq -r '.result_info.total_count // 0')
    _cf_log_success "Zone access verified ($zone_count zones accessible)"

    # Test 2: Tunnel access (validates Cloudflare Tunnel:Read permission)
    if [ -n "${CLOUDFLARE_ACCOUNT_ID:-}" ] && [ -n "${TUNNEL_ID:-}" ]; then
        local tunnel_response
        tunnel_response=$(curl -sf -X GET "https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_ACCOUNT_ID/tunnels/$TUNNEL_ID" \
            -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
            -H "Content-Type: application/json" 2>&1)

        if echo "$tunnel_response" | jq -e '.success == true' >/dev/null 2>&1; then
            local tunnel_name tunnel_status
            tunnel_name=$(echo "$tunnel_response" | jq -r '.result.name // "unknown"')
            tunnel_status=$(echo "$tunnel_response" | jq -r '.result.status // "unknown"')
            _cf_log_success "Tunnel access verified ($tunnel_name: $tunnel_status)"
        else
            _cf_log_warn "Tunnel access failed (token may not have Tunnel permissions)"
        fi
    fi

    # Show accessible zones if verbose
    if [ "${VERBOSE:-false}" = "true" ]; then
        echo ""
        echo "Accessible zones:"
        curl -sf -X GET "https://api.cloudflare.com/client/v4/zones?per_page=10" \
            -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
            -H "Content-Type: application/json" 2>/dev/null | \
            jq -r '.result[] | "  - \(.name) (\(.status))"' 2>/dev/null
    fi

    return 0
}

# =============================================================================
# show_credential_status - Display current credential configuration
# =============================================================================
show_credential_status() {
    echo ""
    echo "Cloudflare Credential Status"
    echo "============================"
    echo ""

    for var in "${CLOUDFLARE_CREDENTIAL_VARS[@]}"; do
        local value="${!var:-}"
        if [ -n "$value" ]; then
            # Mask the value for security
            local masked
            if [ ${#value} -gt 8 ]; then
                masked="${value:0:4}...${value: -4}"
            else
                masked="****"
            fi
            echo "  $var: $masked"
        else
            echo "  $var: (not set)"
        fi
    done

    echo ""
    echo "Credential file: $ENCLII_CREDENTIALS_FILE"
    if [ -f "$ENCLII_CREDENTIALS_FILE" ]; then
        echo "  Status: exists"
    else
        echo "  Status: not found"
    fi
    echo ""
}

# =============================================================================
# create_credential_file - Interactive credential file creation
# =============================================================================
create_credential_file() {
    local dir
    dir=$(dirname "$ENCLII_CREDENTIALS_FILE")

    if [ ! -d "$dir" ]; then
        mkdir -p "$dir"
        chmod 700 "$dir"
    fi

    if [ -f "$ENCLII_CREDENTIALS_FILE" ]; then
        _cf_log_warn "Credential file already exists: $ENCLII_CREDENTIALS_FILE"
        read -p "Overwrite? [y/N] " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            return 1
        fi
    fi

    echo ""
    echo "Creating Cloudflare credentials file..."
    echo ""

    read -p "Cloudflare API Token: " api_token
    read -p "Cloudflare Account ID: " account_id
    read -p "Tunnel ID: " tunnel_id

    cat > "$ENCLII_CREDENTIALS_FILE" << EOF
# Enclii Cloudflare Credentials
# Created: $(date -u +"%Y-%m-%dT%H:%M:%SZ")
#
# WARNING: Keep this file secure. Do not commit to version control.

[cloudflare]
api_token = $api_token
account_id = $account_id
tunnel_id = $tunnel_id
EOF

    chmod 600 "$ENCLII_CREDENTIALS_FILE"

    _cf_log_success "Credentials saved to $ENCLII_CREDENTIALS_FILE"
    echo ""
}

# =============================================================================
# Required API Token Permissions
# =============================================================================
# The "Enclii Platform Token" (All accounts, All zones) has:
#   - Account.Cloudflare Tunnel:Edit (tunnel config)
#   - Zone.Zone:Edit (create/manage zones)
#   - Zone.DNS:Edit (create DNS records)
#   - Zone.Zone Settings:Edit (always_use_https, min_tls_version, etc.)
#   - Zone.SSL and Certificates:Edit (SSL mode, certificates)
#
# To create/edit the token:
#   1. Go to Cloudflare Dashboard -> Profile -> API Tokens
#   2. Edit "Enclii Platform Token" or create new with above permissions
#   3. Set scope to "All accounts, All zones"
#   4. No TTL expiration recommended for platform token
#
# Stored in:
#   - K8s secret: enclii-cloudflare-credentials (enclii namespace)
#   - Local file: ~/.enclii/credentials
# =============================================================================
