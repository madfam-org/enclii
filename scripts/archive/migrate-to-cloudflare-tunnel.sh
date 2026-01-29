#!/bin/bash
# migrate-to-cloudflare-tunnel.sh
# Migrates from hostPort to Cloudflare Tunnel for zero-downtime deployments
#
# Prerequisites:
# 1. cloudflared CLI installed
# 2. Logged into Cloudflare: cloudflared login
# 3. KUBECONFIG set to the target cluster
#
# Usage:
#   ./scripts/migrate-to-cloudflare-tunnel.sh [setup|deploy|remove-hostport|verify|rollback]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config-hetzner}"
TUNNEL_NAME="enclii-production"
NAMESPACE="cloudflare-tunnel"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Check prerequisites
check_prereqs() {
    log_info "Checking prerequisites..."

    if ! command -v cloudflared &> /dev/null; then
        log_error "cloudflared CLI not found. Install: brew install cloudflared"
        exit 1
    fi

    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl not found"
        exit 1
    fi

    if ! KUBECONFIG="$KUBECONFIG" kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to cluster. Check KUBECONFIG"
        exit 1
    fi

    log_info "Prerequisites OK"
}

# Step 1: Setup Cloudflare Tunnel
setup_tunnel() {
    log_info "Setting up Cloudflare Tunnel..."

    # Check if tunnel exists
    if cloudflared tunnel list | grep -q "$TUNNEL_NAME"; then
        log_info "Tunnel '$TUNNEL_NAME' already exists"
    else
        log_info "Creating tunnel '$TUNNEL_NAME'..."
        cloudflared tunnel create "$TUNNEL_NAME"
    fi

    # Get tunnel token
    log_info "Getting tunnel token..."
    TUNNEL_TOKEN=$(cloudflared tunnel token "$TUNNEL_NAME")

    if [ -z "$TUNNEL_TOKEN" ]; then
        log_error "Failed to get tunnel token"
        exit 1
    fi

    log_info "Tunnel token retrieved successfully"
    echo ""
    echo "============================================"
    echo "IMPORTANT: Save this token securely!"
    echo "============================================"
    echo "Tunnel Token: $TUNNEL_TOKEN"
    echo "============================================"
    echo ""

    # Create namespace if not exists
    KUBECONFIG="$KUBECONFIG" kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | \
        KUBECONFIG="$KUBECONFIG" kubectl apply -f -

    # Create secret with token
    log_info "Creating tunnel token secret in cluster..."
    KUBECONFIG="$KUBECONFIG" kubectl create secret generic cloudflared-token \
        --namespace="$NAMESPACE" \
        --from-literal=token="$TUNNEL_TOKEN" \
        --dry-run=client -o yaml | \
        KUBECONFIG="$KUBECONFIG" kubectl apply -f -

    log_info "Tunnel setup complete!"
    echo ""
    echo "Next steps:"
    echo "1. Add DNS routes in Cloudflare dashboard:"
    echo "   - api.enclii.dev → $TUNNEL_NAME"
    echo "   - app.enclii.dev → $TUNNEL_NAME"
    echo "   - enclii.dev → $TUNNEL_NAME"
    echo "   - auth.madfam.io → $TUNNEL_NAME"
    echo "   - (etc. for all hostnames)"
    echo ""
    echo "2. Run: $0 deploy"
}

# Step 2: Deploy cloudflared
deploy_cloudflared() {
    log_info "Deploying cloudflared..."

    # Check if secret exists
    if ! KUBECONFIG="$KUBECONFIG" kubectl get secret cloudflared-token -n "$NAMESPACE" &> /dev/null; then
        log_error "Tunnel token secret not found. Run: $0 setup"
        exit 1
    fi

    # Apply the manifest
    KUBECONFIG="$KUBECONFIG" kubectl apply -f "$PROJECT_ROOT/infra/k8s/production/cloudflared-unified.yaml"

    # Wait for deployment
    log_info "Waiting for cloudflared deployment..."
    KUBECONFIG="$KUBECONFIG" kubectl rollout status deployment/cloudflared -n "$NAMESPACE" --timeout=120s

    log_info "cloudflared deployed successfully!"

    # Show pod status
    KUBECONFIG="$KUBECONFIG" kubectl get pods -n "$NAMESPACE" -l app=cloudflared
}

# Step 3: Remove hostPort from deployments
remove_hostport() {
    log_info "Removing hostPort from deployments..."

    # Enclii namespace
    local enclii_deployments=("switchyard-api" "switchyard-ui" "landing-page")
    for dep in "${enclii_deployments[@]}"; do
        if KUBECONFIG="$KUBECONFIG" kubectl get deployment "$dep" -n enclii &> /dev/null; then
            log_info "Removing hostPort from $dep (enclii)..."
            KUBECONFIG="$KUBECONFIG" kubectl patch deployment "$dep" -n enclii --type='json' \
                -p='[{"op": "remove", "path": "/spec/template/spec/containers/0/ports/0/hostPort"}]' 2>/dev/null || \
                log_warn "hostPort already removed or not present for $dep"
        fi
    done

    # Janua namespace
    local janua_deployments=("janua-api" "janua-admin" "janua-dashboard" "janua-docs" "janua-website")
    for dep in "${janua_deployments[@]}"; do
        if KUBECONFIG="$KUBECONFIG" kubectl get deployment "$dep" -n janua &> /dev/null; then
            log_info "Removing hostPort from $dep (janua)..."
            KUBECONFIG="$KUBECONFIG" kubectl patch deployment "$dep" -n janua --type='json' \
                -p='[{"op": "remove", "path": "/spec/template/spec/containers/0/ports/0/hostPort"}]' 2>/dev/null || \
                log_warn "hostPort already removed or not present for $dep"
        fi
    done

    # Restore RollingUpdate strategy now that hostPort is gone
    log_info "Restoring RollingUpdate strategy..."
    for dep in "${enclii_deployments[@]}"; do
        if KUBECONFIG="$KUBECONFIG" kubectl get deployment "$dep" -n enclii &> /dev/null; then
            KUBECONFIG="$KUBECONFIG" kubectl patch deployment "$dep" -n enclii --type='json' \
                -p='[{"op": "replace", "path": "/spec/strategy", "value": {"type": "RollingUpdate", "rollingUpdate": {"maxSurge": 1, "maxUnavailable": 0}}}]'
        fi
    done

    for dep in "${janua_deployments[@]}"; do
        if KUBECONFIG="$KUBECONFIG" kubectl get deployment "$dep" -n janua &> /dev/null; then
            KUBECONFIG="$KUBECONFIG" kubectl patch deployment "$dep" -n janua --type='json' \
                -p='[{"op": "replace", "path": "/spec/strategy", "value": {"type": "RollingUpdate", "rollingUpdate": {"maxSurge": 1, "maxUnavailable": 0}}}]'
        fi
    done

    log_info "hostPort removal complete!"
}

# Step 4: Verify
verify() {
    log_info "Verifying setup..."

    echo ""
    echo "=== Cloudflared Pods ==="
    KUBECONFIG="$KUBECONFIG" kubectl get pods -n "$NAMESPACE" -l app=cloudflared

    echo ""
    echo "=== Cloudflared Logs (last 10 lines) ==="
    KUBECONFIG="$KUBECONFIG" kubectl logs -n "$NAMESPACE" -l app=cloudflared --tail=10 2>/dev/null || \
        log_warn "No logs available yet"

    echo ""
    echo "=== Deployment Strategies ==="
    echo "Enclii:"
    KUBECONFIG="$KUBECONFIG" kubectl get deployments -n enclii -o jsonpath='{range .items[*]}{.metadata.name}{": "}{.spec.strategy.type}{"\n"}{end}'
    echo "Janua:"
    KUBECONFIG="$KUBECONFIG" kubectl get deployments -n janua -o jsonpath='{range .items[*]}{.metadata.name}{": "}{.spec.strategy.type}{"\n"}{end}'

    echo ""
    echo "=== hostPort Check ==="
    echo "Deployments still using hostPort:"
    KUBECONFIG="$KUBECONFIG" kubectl get deployments -A -o json | \
        jq -r '.items[] | select(.spec.template.spec.containers[0].ports[0].hostPort != null) | "\(.metadata.namespace)/\(.metadata.name): hostPort=\(.spec.template.spec.containers[0].ports[0].hostPort)"' 2>/dev/null || \
        echo "None found (good!)"

    echo ""
    echo "=== External Connectivity Test ==="
    for url in "https://api.enclii.dev/health" "https://app.enclii.dev" "https://auth.madfam.io/.well-known/openid-configuration"; do
        status=$(curl -s -o /dev/null -w "%{http_code}" "$url" 2>/dev/null || echo "000")
        if [ "$status" == "200" ] || [ "$status" == "404" ]; then
            echo "  $url: ${GREEN}OK${NC} ($status)"
        else
            echo "  $url: ${YELLOW}$status${NC}"
        fi
    done
}

# Rollback (restore hostPort if needed)
rollback() {
    log_warn "Rolling back to hostPort configuration..."

    # Restore hostPort for Enclii services
    KUBECONFIG="$KUBECONFIG" kubectl patch deployment switchyard-api -n enclii --type='json' \
        -p='[{"op": "add", "path": "/spec/template/spec/containers/0/ports/0/hostPort", "value": 4200}]' 2>/dev/null || true
    KUBECONFIG="$KUBECONFIG" kubectl patch deployment switchyard-ui -n enclii --type='json' \
        -p='[{"op": "add", "path": "/spec/template/spec/containers/0/ports/0/hostPort", "value": 4201}]' 2>/dev/null || true
    KUBECONFIG="$KUBECONFIG" kubectl patch deployment landing-page -n enclii --type='json' \
        -p='[{"op": "add", "path": "/spec/template/spec/containers/0/ports/0/hostPort", "value": 4204}]' 2>/dev/null || true

    # Restore Recreate strategy for hostPort compatibility
    for dep in switchyard-api switchyard-ui landing-page; do
        KUBECONFIG="$KUBECONFIG" kubectl patch deployment "$dep" -n enclii --type='json' \
            -p='[{"op": "replace", "path": "/spec/strategy", "value": {"type": "Recreate"}}]' 2>/dev/null || true
    done

    log_info "Rollback complete. Services restored to hostPort configuration."
}

# Main
case "${1:-help}" in
    setup)
        check_prereqs
        setup_tunnel
        ;;
    deploy)
        check_prereqs
        deploy_cloudflared
        ;;
    remove-hostport)
        check_prereqs
        remove_hostport
        ;;
    verify)
        check_prereqs
        verify
        ;;
    rollback)
        check_prereqs
        rollback
        ;;
    full)
        check_prereqs
        setup_tunnel
        echo ""
        read -p "Press Enter after configuring DNS routes in Cloudflare..."
        deploy_cloudflared
        remove_hostport
        verify
        ;;
    *)
        echo "Usage: $0 {setup|deploy|remove-hostport|verify|rollback|full}"
        echo ""
        echo "Commands:"
        echo "  setup          - Create Cloudflare Tunnel and store token in cluster"
        echo "  deploy         - Deploy cloudflared to cluster"
        echo "  remove-hostport - Remove hostPort from all deployments"
        echo "  verify         - Check status of migration"
        echo "  rollback       - Restore hostPort configuration"
        echo "  full           - Run complete migration (interactive)"
        exit 1
        ;;
esac
