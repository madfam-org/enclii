#!/usr/bin/env bash
# Bootstrap enclii-resend-api-key-source from Janua's existing Resend key.
#
# Copies resend-api-key from janua/janua-secrets into enclii namespace for
# kubernetes-store merge ESO (same bridge pattern as internal-api-key).
# Does not print secret values.
#
# Usage:
#   ./scripts/ga-resend-bridge-bootstrap.sh
#
# After success:
#   kubectl -n enclii annotate externalsecret enclii-resend-api-key \
#     force-sync=$(date +%s) --overwrite

set -euo pipefail

SOURCE_NS="${JANUA_NAMESPACE:-janua}"
SOURCE_NAME="${JANUA_SECRETS_NAME:-janua-secrets}"
SOURCE_KEY="${JANUA_RESEND_KEY:-resend-api-key}"
TARGET_BRIDGE="${ENCLII_RESEND_BRIDGE:-enclii-resend-api-key-source}"
TARGET_NS="${ENCLII_NAMESPACE:-enclii}"

if ! kubectl -n "$SOURCE_NS" get secret "$SOURCE_NAME" >/dev/null 2>&1; then
  echo "Source secret $SOURCE_NS/$SOURCE_NAME not found" >&2
  exit 1
fi

if ! kubectl -n "$SOURCE_NS" get secret "$SOURCE_NAME" \
  -o "jsonpath={.data.${SOURCE_KEY}}" 2>/dev/null | grep -q .; then
  echo "Source key $SOURCE_KEY missing on $SOURCE_NS/$SOURCE_NAME" >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

kubectl -n "$SOURCE_NS" get secret "$SOURCE_NAME" \
  -o "jsonpath={.data.${SOURCE_KEY}}" | base64 -d >"$tmpdir/${SOURCE_KEY}"

kubectl -n "$TARGET_NS" create secret generic "$TARGET_BRIDGE" \
  --from-file="${SOURCE_KEY}=$tmpdir/${SOURCE_KEY}" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

echo "Bridge secret $TARGET_NS/$TARGET_BRIDGE populated from $SOURCE_NS/$SOURCE_NAME (no values printed)"
echo "DEPRECATED: prefer scripts/backfill-resend-vault-key.sh for direct Vault backfill"

if kubectl get externalsecret enclii-resend-api-key -n "$TARGET_NS" >/dev/null 2>&1; then
  kubectl -n "$TARGET_NS" annotate externalsecret enclii-resend-api-key \
    "force-sync=$(date +%s)" --overwrite >/dev/null
  sleep 5
  reason="$(kubectl -n "$TARGET_NS" get externalsecret enclii-resend-api-key \
    -o jsonpath='{.status.conditions[0].reason}' 2>/dev/null || echo unknown)"
  echo "enclii-resend-api-key ESO status: ${reason}"
fi
