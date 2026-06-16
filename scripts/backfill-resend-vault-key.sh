#!/usr/bin/env bash
# Backfill secret/enclii/resend_api_key in Vault from an existing Kubernetes secret.
#
# Preferred over ga-resend-bridge-bootstrap.sh once Vault is the source of truth.
# Does not print secret values.
#
# Usage:
#   ./scripts/backfill-resend-vault-key.sh
#
# Environment:
#   VAULT_ADDR          Vault API address (required)
#   VAULT_TOKEN         Vault token with write to secret/enclii (required)
#   SOURCE_NS           Kubernetes namespace for source secret (default: enclii)
#   SOURCE_K8S_NAME     Kubernetes secret name (default: enclii-secrets)
#   SOURCE_KEY          Key within secret (default: resend-api-key)
#   VAULT_PATH          Vault KV path (default: secret/comms/resend_api_key)

set -euo pipefail

SOURCE_NS="${SOURCE_NS:-enclii}"
SOURCE_K8S_NAME="${SOURCE_K8S_NAME:-enclii-secrets}"
SOURCE_KEY="${SOURCE_KEY:-resend-api-key}"
VAULT_PATH="${VAULT_PATH:-secret/comms/resend_api_key}"

if [[ -z "${VAULT_ADDR:-}" || -z "${VAULT_TOKEN:-}" ]]; then
  echo "VAULT_ADDR and VAULT_TOKEN are required" >&2
  exit 1
fi

if ! kubectl -n "$SOURCE_NS" get secret "$SOURCE_K8S_NAME" >/dev/null 2>&1; then
  echo "Source secret $SOURCE_NS/$SOURCE_K8S_NAME not found" >&2
  exit 1
fi

if ! kubectl -n "$SOURCE_NS" get secret "$SOURCE_K8S_NAME" \
  -o "jsonpath={.data.${SOURCE_KEY}}" 2>/dev/null | grep -q .; then
  echo "Source key $SOURCE_KEY missing on $SOURCE_NS/$SOURCE_K8S_NAME" >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

kubectl -n "$SOURCE_NS" get secret "$SOURCE_K8S_NAME" \
  -o "jsonpath={.data.${SOURCE_KEY}}" | base64 -d >"$tmpdir/value"

vault kv put "$VAULT_PATH" value=@"$tmpdir/value" >/dev/null

echo "Vault path $VAULT_PATH updated from $SOURCE_NS/$SOURCE_K8S_NAME (no values printed)"
echo "Next: ./scripts/force-sync-comms-fanout.sh and retire enclii-resend-api-key bridge if still present"

if kubectl get externalsecret enclii-resend-api-key -n "$SOURCE_NS" >/dev/null 2>&1; then
  kubectl -n "$SOURCE_NS" annotate externalsecret enclii-resend-api-key \
    "force-sync=$(date +%s)" --overwrite >/dev/null
fi
