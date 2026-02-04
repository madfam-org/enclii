#!/usr/bin/env bash
#
# check-golden.sh - Compare current manifests against golden snapshots
#
# Fails if any critical manifest differs from its golden snapshot.
# Run this in CI to detect unintentional configuration drift.
#
# Usage: ./scripts/check-golden.sh
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
GOLDEN_DIR="${REPO_ROOT}/tests/golden/k8s"

# Check if golden configs exist
if [[ ! -d "${GOLDEN_DIR}" ]] || [[ -z "$(ls -A "${GOLDEN_DIR}" 2>/dev/null)" ]]; then
    echo -e "${YELLOW}Golden configs not initialized. Run ./scripts/update-golden.sh first.${NC}"
    exit 0  # Gracefully skip if not initialized
fi

echo "Checking golden configuration snapshots..."
echo ""

# Define configs as source:dest pairs
CONFIGS=(
    "infra/k8s/production/environment-patch.yaml:production/environment-patch.yaml.golden"
    "infra/k8s/production/cloudflared-unified.yaml:production/cloudflared-unified.yaml.golden"
    "infra/k8s/production/security-patch.yaml:production/security-patch.yaml.golden"
    "infra/k8s/base/roundhouse.yaml:base/roundhouse.yaml.golden"
    "apps/dispatch/k8s/deployment.yaml:apps/dispatch-deployment.yaml.golden"
    "infra/k8s/production/kyverno-guards.yaml:production/kyverno-guards.yaml.golden"
    "infra/k8s/production/monitoring/prometheus.yaml:production/monitoring/prometheus.yaml.golden"
    "infra/k8s/production/monitoring/grafana.yaml:production/monitoring/grafana.yaml.golden"
    "infra/k8s/production/monitoring/alertmanager.yaml:production/monitoring/alertmanager.yaml.golden"
    "infra/argocd/apps/monitoring.yaml:argocd/monitoring.yaml.golden"
    "infra/argocd/apps/storage.yaml:argocd/storage.yaml.golden"
    "infra/argocd/apps/image-updater.yaml:argocd/image-updater.yaml.golden"
    "infra/argocd/apps/external-secrets-operator.yaml:argocd/external-secrets-operator.yaml.golden"
    "infra/argocd/apps/ingress.yaml:argocd/ingress.yaml.golden"
    "infra/argocd/apps/arc-runners.yaml:argocd/arc-runners.yaml.golden"
    "infra/argocd/apps/kyverno.yaml:argocd/kyverno.yaml.golden"
    "infra/argocd/apps/core-services.yaml:argocd/core-services.yaml.golden"
    "infra/argocd/apps/ecosystem-services.yaml:argocd/ecosystem-services.yaml.golden"
    "infra/argocd/apps/janua.yaml:argocd/janua.yaml.golden"
    "infra/argocd/apps/dhanam.yaml:argocd/dhanam.yaml.golden"
)

FAILED=0
CHECKED=0

for config in "${CONFIGS[@]}"; do
    source="${config%%:*}"
    dest="${config##*:}"
    source_path="${REPO_ROOT}/${source}"
    dest_path="${GOLDEN_DIR}/${dest}"

    # Skip if golden doesn't exist yet
    if [[ ! -f "${dest_path}" ]]; then
        echo -e "${YELLOW}  Skipped: ${dest} (golden not found)${NC}"
        continue
    fi

    # Skip if source doesn't exist
    if [[ ! -f "${source_path}" ]]; then
        echo -e "${RED}  Missing: ${source}${NC}"
        FAILED=1
        continue
    fi

    CHECKED=$((CHECKED + 1))

    # Compare files
    if diff -q "${source_path}" "${dest_path}" > /dev/null 2>&1; then
        echo -e "${GREEN}  OK: ${source}${NC}"
    else
        echo -e "${RED}  DRIFT: ${source}${NC}"
        echo ""
        echo "  Diff (current vs golden):"
        diff "${source_path}" "${dest_path}" || true
        echo ""
        FAILED=1
    fi
done

echo ""

if [[ ${CHECKED} -eq 0 ]]; then
    echo -e "${YELLOW}No golden configs to check. Run ./scripts/update-golden.sh to initialize.${NC}"
    exit 0
fi

if [[ ${FAILED} -eq 1 ]]; then
    echo -e "${RED}Golden config check failed!${NC}"
    echo ""
    echo "Options:"
    echo "  1. If changes are intentional and tested: ./scripts/update-golden.sh"
    echo "  2. If changes are unintentional: revert the manifest changes"
    exit 1
fi

echo -e "${GREEN}All golden config checks passed! (${CHECKED} files)${NC}"
