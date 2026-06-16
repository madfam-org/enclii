#!/usr/bin/env bash
#
# check-zero-touch-boundaries.sh
#
# Enforces the Phase 0 zero-touch boundary:
# - existing app-specific state in Enclii is treated as legacy/adopted state
# - new app-specific Argo, route, status, CORS, secret, monitor, and UI catalog
#   entries must not be added to Enclii without explicitly updating this
#   boundary script

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_ROOT="${ZERO_TOUCH_REPO_ROOT:-${DEFAULT_REPO_ROOT}}"
cd "${REPO_ROOT}"

FAILURES=0

fail() {
    echo -e "${RED}ERROR:${NC} $*"
    FAILURES=1
}

note() {
    echo -e "${YELLOW}NOTE:${NC} $*"
}

contains() {
    local needle="$1"
    shift
    local item
    for item in "$@"; do
        if [[ "${item}" == "${needle}" ]]; then
            return 0
        fi
    done
    return 1
}

require_cmd() {
    local cmd="$1"
    if ! command -v "${cmd}" >/dev/null 2>&1; then
        fail "Required command not found: ${cmd}"
        return 1
    fi
}

CORE_ARGO_PROJECTS=(
    dispatch
    enclii
    npm-registry
    platform-infra
    status-enclii
    status-madfam
)

LEGACY_ARGO_PROJECTS=(
    accionables-madlab
    selva
    avala
    bloom-scroll
    blueprint-harvester
    ceq
    coforma-studio
    converge-dash
    dhanam
    digifab-quoting
    factlas
    forgesight
    forj
    fortuna
    foundry-scout
    janua
    karafiel
    madfam-site
    nuit-one
    phynd-crm
    pravara-mes
    primavera3d
    routecraft
    sim4d
    subtext
    symbiosis-hcm
    tezca
    tulana
    yantra4d
)

ALLOWED_EXTERNAL_SECRET_FILES=(
    arc-runners-secrets.yaml
    selva-secrets.yaml
    cloudflare-secrets.yaml
    data-secrets.yaml
    dhanam-secrets.yaml
    dhanam-secrets-extended.yaml
    enclii-builds-secrets.yaml
    enclii-secrets.yaml
    forgesight-secrets.yaml
    janua-secrets.yaml
    karafiel-secrets.yaml
    kyverno-secrets.yaml
    longhorn-secrets.yaml
    madfam-site-secrets.yaml
    monitoring-secrets.yaml
    npm-registry-secrets.yaml
    pravara-mes-secrets.yaml
    tezca-secrets.yaml
    yantra4d-secrets.yaml
)

ALLOWED_JANUA_ORIGINS=(
    https://admin.enclii.dev
    https://admin.forgesight.quest
    https://admin.janua.dev
    https://api.enclii.dev
    https://api.forgesight.quest
    https://app.enclii.dev
    https://app.forgesight.quest
    https://app.janua.dev
    https://auth.madfam.io
    https://forgesight.quest
    https://sniper.madfam.io
    https://www.forgesight.quest
)

ALLOWED_TUNNEL_HOSTNAMES=(
    admin.dhan.am
    admin.enclii.dev
    admin.forgesight.quest
    admin.karafiel.mx
    admin.tezca.mx
    admin.yantra4d.com
    api.dhan.am
    api.enclii.dev
    api.forgesight.quest
    api.karafiel.mx
    api.phynd.app
    api.tezca.mx
    api.yantra4d.com
    app.dhan.am
    app.enclii.dev
    app.forgesight.quest
    app.karafiel.mx
    app.yantra4d.com
    auth.madfam.io
    cms.madfam.io
    crm.madfam.io
    crm.phynd.app
    dhan.am
    docs.enclii.dev
    enclii.dev
    forgesight.quest
    grafana.enclii.dev
    karafiel.mx
    madfam.io
    mes-admin.madfam.io
    mes-api.madfam.io
    mes.madfam.io
    metrics.enclii.dev
    phynd.app
    sniper.madfam.io
    staging-crm.madfam.io
    status.cotiza.studio
    status.dhan.am
    status.enclii.dev
    status.karafiel.mx
    status.madfam.io
    status.tezca.mx
    tezca.mx
    www.enclii.dev
    www.forgesight.quest
    www.madfam.io
    www.phynd.app
    yantra4d.com
)

ALLOWED_STATUS_URLS=(
    https://admin.dhan.am
    https://admin.enclii.dev
    https://admin.forgesight.quest
    https://admin.karafiel.mx
    https://admin.karafiel.mx/login
    https://admin.routecraft.app
    https://admin.selva.town
    https://admin.selva.town/login
    https://admin.tezca.mx
    https://admin.yantra4d.com
    https://almanac.solar
    https://analytics.madfam.io
    https://analytics.madfam.io/static/array.js
    https://api.almanac.solar
    https://api.almanac.solar/health
    https://api.avala.studio
    https://api.avala.studio/v1/health
    https://api.cotiza.studio
    https://api.cotiza.studio/api/docs
    https://api.dhan.am
    https://api.dhan.am/health
    https://api.enclii.dev
    https://api.enclii.dev/health/public
    https://api.factl.as
    https://api.factl.as/health
    https://api.forgesight.quest
    https://api.fortuna.tube
    https://api.fortuna.tube/health
    https://api.karafiel.mx
    https://api.karafiel.mx/api/v1/health/
    https://api.rondel.io
    https://api.rondel.io/health
    https://api.routecraft.app
    https://api.routecraft.app/health
    https://api.selva.town
    https://api.selva.town/health
    https://api.tezca.mx
    https://api.tezca.mx/health
    https://app.dhan.am
    https://app.enclii.dev
    https://app.factl.as
    https://app.forgesight.quest
    https://app.karafiel.mx
    https://app.selva.town
    https://app.selva.town/demo
    https://app.yantra4d.com
    https://auth.madfam.io
    https://auth.madfam.io/health
    https://avala.studio
    https://blueprint.tube
    https://blueprint.tube/health
    https://ceq.lol
    https://coforma.studio
    https://cotiza.studio
    https://crm.madfam.io
    https://crm.phynd.app
    https://dhan.am
    https://docs.enclii.dev
    https://docs.janua.dev
    https://enclii.dev
    https://factl.as
    https://forgesight.quest
    https://forj.design
    https://fortuna.tube
    https://hcm.madfam.io
    https://janua.dev
    https://karafiel.mx
    https://karafiel.mx/login
    https://madfam.io
    https://mes-api.madfam.io
    https://mes-api.madfam.io/health
    https://mes.madfam.io
    https://npm.madfam.io
    https://nuit.one
    https://phynd.app
    https://phynd.app/api/health
    https://play.rondel.io
    https://primavera3d.pro
    https://rondel.io
    https://routecraft.app
    https://selva.town
    https://sniper.madfam.io
    https://status.madfam.io
    https://tezca.mx
    https://tiles.factl.as
    https://tulana-api.madfam.io
    https://tulana-api.madfam.io/api/v1/health/
    https://tulana-app.madfam.io
    https://tulana.madfam.io
    https://yantra4d.com
)

ALLOWED_DEPLOY_MONITOR_REPOS=(
    madfam-org/dhanam
    madfam-org/digifab-quoting
    madfam-org/enclii
    madfam-org/forgesight
    madfam-org/fortuna
    madfam-org/janua
    madfam-org/karafiel
    madfam-org/madfam-site
    madfam-org/phynd-crm
    madfam-org/pravara-mes
    madfam-org/rondelio
    madfam-org/selva-office
    madfam-org/sim4d
    madfam-org/tezca
)

ALLOWED_DASHBOARD_FRAMEWORK_KEYS=(
    selva-office
    dhanam
    enclii
    forgesight
    janua
    karafiel
    leyes-como-codigo-mx
    madfam-site
    pravara-mes
    tezca
    yantra4d
)

echo "Checking zero-touch boundary invariants..."

require_cmd git
require_cmd jq

echo "  - ArgoCD project config allowlist"
for config in infra/argocd/projects/*/config.json; do
    [[ -f "${config}" ]] || continue
    project="$(basename "$(dirname "${config}")")"
    repo_url="$(jq -r '.repoURL // ""' "${config}")"

    if contains "${project}" "${CORE_ARGO_PROJECTS[@]}" || contains "${project}" "${LEGACY_ARGO_PROJECTS[@]}"; then
        continue
    fi

    fail "New Argo project config is not allowed in Enclii: ${config} (${repo_url}). Add client desired state to the client repo and use runtime reconciliation instead."
done

echo "  - ExternalSecret legacy file allowlist"
for secret_file in infra/k8s/base/external-secrets/vault-secrets/*-secrets.yaml; do
    [[ -f "${secret_file}" ]] || continue
    base="$(basename "${secret_file}")"
    if ! contains "${base}" "${ALLOWED_EXTERNAL_SECRET_FILES[@]}"; then
        fail "New product ExternalSecret file is not allowed in Enclii: ${secret_file}. Declare the secret contract in client desired state instead."
    fi
done

echo "  - Janua CORS/CSRF origin allowlist"
if [[ -f infra/k8s/production/janua-env-config.yaml ]]; then
    while IFS= read -r origin; do
        [[ -n "${origin}" ]] || continue
        if ! contains "${origin}" "${ALLOWED_JANUA_ORIGINS[@]}"; then
            fail "Unexpected Janua origin in Enclii config: ${origin}. Product origins must come from Janua OAuth client metadata."
        fi
    done < <(grep -E '^[[:space:]]+(CORS_ORIGINS|CSRF_TRUSTED_ORIGINS):' infra/k8s/production/janua-env-config.yaml | grep -oE 'https://[^", ]+' | sort -u)
fi

echo "  - Cloudflare tunnel expected hostname allowlist"
if [[ -f infra/k8s/production/expected-tunnel-config.json ]]; then
    while IFS= read -r hostname; do
        [[ -n "${hostname}" ]] || continue
        if ! contains "${hostname}" "${ALLOWED_TUNNEL_HOSTNAMES[@]}"; then
            fail "Unexpected hostname in expected tunnel config: ${hostname}. Product routes must be reconciled from client desired state/runtime inventory."
        fi
    done < <(jq -r '.[] | select(.hostname != null) | .hostname' infra/k8s/production/expected-tunnel-config.json | sort -u)
fi

echo "  - Status URL allowlist"
if [[ -f apps/status/k8s/madfam/configmap.yaml ]]; then
    while IFS= read -r url; do
        [[ -n "${url}" ]] || continue
        if ! contains "${url}" "${ALLOWED_STATUS_URLS[@]}"; then
            fail "Unexpected MADFAM status URL in Enclii configmap: ${url}. Status entries must be sourced from client repo desired state."
        fi
    done < <(grep -oE 'https://[^", ]+' apps/status/k8s/madfam/configmap.yaml | sort -u)
fi

echo "  - Deploy monitor repo allowlist"
if [[ -f infra/k8s/production/monitoring/deploy-pipeline-monitor.yaml ]]; then
    while IFS= read -r repo; do
        [[ -n "${repo}" ]] || continue
        if ! contains "${repo}" "${ALLOWED_DEPLOY_MONITOR_REPOS[@]}"; then
            fail "Unexpected deploy-monitor repo in Enclii config: ${repo}. Monitor targets must be sourced from onboarding/Argo state."
        fi
    done < <(grep -oE '"repo": "[^"]+"' infra/k8s/production/monitoring/deploy-pipeline-monitor.yaml | sed -E 's/.*"repo": "([^"]+)".*/\1/' | sort -u)
fi

echo "  - Dashboard hardcoded framework allowlist"
framework_file="apps/switchyard-ui/components/dashboard/framework-icon.tsx"
if [[ -f "${framework_file}" ]] && grep -q 'KNOWN_REPO_FRAMEWORKS' "${framework_file}"; then
    while IFS= read -r key; do
        [[ -n "${key}" ]] || continue
        if ! contains "${key}" "${ALLOWED_DASHBOARD_FRAMEWORK_KEYS[@]}"; then
            fail "Unexpected hardcoded dashboard framework key: ${key}. Frameworks must come from backend service facts."
        fi
    done < <(
        awk '/^const KNOWN_REPO_FRAMEWORKS/{in_map=1; next} in_map && /^};/{in_map=0} in_map {print}' "${framework_file}" \
            | sed -nE 's/^[[:space:]]*"([^"]+)":[[:space:]].*/\1/p; s/^[[:space:]]*([A-Za-z0-9_-]+):[[:space:]].*/\1/p' \
            | sort -u
    )
fi

echo "  - Active onboarding docs and CLI wording"
FORBIDDEN_MATCHES="$(
    git grep -inE \
        'auto-commit[s]?[[:space:]]+.*(enclii repo|infra/argocd/projects)|auto-committing ArgoCD apps|Project config auto-committed' \
        -- \
        README.md \
        docs/guides/EXTERNAL_REPO_DEPLOY.md \
        docs/guides/ZERO_TOUCH_CONTRACT.md \
        docs/guides/ONBOARDING_GUIDE.md \
        docs/cli/commands/onboard.md \
        packages/cli/internal/cmd/onboard.go \
        2>/dev/null || true
)"
if [[ -n "${FORBIDDEN_MATCHES}" ]]; then
    echo "${FORBIDDEN_MATCHES}"
    fail "Active onboarding docs/CLI still describe Enclii repo commits as the normal zero-touch path. Mark legacy behavior explicitly and point to runtime reconciliation."
fi

if [[ "${FAILURES}" -eq 1 ]]; then
    echo ""
    note "Existing entries in this script are legacy/adopted state. Do not add a new app-specific Enclii entry; put desired state in the client repo and reconcile it at runtime."
    exit 1
fi

echo -e "${GREEN}Zero-touch boundary check passed.${NC}"
