#!/bin/bash
# deploy-monitoring.sh - Deploy Enclii production monitoring stack
#
# Usage:
#   ./scripts/deploy-monitoring.sh [command]
#
# Commands:
#   deploy    Deploy the full monitoring stack (default)
#   status    Check monitoring stack status
#   destroy   Remove the monitoring stack
#   password  Generate and set a secure Grafana admin password
#   tunnel    Add monitoring endpoints to Cloudflare tunnel

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
MONITORING_DIR="${PROJECT_ROOT}/infra/k8s/production/monitoring"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

check_prerequisites() {
    log_info "Checking prerequisites..."

    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed"
        exit 1
    fi

    if ! kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster"
        exit 1
    fi

    log_success "Prerequisites check passed"
}

deploy_monitoring() {
    log_info "Deploying monitoring stack..."

    # Apply monitoring manifests
    kubectl apply -k "${MONITORING_DIR}"

    # Wait for deployments
    log_info "Waiting for Prometheus to be ready..."
    kubectl wait --for=condition=available --timeout=300s deployment/prometheus -n monitoring || true

    log_info "Waiting for Grafana to be ready..."
    kubectl wait --for=condition=available --timeout=300s deployment/grafana -n monitoring || true

    log_info "Waiting for AlertManager to be ready..."
    kubectl wait --for=condition=available --timeout=300s deployment/alertmanager -n monitoring || true

    log_success "Monitoring stack deployed successfully!"

    # Show status
    show_status

    # Reminder about password
    log_warn "IMPORTANT: Change the default Grafana password!"
    echo -e "  Run: ${YELLOW}./scripts/deploy-monitoring.sh password${NC}"
}

show_status() {
    log_info "Monitoring Stack Status:"
    echo ""

    echo "=== Pods ==="
    kubectl get pods -n monitoring -o wide
    echo ""

    echo "=== Services ==="
    kubectl get svc -n monitoring
    echo ""

    echo "=== PVCs ==="
    kubectl get pvc -n monitoring
    echo ""

    # Port forward instructions
    log_info "To access locally:"
    echo "  Prometheus:    kubectl port-forward -n monitoring svc/prometheus 9090:9090"
    echo "  Grafana:       kubectl port-forward -n monitoring svc/grafana 3000:3000"
    echo "  AlertManager:  kubectl port-forward -n monitoring svc/alertmanager 9093:9093"
    echo ""

    log_info "Default Grafana credentials:"
    echo "  Username: admin"
    echo "  Password: (set via kubectl secret or deploy-monitoring.sh password)"
}

destroy_monitoring() {
    log_warn "This will remove the entire monitoring stack!"
    read -p "Are you sure? (y/N) " -n 1 -r
    echo

    if [[ $REPLY =~ ^[Yy]$ ]]; then
        log_info "Removing monitoring stack..."
        kubectl delete -k "${MONITORING_DIR}" --ignore-not-found=true
        log_success "Monitoring stack removed"
    else
        log_info "Cancelled"
    fi
}

set_grafana_password() {
    log_info "Generating secure Grafana admin password..."

    # Generate secure password
    NEW_PASSWORD=$(openssl rand -base64 24)

    # Update secret
    kubectl create secret generic grafana-credentials \
        --from-literal=admin-user=admin \
        --from-literal=admin-password="${NEW_PASSWORD}" \
        -n monitoring \
        --dry-run=client -o yaml | kubectl apply -f -

    # Restart Grafana to pick up new password
    kubectl rollout restart deployment/grafana -n monitoring

    log_success "Grafana password updated!"
    echo ""
    echo "=== New Grafana Credentials ==="
    echo "  Username: admin"
    echo "  Password: ${NEW_PASSWORD}"
    echo ""
    log_warn "Save this password securely! It won't be shown again."
}

add_tunnel_routes() {
    log_info "Adding monitoring routes to Cloudflare tunnel..."

    # This would update the cloudflared config to expose monitoring endpoints
    # For now, provide instructions

    log_warn "Manual step required:"
    echo ""
    echo "Add the following to your cloudflared tunnel config:"
    echo ""
    cat << 'EOF'
# In infra/k8s/production/cloudflared-unified.yaml, add to ingress rules:

- hostname: grafana.enclii.dev
  service: http://grafana.monitoring.svc.cluster.local:3000
  originRequest:
    noTLSVerify: true

# Do NOT publish Prometheus or AlertManager on the tunnel: neither has
# authentication. Grafana, which requires a login, is the UI (FDY-01,
# 2026-10-04).
EOF
    echo ""
    log_info "After updating, restart cloudflared:"
    echo "  kubectl rollout restart deployment/cloudflared -n ingress"
}

# Main
case "${1:-deploy}" in
    deploy)
        check_prerequisites
        deploy_monitoring
        ;;
    status)
        show_status
        ;;
    destroy)
        destroy_monitoring
        ;;
    password)
        set_grafana_password
        ;;
    tunnel)
        add_tunnel_routes
        ;;
    *)
        echo "Usage: $0 {deploy|status|destroy|password|tunnel}"
        exit 1
        ;;
esac
