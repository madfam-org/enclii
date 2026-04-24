#!/usr/bin/env bash
#
# tunnel-routes-test.sh - Verify Terraform tunnel routes match expected config
#
# Compares the service values in cloudflare.tf ingress_rules against the
# expected-tunnel-config.json reference file. Fails on any drift.
#
# Usage: ./tests/golden/infra/tunnel-routes-test.sh
#

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

TF_FILE="${REPO_ROOT}/infra/terraform/cloudflare.tf"
EXPECTED_FILE="${REPO_ROOT}/infra/k8s/production/expected-tunnel-config.json"

if [[ ! -f "${TF_FILE}" ]]; then
    echo -e "${RED}ERROR: ${TF_FILE} not found${NC}"
    exit 1
fi

if [[ ! -f "${EXPECTED_FILE}" ]]; then
    echo -e "${RED}ERROR: ${EXPECTED_FILE} not found${NC}"
    exit 1
fi

# Fail loudly if jq is missing — the script parses JSON via jq below.
# Silent success on missing jq is how this whole script went unnoticed.
if ! command -v jq >/dev/null 2>&1; then
    echo -e "${RED}ERROR: jq is required but not installed${NC}"
    echo "Install with: brew install jq (macOS) or apt install jq (Linux)"
    exit 1
fi

echo "Checking Terraform tunnel routes against expected config..."
echo ""

FAILED=0
CHECKED=0

# Extract hostname→service pairs from expected config (skip entries without hostname, skip comments)
while IFS= read -r line; do
    hostname=$(echo "${line}" | cut -d'|' -f1)
    expected_service=$(echo "${line}" | cut -d'|' -f2)

    # Skip catch-all (no hostname)
    [[ -z "${hostname}" ]] && continue

    CHECKED=$((CHECKED + 1))

    # Search for this hostname's service value in the Terraform file
    # Look for ingress_rule blocks containing this hostname and extract the service line
    # We match the domain portion — Terraform uses variables like ${var.domain}
    # so we extract the raw service value from any ingress_rule in the file
    #
    # Strategy: find the service line in the ingress_rule block that routes this hostname
    # For hostnames like "api.enclii.dev", TF uses "${var.subdomain_api}.${var.domain}"
    # For hostnames like "enclii.dev", TF uses var.domain
    # For hostnames like "metrics.enclii.dev", TF uses "metrics.${var.domain}"

    # Extract the subdomain portion to search in TF
    subdomain="${hostname%%.*}"
    base_domain="${hostname#*.}"

    # Helper: extract quoted service value from an awk-filtered line
    # Works on both GNU and BSD (macOS) — avoids grep -P
    extract_service() {
        sed -n 's/.*"\(http[^"]*\)".*/\1/p' | head -1
    }

    # Find matching service in TF file
    tf_service=""

    if [[ "${hostname}" == "enclii.dev" ]]; then
        # Apex domain: look for `hostname = var.domain` (not www or other subdomains)
        tf_service=$(awk '/hostname[[:space:]]*=[[:space:]]*var\.domain[[:space:]]*$/{found=1} found && /service[[:space:]]*=/{print; found=0}' "${TF_FILE}" | extract_service)
    elif [[ "${hostname}" == "www.enclii.dev" ]]; then
        tf_service=$(awk '/hostname[[:space:]]*=[[:space:]]*"www\./{found=1} found && /service[[:space:]]*=/{print; found=0}' "${TF_FILE}" | extract_service)
    elif [[ "${hostname}" == "metrics.enclii.dev" ]]; then
        tf_service=$(awk '/hostname[[:space:]]*=[[:space:]]*"metrics\./{found=1} found && /service[[:space:]]*=/{print; found=0}' "${TF_FILE}" | extract_service)
    elif [[ "${hostname}" == "grafana.enclii.dev" ]]; then
        tf_service=$(awk '/hostname[[:space:]]*=[[:space:]]*"grafana\./{found=1} found && /service[[:space:]]*=/{print; found=0}' "${TF_FILE}" | extract_service)
    elif [[ "${hostname}" == "api.enclii.dev" ]]; then
        tf_service=$(awk '/subdomain_api/{found=1} found && /service[[:space:]]*=/{print; found=0}' "${TF_FILE}" | extract_service)
    elif [[ "${hostname}" == "app.enclii.dev" ]]; then
        tf_service=$(awk '/subdomain_app/{found=1} found && /service[[:space:]]*=/{print; found=0}' "${TF_FILE}" | extract_service)
    elif [[ "${hostname}" == "docs.enclii.dev" ]]; then
        tf_service=$(awk '/hostname[[:space:]]*=[[:space:]]*"docs\./{found=1} found && /service[[:space:]]*=/{print; found=0}' "${TF_FILE}" | extract_service)
    elif [[ "${hostname}" == "status.enclii.dev" ]]; then
        tf_service=$(awk '/hostname[[:space:]]*=[[:space:]]*"status\./{found=1} found && /service[[:space:]]*=/{print; found=0}' "${TF_FILE}" | extract_service)
    else
        # Hostname not managed in this Terraform file (other domains like dhan.am, tezca.mx, etc.)
        echo -e "${YELLOW}  SKIP: ${hostname} (not in enclii cloudflare.tf)${NC}"
        CHECKED=$((CHECKED - 1))
        continue
    fi

    if [[ -z "${tf_service}" ]]; then
        echo -e "${RED}  MISSING: ${hostname} — not found in Terraform${NC}"
        FAILED=1
        continue
    fi

    if [[ "${tf_service}" == "${expected_service}" ]]; then
        echo -e "${GREEN}  OK: ${hostname} → ${expected_service}${NC}"
    else
        echo -e "${RED}  DRIFT: ${hostname}${NC}"
        echo -e "${RED}    Terraform: ${tf_service}${NC}"
        echo -e "${RED}    Expected:  ${expected_service}${NC}"
        FAILED=1
    fi
done < <(jq -r '.[] | select(.hostname != null) | "\(.hostname)|\(.service)"' "${EXPECTED_FILE}")

echo ""

if [[ ${CHECKED} -eq 0 ]]; then
    echo -e "${YELLOW}No routes checked. Verify expected-tunnel-config.json format.${NC}"
    exit 1
fi

if [[ ${FAILED} -eq 1 ]]; then
    echo -e "${RED}Tunnel route drift detected! (${CHECKED} routes checked)${NC}"
    echo ""
    echo "Fix cloudflare.tf to match expected-tunnel-config.json, then re-run."
    exit 1
fi

echo -e "${GREEN}All tunnel routes match expected config! (${CHECKED} routes checked)${NC}"
