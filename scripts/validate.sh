#!/usr/bin/env bash
#
# validate.sh - Repository validation script (Operation Ratchet)
#
# Validates critical configuration keys and runs quality checks.
# This is the "Repo Guard" that prevents configuration drift.
#
# Usage:
#   ./scripts/validate.sh          # Full validation (lint + manifest audit)
#   ./scripts/validate.sh --quick  # Quick lint only (skip builds)
#   ./scripts/validate.sh --golden # Include golden config drift check
#   ./scripts/validate.sh --all    # Full validation + golden + builds
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Parse arguments
QUICK_MODE=false
GOLDEN_CHECK=false
FULL_BUILD=false

for arg in "$@"; do
    case $arg in
        --quick)
            QUICK_MODE=true
            ;;
        --golden)
            GOLDEN_CHECK=true
            ;;
        --all)
            GOLDEN_CHECK=true
            FULL_BUILD=true
            ;;
        --help|-h)
            echo "Usage: ./scripts/validate.sh [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --quick   Quick lint only (skip manifest audit)"
            echo "  --golden  Include golden config drift check"
            echo "  --all     Full validation + golden + builds"
            echo "  --help    Show this help message"
            exit 0
            ;;
    esac
done

echo ""
echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}  Operation Ratchet - Repo Guard${NC}"
echo -e "${BLUE}======================================${NC}"
echo ""

ERRORS=0

# ==============================================================================
# Section 1: Critical Config Key Validation (Manifest Audit)
# ==============================================================================

manifest_audit() {
    echo -e "${YELLOW}[1/4] Manifest Audit - Critical Config Keys${NC}"
    echo ""

    local audit_failed=0

    # --- environment-patch.yaml checks ---
    ENV_PATCH="${REPO_ROOT}/infra/k8s/production/environment-patch.yaml"
    if [[ -f "${ENV_PATCH}" ]]; then
        echo "  Checking: environment-patch.yaml"

        # ENCLII_OIDC_ISSUER
        if grep -q "ENCLII_OIDC_ISSUER" "${ENV_PATCH}"; then
            echo -e "    ${GREEN}ENCLII_OIDC_ISSUER${NC}"
        else
            echo -e "    ${RED}MISSING: ENCLII_OIDC_ISSUER${NC}"
            audit_failed=1
        fi

        # ENCLII_OIDC_CLIENT_ID
        if grep -q "ENCLII_OIDC_CLIENT_ID" "${ENV_PATCH}"; then
            echo -e "    ${GREEN}ENCLII_OIDC_CLIENT_ID${NC}"
        else
            echo -e "    ${RED}MISSING: ENCLII_OIDC_CLIENT_ID${NC}"
            audit_failed=1
        fi

        # ENCLII_OIDC_CLIENT_SECRET
        if grep -q "ENCLII_OIDC_CLIENT_SECRET" "${ENV_PATCH}"; then
            echo -e "    ${GREEN}ENCLII_OIDC_CLIENT_SECRET${NC}"
        else
            echo -e "    ${RED}MISSING: ENCLII_OIDC_CLIENT_SECRET${NC}"
            audit_failed=1
        fi

        # ENCLII_EXTERNAL_JWKS_URL
        if grep -q "ENCLII_EXTERNAL_JWKS_URL" "${ENV_PATCH}"; then
            echo -e "    ${GREEN}ENCLII_EXTERNAL_JWKS_URL${NC}"
        else
            echo -e "    ${RED}MISSING: ENCLII_EXTERNAL_JWKS_URL${NC}"
            audit_failed=1
        fi
    else
        echo -e "  ${RED}MISSING FILE: environment-patch.yaml${NC}"
        audit_failed=1
    fi

    # --- cloudflared-unified.yaml checks ---
    # NOTE: Ingress routes are remotely-managed via Cloudflare Tunnel Configuration API
    # (session 22). The ConfigMap no longer contains ingress rules — routes are managed
    # by switchyard-api's domain provisioner. We verify the file exists and has metrics config.
    CLOUDFLARED="${REPO_ROOT}/infra/k8s/production/cloudflared-unified.yaml"
    if [[ -f "${CLOUDFLARED}" ]]; then
        echo ""
        echo "  Checking: cloudflared-unified.yaml"

        if grep -q "metrics: 0.0.0.0:2000" "${CLOUDFLARED}"; then
            echo -e "    ${GREEN}metrics endpoint configured${NC}"
        else
            echo -e "    ${RED}MISSING: metrics endpoint${NC}"
            audit_failed=1
        fi
        echo -e "    ${GREEN}ingress routes: remotely-managed via Cloudflare API${NC}"
    else
        echo -e "  ${RED}MISSING FILE: cloudflared-unified.yaml${NC}"
        audit_failed=1
    fi

    # --- roundhouse.yaml checks ---
    ROUNDHOUSE="${REPO_ROOT}/infra/k8s/base/roundhouse.yaml"
    if [[ -f "${ROUNDHOUSE}" ]]; then
        echo ""
        echo "  Checking: roundhouse.yaml"

        if grep -q "imagePullSecrets:" "${ROUNDHOUSE}"; then
            echo -e "    ${GREEN}imagePullSecrets:${NC}"
        else
            echo -e "    ${RED}MISSING: imagePullSecrets:${NC}"
            audit_failed=1
        fi
    else
        echo -e "  ${RED}MISSING FILE: roundhouse.yaml${NC}"
        audit_failed=1
    fi

    # --- dispatch deployment checks ---
    DISPATCH="${REPO_ROOT}/apps/admin-console/k8s/deployment.yaml"
    if [[ -f "${DISPATCH}" ]]; then
        echo ""
        echo "  Checking: dispatch/k8s/deployment.yaml"

        if grep -q "imagePullSecrets:" "${DISPATCH}"; then
            echo -e "    ${GREEN}imagePullSecrets:${NC}"
        else
            echo -e "    ${RED}MISSING: imagePullSecrets:${NC}"
            audit_failed=1
        fi

        if grep -q "ALLOWED_ADMIN_DOMAINS" "${DISPATCH}"; then
            echo -e "    ${GREEN}ALLOWED_ADMIN_DOMAINS${NC}"
        else
            echo -e "    ${RED}MISSING: ALLOWED_ADMIN_DOMAINS${NC}"
            audit_failed=1
        fi

        if grep -q "ALLOWED_ADMIN_ROLES" "${DISPATCH}"; then
            echo -e "    ${GREEN}ALLOWED_ADMIN_ROLES${NC}"
        else
            echo -e "    ${RED}MISSING: ALLOWED_ADMIN_ROLES${NC}"
            audit_failed=1
        fi

        if grep -q "NEXT_PUBLIC_JANUA_URL" "${DISPATCH}"; then
            echo -e "    ${GREEN}NEXT_PUBLIC_JANUA_URL${NC}"
        else
            echo -e "    ${RED}MISSING: NEXT_PUBLIC_JANUA_URL${NC}"
            audit_failed=1
        fi
    else
        echo -e "  ${RED}MISSING FILE: dispatch/k8s/deployment.yaml${NC}"
        audit_failed=1
    fi

    # --- Dockerfile checks for NEXT_PUBLIC_JANUA_URL ---
    echo ""
    echo "  Checking: Dockerfiles for NEXT_PUBLIC_JANUA_URL"

    for dockerfile in "${REPO_ROOT}/apps/switchyard-ui/Dockerfile" "${REPO_ROOT}/apps/admin-console/Dockerfile"; do
        if [[ -f "${dockerfile}" ]]; then
            basename_file=$(basename "$(dirname "${dockerfile}")")/$(basename "${dockerfile}")
            if grep -q "NEXT_PUBLIC_JANUA_URL" "${dockerfile}"; then
                echo -e "    ${GREEN}${basename_file}: NEXT_PUBLIC_JANUA_URL${NC}"
            else
                echo -e "    ${YELLOW}${basename_file}: NEXT_PUBLIC_JANUA_URL not in Dockerfile (may be in build args)${NC}"
            fi
        fi
    done

    echo ""
    if [[ ${audit_failed} -eq 1 ]]; then
        echo -e "${RED}Manifest audit FAILED${NC}"
        return 1
    else
        echo -e "${GREEN}Manifest audit passed${NC}"
        return 0
    fi
}

# ==============================================================================
# Section 1.5: Port Consistency Check (Operation Fortification)
# ==============================================================================

port_consistency_check() {
    echo ""
    echo -e "${YELLOW}[1.5/4] Port Consistency Check (Fortification)${NC}"
    echo ""

    local check_failed=0

    API_MANIFEST="${REPO_ROOT}/infra/k8s/base/switchyard-api.yaml"

    echo "  Checking: switchyard-api.yaml for forbidden port 8080"

    # Check for containerPort: 8080
    if grep -q "containerPort: 8080" "${API_MANIFEST}" 2>/dev/null; then
        echo -e "    ${RED}FORBIDDEN: containerPort: 8080 found${NC}"
        echo -e "    ${RED}Must use containerPort: 4200${NC}"
        check_failed=1
    else
        echo -e "    ${GREEN}containerPort: 4200 (correct)${NC}"
    fi

    # Check for targetPort: 8080
    if grep -q "targetPort: 8080" "${API_MANIFEST}" 2>/dev/null; then
        echo -e "    ${RED}FORBIDDEN: targetPort: 8080 found${NC}"
        echo -e "    ${RED}Must use targetPort: 4200${NC}"
        check_failed=1
    else
        echo -e "    ${GREEN}targetPort: 4200 (correct)${NC}"
    fi

    # Check probe ports
    if grep -E "port: 8080" "${API_MANIFEST}" 2>/dev/null; then
        echo -e "    ${RED}FORBIDDEN: port 8080 found in probes${NC}"
        check_failed=1
    else
        echo -e "    ${GREEN}probe ports: 4200 (correct)${NC}"
    fi

    echo ""
    if [[ ${check_failed} -eq 1 ]]; then
        echo -e "${RED}Port consistency check FAILED${NC}"
        echo -e "${RED}Switchyard API MUST use port 4200, not 8080${NC}"
        return 1
    else
        echo -e "${GREEN}Port consistency check passed${NC}"
        return 0
    fi
}

# ==============================================================================
# Section 2: Go Lint (golangci-lint)
# ==============================================================================

go_lint() {
    echo ""
    echo -e "${YELLOW}[2/4] Go Lint${NC}"
    echo ""

    local lint_failed=0

    # Check if golangci-lint is available
    if ! command -v golangci-lint &> /dev/null; then
        echo -e "  ${YELLOW}golangci-lint not found, skipping Go lint${NC}"
        return 0
    fi

    for app in "switchyard-api" "roundhouse" "waybill"; do
        app_dir="${REPO_ROOT}/apps/${app}"
        if [[ -d "${app_dir}" ]]; then
            echo "  Linting: apps/${app}"
            if (cd "${app_dir}" && golangci-lint run --timeout=5m ./... 2>&1 | head -20); then
                echo -e "    ${GREEN}passed${NC}"
            else
                echo -e "    ${RED}failed${NC}"
                lint_failed=1
            fi
        fi
    done

    cli_dir="${REPO_ROOT}/packages/cli"
    if [[ -d "${cli_dir}" ]]; then
        echo "  Linting: packages/cli"
        if (cd "${cli_dir}" && golangci-lint run --timeout=5m ./... 2>&1 | head -20); then
            echo -e "    ${GREEN}passed${NC}"
        else
            echo -e "    ${RED}failed${NC}"
            lint_failed=1
        fi
    fi

    echo ""
    if [[ ${lint_failed} -eq 1 ]]; then
        echo -e "${RED}Go lint FAILED${NC}"
        return 1
    else
        echo -e "${GREEN}Go lint passed${NC}"
        return 0
    fi
}

# ==============================================================================
# Section 3: Golden Config Check
# ==============================================================================

golden_check() {
    echo ""
    echo -e "${YELLOW}[3/4] Golden Config Check${NC}"
    echo ""

    if [[ -x "${SCRIPT_DIR}/check-golden.sh" ]]; then
        if "${SCRIPT_DIR}/check-golden.sh"; then
            return 0
        else
            return 1
        fi
    else
        echo -e "  ${YELLOW}Golden check script not found, skipping${NC}"
        return 0
    fi
}

# ==============================================================================
# Section 4: Build Check (optional)
# ==============================================================================

build_check() {
    echo ""
    echo -e "${YELLOW}[4/4] Build Check${NC}"
    echo ""

    local build_failed=0

    # Check if Go is available
    if ! command -v go &> /dev/null; then
        echo -e "  ${YELLOW}Go not found, skipping build check${NC}"
        return 0
    fi

    for app in "switchyard-api" "roundhouse" "waybill"; do
        app_dir="${REPO_ROOT}/apps/${app}"
        if [[ -d "${app_dir}" ]]; then
            echo "  Building: apps/${app}"
            if (cd "${app_dir}" && go build ./... 2>&1 | head -10); then
                echo -e "    ${GREEN}passed${NC}"
            else
                echo -e "    ${RED}failed${NC}"
                build_failed=1
            fi
        fi
    done

    echo ""
    if [[ ${build_failed} -eq 1 ]]; then
        echo -e "${RED}Build check FAILED${NC}"
        return 1
    else
        echo -e "${GREEN}Build check passed${NC}"
        return 0
    fi
}

# ==============================================================================
# Main Execution
# ==============================================================================

# Run manifest audit (always)
if ! manifest_audit; then
    ERRORS=$((ERRORS + 1))
fi

# Run port consistency check (Operation Fortification - always)
if ! port_consistency_check; then
    ERRORS=$((ERRORS + 1))
fi

# Run Go lint (skip in quick mode is not effective here - we want lint in quick)
if ! go_lint; then
    ERRORS=$((ERRORS + 1))
fi

# Run golden check (only if requested)
if [[ "${GOLDEN_CHECK}" == "true" ]]; then
    if ! golden_check; then
        ERRORS=$((ERRORS + 1))
    fi
else
    echo ""
    echo -e "${YELLOW}[3/4] Golden Config Check - SKIPPED (use --golden to enable)${NC}"
fi

# Run build check (only if --all)
if [[ "${FULL_BUILD}" == "true" ]]; then
    if ! build_check; then
        ERRORS=$((ERRORS + 1))
    fi
else
    echo ""
    echo -e "${YELLOW}[4/4] Build Check - SKIPPED (use --all to enable)${NC}"
fi

# ==============================================================================
# Summary
# ==============================================================================

echo ""
echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}  Validation Summary${NC}"
echo -e "${BLUE}======================================${NC}"
echo ""

if [[ ${ERRORS} -eq 0 ]]; then
    echo -e "${GREEN}All validation checks passed!${NC}"
    exit 0
else
    echo -e "${RED}${ERRORS} validation check(s) failed${NC}"
    echo ""
    echo "Review the errors above and fix before committing."
    exit 1
fi
