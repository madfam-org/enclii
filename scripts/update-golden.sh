#!/usr/bin/env bash
#
# update-golden.sh - Update golden configuration snapshots
#
# Run this after intentionally modifying critical manifests and verifying
# the changes work in production.
#
# Usage: ./scripts/update-golden.sh
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

echo -e "${YELLOW}Updating golden configuration snapshots...${NC}"
echo ""

# Ensure golden directories exist
mkdir -p "${GOLDEN_DIR}/production"
mkdir -p "${GOLDEN_DIR}/base"
mkdir -p "${GOLDEN_DIR}/apps"

# Define configs as source:dest pairs (must match check-golden.sh)
CONFIGS=(
    "infra/k8s/production/environment-patch.yaml:production/environment-patch.yaml.golden"
    "infra/k8s/production/cloudflared-unified.yaml:production/cloudflared-unified.yaml.golden"
    "infra/k8s/production/security-patch.yaml:production/security-patch.yaml.golden"
    "infra/k8s/production/kyverno-guards.yaml:production/kyverno-guards.yaml.golden"
    "infra/k8s/production/monitoring/prometheus.yaml:production/monitoring/prometheus.yaml.golden"
    "infra/k8s/production/monitoring/grafana.yaml:production/monitoring/grafana.yaml.golden"
    "infra/k8s/production/monitoring/alertmanager.yaml:production/monitoring/alertmanager.yaml.golden"
    "infra/k8s/base/roundhouse.yaml:base/roundhouse.yaml.golden"
    "apps/dispatch/k8s/deployment.yaml:apps/dispatch-deployment.yaml.golden"
    "infra/argocd/apps/monitoring.yaml:argocd/monitoring.yaml.golden"
    "infra/argocd/apps/storage.yaml:argocd/storage.yaml.golden"
    "infra/argocd/apps/external-secrets-operator.yaml:argocd/external-secrets-operator.yaml.golden"
    "infra/argocd/apps/ingress.yaml:argocd/ingress.yaml.golden"
    "infra/argocd/apps/arc-runners.yaml:argocd/arc-runners.yaml.golden"
    "infra/argocd/apps/kyverno.yaml:argocd/kyverno.yaml.golden"
    "infra/argocd/apps/core-services.yaml:argocd/core-services.yaml.golden"
    "infra/argocd/apps/ecosystem-services.yaml:argocd/ecosystem-services.yaml.golden"
    "infra/argocd/apps/janua.yaml:argocd/janua.yaml.golden"
    "infra/argocd/apps/dhanam.yaml:argocd/dhanam.yaml.golden"
)

# Copy each config
for config in "${CONFIGS[@]}"; do
    source="${config%%:*}"
    dest="${config##*:}"
    source_path="${REPO_ROOT}/${source}"
    dest_path="${GOLDEN_DIR}/${dest}"

    if [[ -f "${source_path}" ]]; then
        cp "${source_path}" "${dest_path}"
        echo -e "${GREEN}  Updated: ${dest}${NC}"
    else
        echo -e "${RED}  Missing: ${source}${NC}"
    fi
done

echo ""
echo -e "${GREEN}Golden configs updated successfully!${NC}"
echo ""
echo "Next steps:"
echo "  1. Commit the updated golden configs"
echo "  2. Push to trigger CI validation"
