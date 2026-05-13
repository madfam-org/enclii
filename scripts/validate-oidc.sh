#!/bin/bash
# validate-oidc.sh - Validate OIDC credentials before deployment
#
# This script verifies that the OIDC client credentials in the K8s secret
# match what's registered in Janua SSO. Run this BEFORE deploying or after
# rotating secrets to catch mismatches early.
#
# Usage:
#   ./scripts/validate-oidc.sh                    # Uses KUBECONFIG from env
#   KUBECONFIG=~/.kube/config-hetzner ./scripts/validate-oidc.sh
#
# Exit codes:
#   0 - OIDC credentials are valid
#   1 - Script error (missing dependencies, etc.)
#   2 - OIDC credentials are invalid or expired
#   3 - Cannot reach Janua SSO

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

NAMESPACE="${ENCLII_NAMESPACE:-enclii}"
SECRET_NAME="${ENCLII_OIDC_SECRET:-enclii-oidc-credentials}"
JANUA_ISSUER="${JANUA_ISSUER:-https://auth.madfam.io}"
JANUA_TOKEN_ENDPOINT="${JANUA_ISSUER}/oauth/token"

echo "================================================"
echo "  OIDC Credential Validation"
echo "================================================"
echo ""

# Check dependencies
for cmd in kubectl curl jq base64; do
    if ! command -v "$cmd" &> /dev/null; then
        echo -e "${RED}ERROR: Required command '$cmd' not found${NC}"
        exit 1
    fi
done

# Step 1: Check if secret exists
echo "1. Checking K8s secret exists..."
if ! kubectl -n "$NAMESPACE" get secret "$SECRET_NAME" &> /dev/null; then
    echo -e "${RED}ERROR: Secret '$SECRET_NAME' not found in namespace '$NAMESPACE'${NC}"
    echo ""
    echo "To create the secret:"
    echo "  kubectl -n $NAMESPACE create secret generic $SECRET_NAME \\"
    echo "    --from-literal=client-id=<CLIENT_ID> \\"
    echo "    --from-literal=client-secret=<CLIENT_SECRET>"
    exit 2
fi
echo -e "${GREEN}   Secret exists${NC}"

# Step 2: Extract credentials
echo "2. Extracting credentials from secret..."
CLIENT_ID=$(kubectl -n "$NAMESPACE" get secret "$SECRET_NAME" -o jsonpath='{.data.client-id}' | base64 -d)
CLIENT_SECRET=$(kubectl -n "$NAMESPACE" get secret "$SECRET_NAME" -o jsonpath='{.data.client-secret}' | base64 -d)

if [[ -z "$CLIENT_ID" || -z "$CLIENT_SECRET" ]]; then
    echo -e "${RED}ERROR: client-id or client-secret is empty${NC}"
    exit 2
fi

PLACEHOLDER_VALUE="REPLACE_WITH_""ACTUAL_SECRET"
if [[ "$CLIENT_SECRET" == "$PLACEHOLDER_VALUE" || "$CLIENT_SECRET" == "example-client-secret" ]]; then
    echo -e "${RED}ERROR: client-secret is still placeholder value${NC}"
    exit 2
fi

echo -e "${GREEN}   Extracted client-id: ${CLIENT_ID:0:10}...${NC}"

# Step 3: Check Janua is reachable
echo "3. Checking Janua SSO is reachable..."
if ! curl -s --connect-timeout 5 "${JANUA_ISSUER}/.well-known/openid-configuration" > /dev/null; then
    echo -e "${RED}ERROR: Cannot reach Janua SSO at $JANUA_ISSUER${NC}"
    exit 3
fi
echo -e "${GREEN}   Janua SSO is reachable${NC}"

# Step 4: Validate credentials with client_credentials grant
echo "4. Validating credentials with Janua..."
RESPONSE=$(curl -s -w "\n%{http_code}" \
    -X POST "$JANUA_TOKEN_ENDPOINT" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "grant_type=client_credentials" \
    -d "client_id=$CLIENT_ID" \
    -d "client_secret=$CLIENT_SECRET" \
    -d "scope=openid")

HTTP_CODE=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | head -n -1)

if [[ "$HTTP_CODE" == "200" ]]; then
    echo -e "${GREEN}   Credentials are VALID${NC}"
    echo ""
    echo "================================================"
    echo -e "${GREEN}  OIDC Validation: PASSED${NC}"
    echo "================================================"
    echo ""
    echo "Client ID: $CLIENT_ID"
    echo "Issuer:    $JANUA_ISSUER"
    echo ""
    exit 0
elif [[ "$HTTP_CODE" == "401" ]]; then
    ERROR_MSG=$(echo "$BODY" | jq -r '.error.message // .error_description // .error // "Unknown error"')
    echo -e "${RED}   Credentials are INVALID: $ERROR_MSG${NC}"
    echo ""
    echo "================================================"
    echo -e "${RED}  OIDC Validation: FAILED${NC}"
    echo "================================================"
    echo ""
    echo "The client_secret in K8s does not match Janua."
    echo ""
    echo "To fix this:"
    echo "  1. Go to https://auth.madfam.io admin panel"
    echo "  2. Find OAuth client: $CLIENT_ID"
    echo "  3. Regenerate or copy the client secret"
    echo "  4. Run: ./scripts/rotate-oidc-secret.sh"
    echo ""
    exit 2
else
    echo -e "${YELLOW}   Unexpected response (HTTP $HTTP_CODE): $BODY${NC}"
    exit 1
fi
