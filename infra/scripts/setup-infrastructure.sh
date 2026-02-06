#!/bin/bash
# Enclii Infrastructure Setup Script
# This script deploys all infrastructure components via ArgoCD GitOps
#
# Prerequisites:
# - kubectl configured with cluster access
# - ArgoCD installed and accessible
# - Cloudflare tunnel configured
# - Doppler account with service token (for ESO)
# - GitHub PAT with read:packages scope (for image pulls)
#
# Usage:
#   ./setup-infrastructure.sh check     # Verify prerequisites
#   ./setup-infrastructure.sh repos     # Add Helm repositories
#   ./setup-infrastructure.sh secrets   # Create required secrets
#   ./setup-infrastructure.sh apply     # Apply ArgoCD applications
#   ./setup-infrastructure.sh status    # Check deployment status
#   ./setup-infrastructure.sh all       # Run all steps

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

check_prerequisites() {
    log_info "Checking prerequisites..."

    local missing=()

    # Check kubectl
    if ! command -v kubectl &> /dev/null; then
        missing+=("kubectl")
    else
        log_success "kubectl found"
    fi

    # Check helm
    if ! command -v helm &> /dev/null; then
        missing+=("helm")
    else
        log_success "helm found"
    fi

    # Check cluster access
    if ! kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster"
        log_info "Ensure KUBECONFIG is set correctly"
        return 1
    else
        log_success "Cluster connection verified"
    fi

    # Check ArgoCD namespace
    if ! kubectl get namespace argocd &> /dev/null; then
        log_warn "ArgoCD namespace not found - install ArgoCD first"
        log_info "Run: kubectl create namespace argocd && kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/v2.14.3/manifests/install.yaml"
    else
        log_success "ArgoCD namespace exists"
    fi

    # Check for required environment variables (optional)
    if [[ -z "${GITHUB_TOKEN:-}" ]]; then
        log_warn "GITHUB_TOKEN not set - needed for ghcr.io authentication"
    fi

    if [[ -z "${DOPPLER_SERVICE_TOKEN:-}" ]]; then
        log_warn "DOPPLER_SERVICE_TOKEN not set - needed for External Secrets"
    fi

    if [[ ${#missing[@]} -gt 0 ]]; then
        log_error "Missing tools: ${missing[*]}"
        return 1
    fi

    log_success "All prerequisites met"
}

add_helm_repos() {
    log_info "Adding Helm repositories..."

    helm repo add kyverno https://kyverno.github.io/kyverno/ || true
    helm repo add argo https://argoproj.github.io/argo-helm || true
    helm repo add external-secrets https://charts.external-secrets.io || true
    helm repo add longhorn https://charts.longhorn.io || true
    helm repo add prometheus-community https://prometheus-community.github.io/helm-charts || true

    log_info "Updating Helm repositories..."
    helm repo update

    log_success "Helm repositories configured"
}

create_secrets() {
    log_info "Creating required secrets..."

    # GHCR credentials for ArgoCD
    if [[ -n "${GITHUB_TOKEN:-}" && -n "${GITHUB_USERNAME:-}" ]]; then
        log_info "Creating ghcr-credentials secret..."
        kubectl create secret generic ghcr-credentials \
            -n argocd \
            --from-literal=username="${GITHUB_USERNAME}" \
            --from-literal=password="${GITHUB_TOKEN}" \
            --dry-run=client -o yaml | kubectl apply -f -
        log_success "ghcr-credentials created"
    else
        log_warn "Skipping ghcr-credentials - GITHUB_TOKEN or GITHUB_USERNAME not set"
    fi

    # Git credentials for image updater write-back
    if [[ -n "${GITHUB_TOKEN:-}" && -n "${GITHUB_USERNAME:-}" ]]; then
        log_info "Creating git-creds secret..."
        kubectl create secret generic git-creds \
            -n argocd \
            --from-literal=username="${GITHUB_USERNAME}" \
            --from-literal=password="${GITHUB_TOKEN}" \
            --dry-run=client -o yaml | kubectl apply -f -
        log_success "git-creds created"
    else
        log_warn "Skipping git-creds - GITHUB_TOKEN or GITHUB_USERNAME not set"
    fi

    # Doppler token for External Secrets
    if [[ -n "${DOPPLER_SERVICE_TOKEN:-}" ]]; then
        log_info "Creating doppler-token-auth secret..."
        kubectl create namespace external-secrets --dry-run=client -o yaml | kubectl apply -f -
        kubectl create secret generic doppler-token-auth \
            -n external-secrets \
            --from-literal=dopplerToken="${DOPPLER_SERVICE_TOKEN}" \
            --dry-run=client -o yaml | kubectl apply -f -
        log_success "doppler-token-auth created"
    else
        log_warn "Skipping doppler-token-auth - DOPPLER_SERVICE_TOKEN not set"
    fi

    # Build namespace secrets
    log_info "Creating enclii-builds namespace..."
    kubectl create namespace enclii-builds --dry-run=client -o yaml | kubectl apply -f -

    if [[ -n "${GITHUB_TOKEN:-}" && -n "${GITHUB_USERNAME:-}" ]]; then
        log_info "Creating ghcr-credentials for builds..."
        kubectl create secret docker-registry ghcr-credentials \
            -n enclii-builds \
            --docker-server=ghcr.io \
            --docker-username="${GITHUB_USERNAME}" \
            --docker-password="${GITHUB_TOKEN}" \
            --dry-run=client -o yaml | kubectl apply -f -
        log_success "ghcr-credentials created in enclii-builds"

        log_info "Creating git-credentials for builds..."
        kubectl create secret generic git-credentials \
            -n enclii-builds \
            --from-literal=username="${GITHUB_USERNAME}" \
            --from-literal=password="${GITHUB_TOKEN}" \
            --dry-run=client -o yaml | kubectl apply -f -
        log_success "git-credentials created in enclii-builds"
    fi

    # Grafana admin password
    log_info "Creating Grafana credentials..."
    kubectl create namespace monitoring --dry-run=client -o yaml | kubectl apply -f -
    GRAFANA_PASSWORD=$(openssl rand -base64 32)
    kubectl create secret generic grafana-credentials \
        -n monitoring \
        --from-literal=admin-user=admin \
        --from-literal=admin-password="${GRAFANA_PASSWORD}" \
        --dry-run=client -o yaml | kubectl apply -f -
    log_success "grafana-credentials created"
    log_info "Grafana admin password: ${GRAFANA_PASSWORD}"

    log_success "All secrets created"
}

apply_argocd_apps() {
    log_info "Applying ArgoCD applications..."

    # Apply root application (App-of-Apps pattern)
    log_info "Applying root application..."
    kubectl apply -f "${REPO_ROOT}/infra/argocd/root-application.yaml"

    log_success "ArgoCD applications applied"
    log_info "ArgoCD will now sync all child applications automatically"
}

check_status() {
    log_info "Checking deployment status..."

    echo ""
    log_info "=== ArgoCD Applications ==="
    kubectl get applications -n argocd -o wide 2>/dev/null || log_warn "Could not fetch ArgoCD applications"

    echo ""
    log_info "=== Kyverno ==="
    kubectl get pods -n kyverno 2>/dev/null || log_warn "Kyverno not deployed yet"

    echo ""
    log_info "=== External Secrets ==="
    kubectl get pods -n external-secrets 2>/dev/null || log_warn "External Secrets not deployed yet"

    echo ""
    log_info "=== Monitoring ==="
    kubectl get pods -n monitoring 2>/dev/null || log_warn "Monitoring not deployed yet"

    echo ""
    log_info "=== Kyverno Policies ==="
    kubectl get clusterpolicies 2>/dev/null || log_warn "Kyverno policies not deployed yet"

    echo ""
    log_info "=== ClusterSecretStores ==="
    kubectl get clustersecretstores 2>/dev/null || log_warn "ClusterSecretStores not configured yet"
}

print_dns_instructions() {
    log_info "=== DNS Configuration Required ==="
    echo ""
    echo "Add the following DNS records in Cloudflare (CNAME to your tunnel):"
    echo ""
    echo "  grafana.enclii.dev     → <tunnel-id>.cfargotunnel.com"
    echo "  prometheus.enclii.dev  → <tunnel-id>.cfargotunnel.com"
    echo "  alertmanager.enclii.dev → <tunnel-id>.cfargotunnel.com"
    echo ""
    echo "Access URLs after deployment:"
    echo "  Grafana:      https://grafana.enclii.dev"
    echo "  Prometheus:   https://prometheus.enclii.dev"
    echo "  AlertManager: https://alertmanager.enclii.dev"
    echo "  ArgoCD:       https://argocd.enclii.dev"
    echo ""
}

run_all() {
    check_prerequisites
    add_helm_repos
    create_secrets
    apply_argocd_apps

    log_info "Waiting for ArgoCD to sync (60 seconds)..."
    sleep 60

    check_status
    print_dns_instructions

    log_success "Infrastructure setup complete!"
}

# Main
case "${1:-help}" in
    check)
        check_prerequisites
        ;;
    repos)
        add_helm_repos
        ;;
    secrets)
        create_secrets
        ;;
    apply)
        apply_argocd_apps
        ;;
    status)
        check_status
        ;;
    dns)
        print_dns_instructions
        ;;
    all)
        run_all
        ;;
    *)
        echo "Enclii Infrastructure Setup"
        echo ""
        echo "Usage: $0 <command>"
        echo ""
        echo "Commands:"
        echo "  check   - Verify prerequisites"
        echo "  repos   - Add Helm repositories"
        echo "  secrets - Create required secrets"
        echo "  apply   - Apply ArgoCD applications"
        echo "  status  - Check deployment status"
        echo "  dns     - Print DNS configuration instructions"
        echo "  all     - Run all steps"
        echo ""
        echo "Environment variables:"
        echo "  GITHUB_USERNAME         - GitHub username for registry auth"
        echo "  GITHUB_TOKEN            - GitHub PAT with read:packages scope"
        echo "  DOPPLER_SERVICE_TOKEN   - Doppler service token for ESO"
        ;;
esac
