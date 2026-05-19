#!/bin/bash
# Rotate GHCR credentials across all namespaces that need image pull access.
#
# Usage:
#   GHCR_USERNAME=madfam-bot GHCR_PAT=ghp_xxx ./scripts/rotate-ghcr-credentials.sh
#   GHCR_USERNAME=madfam-bot GHCR_PAT=ghp_xxx ./scripts/rotate-ghcr-credentials.sh --dry-run
#
# Prerequisites:
#   - kubectl configured with cluster access
#   - GHCR_PAT with read:packages (and write:packages if pushing) scope
#   - crane (optional) for credential validation

set -euo pipefail

GHCR_USERNAME="${GHCR_USERNAME:-madfam-bot}"
GHCR_PAT="${GHCR_PAT:-}"
SECRET_NAME="ghcr-credentials"
DRY_RUN="${1:-}"

# All namespaces that need GHCR pull access
NAMESPACES=(
    enclii
    janua
    dhanam
    enclii-builds
    npm-registry
    argocd
    cloudflare-tunnel
    foundry-scout
    monitoring
)

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info()    { echo -e "[INFO]    $1"; }
log_success() { echo -e "${GREEN}[OK]${NC}      $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC}    $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC}   $1"; }

# ── Validate inputs ──────────────────────────────────────────────────────────
if [[ -z "${GHCR_PAT}" ]]; then
    log_error "GHCR_PAT environment variable is required"
    echo ""
    echo "Usage: GHCR_USERNAME=madfam-bot GHCR_PAT=ghp_xxx $0"
    echo ""
    echo "Create a fine-grained PAT at https://github.com/settings/tokens?type=beta"
    echo "  Required scopes: read:packages (+ write:packages for build pushes)"
    echo "  Resource owner: madfam-org"
    echo "  Expiry: 1 year recommended"
    exit 1
fi

# ── Validate credential (optional, uses crane if available) ──────────────────
if command -v crane &>/dev/null; then
    log_info "Validating GHCR credentials with crane..."
    if echo "${GHCR_PAT}" | crane auth login ghcr.io --username "${GHCR_USERNAME}" --password-stdin 2>/dev/null; then
        log_success "GHCR credential is valid"
    else
        log_error "GHCR credential validation failed — check PAT scopes and expiry"
        exit 1
    fi
else
    log_warn "crane not found — skipping credential pre-validation (install with: brew install crane)"
fi

# ── Rotate in each namespace ─────────────────────────────────────────────────
UPDATED=0
FAILED=0

for NS in "${NAMESPACES[@]}"; do
    # Ensure namespace exists
    kubectl get namespace "${NS}" &>/dev/null 2>&1 || {
        log_warn "Namespace '${NS}' does not exist — skipping"
        continue
    }

    log_info "Rotating ${SECRET_NAME} in namespace '${NS}'..."

    CMD=(kubectl create secret docker-registry "${SECRET_NAME}"
        --namespace="${NS}"
        --docker-server=ghcr.io
        --docker-username="${GHCR_USERNAME}"
        --docker-password="${GHCR_PAT}"
        --dry-run=client -o yaml)

    if [[ "${DRY_RUN}" == "--dry-run" ]]; then
        "${CMD[@]}" | head -5
        log_info "(dry-run) Would apply to namespace '${NS}'"
    else
        if "${CMD[@]}" | kubectl apply -f - 2>/dev/null; then
            log_success "${NS}/${SECRET_NAME} updated"
            ((UPDATED++))
        else
            log_error "Failed to update ${NS}/${SECRET_NAME}"
            ((FAILED++))
        fi
    fi
done

# ── Summary ──────────────────────────────────────────────────────────────────
echo ""
if [[ "${DRY_RUN}" == "--dry-run" ]]; then
    log_info "Dry run complete. No changes were applied."
else
    log_info "Rotation complete: ${UPDATED} updated, ${FAILED} failed"
    if [[ ${FAILED} -gt 0 ]]; then
        exit 1
    fi
fi
