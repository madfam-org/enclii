#!/bin/bash
# ARC Installation Script
# Usage: ./install-arc.sh [--controller-only|--runners-only|--all]
#
# Prerequisites:
# 1. kubectl configured with cluster access
# 2. helm v3.14+ installed
# 3. GitHub App secret created in arc-system namespace
#
# To create the GitHub App secret:
#   kubectl create secret generic github-app-secret \
#     --namespace arc-system \
#     --from-literal=github_app_id=<APP_ID> \
#     --from-literal=github_app_installation_id=<INSTALLATION_ID> \
#     --from-file=github_app_private_key=<PATH_TO_PEM>

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
CHART_VERSION="0.14.0"
CONTROLLER_NAMESPACE="arc-system"
RUNNER_NAMESPACE="arc-runners"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_prerequisites() {
    log_info "Checking prerequisites..."

    # Check kubectl
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl not found. Please install kubectl."
        exit 1
    fi

    # Check helm
    if ! command -v helm &> /dev/null; then
        log_error "helm not found. Please install helm v3.14+."
        exit 1
    fi

    # Check helm version
    HELM_VERSION=$(helm version --short | grep -oE 'v[0-9]+\.[0-9]+' | head -1)
    if [[ "${HELM_VERSION}" < "v3.14" ]]; then
        log_warn "Helm version ${HELM_VERSION} may not support OCI charts. Recommended: v3.14+"
    fi

    # Check cluster access
    if ! kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster. Check your kubeconfig."
        exit 1
    fi

    log_success "Prerequisites check passed"
}

apply_base_manifests() {
    log_info "Applying base Kubernetes manifests..."

    kubectl apply -f "${REPO_ROOT}/infra/k8s/base/arc/namespace.yaml"
    kubectl apply -f "${REPO_ROOT}/infra/k8s/base/arc/network-policies.yaml"

    log_success "Base manifests applied"
}

apply_production_manifests() {
    log_info "Applying production Kubernetes manifests..."

    kubectl apply -f "${REPO_ROOT}/infra/k8s/production/arc/storage.yaml"

    # Apply monitoring if CRDs exist
    if kubectl get crd servicemonitors.monitoring.coreos.com &> /dev/null; then
        kubectl apply -f "${REPO_ROOT}/infra/k8s/production/arc/monitoring.yaml"
        log_success "Monitoring manifests applied"
    else
        log_warn "Prometheus Operator CRDs not found. Skipping monitoring manifests."
    fi

    log_success "Production manifests applied"
}

check_github_secret() {
    log_info "Checking for GitHub App secret..."

    if ! kubectl get secret github-app-secret -n "${CONTROLLER_NAMESPACE}" &> /dev/null; then
        log_error "GitHub App secret not found in ${CONTROLLER_NAMESPACE} namespace."
        echo ""
        echo "Create it with:"
        echo "  kubectl create secret generic github-app-secret \\"
        echo "    --namespace ${CONTROLLER_NAMESPACE} \\"
        echo "    --from-literal=github_app_id=<APP_ID> \\"
        echo "    --from-literal=github_app_installation_id=<INSTALLATION_ID> \\"
        echo "    --from-file=github_app_private_key=<PATH_TO_PEM>"
        exit 1
    fi

    log_success "GitHub App secret found"
}

install_controller() {
    log_info "Installing ARC Controller..."

    helm upgrade --install arc-controller \
        oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set-controller \
        --namespace "${CONTROLLER_NAMESPACE}" \
        --create-namespace \
        --version "${CHART_VERSION}" \
        --values "${REPO_ROOT}/infra/helm/arc/values-controller.yaml" \
        --wait \
        --timeout 5m

    log_info "Waiting for controller to be ready..."
    kubectl rollout status deployment/arc-gha-rs-controller -n "${CONTROLLER_NAMESPACE}" --timeout=2m

    log_success "ARC Controller installed successfully"
}

install_runners() {
    log_info "Installing ARC Runner Scale Sets..."

    # Copy secret to runner namespace for scale sets
    if ! kubectl get secret github-app-secret -n "${RUNNER_NAMESPACE}" &> /dev/null; then
        log_info "Copying GitHub App secret to runner namespace..."
        kubectl get secret github-app-secret -n "${CONTROLLER_NAMESPACE}" -o yaml | \
            sed "s/namespace: ${CONTROLLER_NAMESPACE}/namespace: ${RUNNER_NAMESPACE}/" | \
            kubectl apply -f -
    fi

    # Install Blue scale set (active)
    log_info "Installing Blue runner scale set..."
    helm upgrade --install madfam-runners-blue \
        oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set \
        --namespace "${RUNNER_NAMESPACE}" \
        --create-namespace \
        --version "${CHART_VERSION}" \
        --values "${REPO_ROOT}/infra/helm/arc/values-runner-set.yaml" \
        --values "${REPO_ROOT}/infra/helm/arc/values-runner-set-blue.yaml" \
        --wait \
        --timeout 5m

    # Install Green scale set (standby)
    log_info "Installing Green runner scale set..."
    helm upgrade --install madfam-runners-green \
        oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set \
        --namespace "${RUNNER_NAMESPACE}" \
        --create-namespace \
        --version "${CHART_VERSION}" \
        --values "${REPO_ROOT}/infra/helm/arc/values-runner-set.yaml" \
        --values "${REPO_ROOT}/infra/helm/arc/values-runner-set-green.yaml" \
        --wait \
        --timeout 5m

    log_success "Runner scale sets installed"
}

verify_installation() {
    log_info "Verifying installation..."

    echo ""
    echo "=== ARC Controller ==="
    kubectl get pods -n "${CONTROLLER_NAMESPACE}" -l app.kubernetes.io/name=gha-rs-controller

    echo ""
    echo "=== Runner Scale Sets ==="
    kubectl get autoscalingrunnerset -n "${RUNNER_NAMESPACE}" 2>/dev/null || \
        kubectl get pods -n "${RUNNER_NAMESPACE}"

    echo ""
    echo "=== PVCs ==="
    kubectl get pvc -n "${RUNNER_NAMESPACE}"

    echo ""
    log_success "Installation verification complete"
    echo ""
    echo "Next steps:"
    echo "1. Check GitHub UI: Settings → Actions → Runners"
    echo "2. Runners should appear as 'madfam-runners-blue' and 'madfam-runners-green'"
    echo "3. Update .github/workflows/ci.yml to use 'runs-on: madfam-runners-blue'"
}

print_usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --controller-only   Install only the ARC controller"
    echo "  --runners-only      Install only the runner scale sets (requires controller)"
    echo "  --all               Install everything (default)"
    echo "  --verify            Only run verification checks"
    echo "  -h, --help          Show this help message"
}

# Main execution
main() {
    local mode="all"

    while [[ $# -gt 0 ]]; do
        case $1 in
            --controller-only)
                mode="controller"
                shift
                ;;
            --runners-only)
                mode="runners"
                shift
                ;;
            --all)
                mode="all"
                shift
                ;;
            --verify)
                mode="verify"
                shift
                ;;
            -h|--help)
                print_usage
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                print_usage
                exit 1
                ;;
        esac
    done

    echo ""
    echo "============================================"
    echo "  ARC Installation Script"
    echo "  Mode: ${mode}"
    echo "============================================"
    echo ""

    check_prerequisites

    case ${mode} in
        controller)
            apply_base_manifests
            check_github_secret
            install_controller
            ;;
        runners)
            apply_production_manifests
            check_github_secret
            install_runners
            ;;
        all)
            apply_base_manifests
            apply_production_manifests
            check_github_secret
            install_controller
            install_runners
            ;;
        verify)
            verify_installation
            exit 0
            ;;
    esac

    verify_installation
}

main "$@"
