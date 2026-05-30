#!/usr/bin/env bash
# O-10 — backfill enclii K8s secrets into Vault for merge ESO sync.
#
# Writes normalized keys from enclii/enclii-secrets into secret/enclii (including
# internal_api_key for enclii-internal-api-key ExternalSecret). Does not print values.
#
# Usage:
#   VAULT_TOKEN=<write-capable-token> ./scripts/ga-o10-enclii-vault-backfill.sh
#   VAULT_TOKEN_FILE=/secure/vault.token ./scripts/ga-o10-enclii-vault-backfill.sh
#
# After success:
#   kubectl -n enclii annotate externalsecret enclii-internal-api-key \
#     force-sync=$(date +%s) --overwrite

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BRIDGE_K8S_NAME="${ENCLII_INTERNAL_API_KEY_BRIDGE:-enclii-internal-api-key-source}"

bootstrap_k8s_bridge() {
  if kubectl -n enclii get secret "$BRIDGE_K8S_NAME" >/dev/null 2>&1; then
    echo "Bridge secret $BRIDGE_K8S_NAME already exists (kubernetes-store)"
    return 0
  fi
  if ! kubectl -n enclii get secret enclii-secrets >/dev/null 2>&1; then
    echo "enclii-secrets not found — cannot bootstrap $BRIDGE_K8S_NAME" >&2
    return 1
  fi
  echo "Bootstrapping $BRIDGE_K8S_NAME from enclii-secrets (no values printed)..."
  tmp="$(mktemp)"
  trap 'rm -f "$tmp"' RETURN
  kubectl -n enclii get secret enclii-secrets -o "jsonpath={.data.internal-api-key}" | base64 -d >"$tmp"
  prop="internal-api-key"
  kubectl -n enclii create secret generic "$BRIDGE_K8S_NAME" \
    --from-file="${prop}=${tmp}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
}

echo "=== O-10 Enclii secrets sync ==="
bootstrap_k8s_bridge

if [[ -n "${VAULT_TOKEN:-}" || -n "${VAULT_TOKEN_FILE:-}" ]]; then
  echo
  echo "Vault backfill requested..."
  "${ROOT}/scripts/backfill-vault-path-from-k8s-secret.sh" \
    --namespace enclii \
    --secret enclii-secrets \
    --vault-path secret/enclii
else
  echo "Skipping Vault backfill (set VAULT_TOKEN to write secret/enclii/internal_api_key)"
fi

echo
echo "Refreshing merge ExternalSecret..."
if kubectl get externalsecret enclii-internal-api-key -n enclii >/dev/null 2>&1; then
  kubectl -n enclii annotate externalsecret enclii-internal-api-key \
    "force-sync=$(date +%s)" --overwrite >/dev/null
  sleep 5
  reason="$(kubectl -n enclii get externalsecret enclii-internal-api-key \
    -o jsonpath='{.status.conditions[0].reason}' 2>/dev/null || echo unknown)"
  echo "enclii-internal-api-key ESO status: ${reason}"
  if [ "$reason" = "SecretSynced" ]; then
    echo "O-10 merge ESO sync OK"
  else
    echo "ESO not Ready yet — describe externalsecret enclii-internal-api-key -n enclii"
  fi
else
  echo "ExternalSecret enclii-internal-api-key not found — sync core-services Argo app first"
fi
