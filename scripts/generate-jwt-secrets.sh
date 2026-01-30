#!/usr/bin/env bash
# generate-jwt-secrets.sh — Create the shared JWT signing key for switchyard-api.
# Usage:  ./scripts/generate-jwt-secrets.sh [NAMESPACE]
#
# This generates a 2048-bit RSA private key and stores it as the K8s secret
# "jwt-secrets" so that all API replicas sign/verify tokens with the same key.
set -euo pipefail

NAMESPACE="${1:-enclii}"
TMPKEY="$(mktemp)"

cleanup() { rm -f "$TMPKEY"; }
trap cleanup EXIT

echo "==> Generating 2048-bit RSA private key..."
openssl genrsa -out "$TMPKEY" 2048

echo "==> Creating/updating jwt-secrets in namespace $NAMESPACE..."
kubectl create secret generic jwt-secrets \
  --from-literal=jwt-secret="$(openssl rand -hex 32)" \
  --from-file=jwt-private-key="$TMPKEY" \
  -n "$NAMESPACE" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "==> Rolling restart of switchyard-api..."
kubectl rollout restart deployment/switchyard-api -n "$NAMESPACE"
kubectl rollout status deployment/switchyard-api -n "$NAMESPACE" --timeout=120s

echo "==> Validating pods loaded the shared key..."
sleep 5
if kubectl logs -n "$NAMESPACE" -l app=switchyard-api --tail=50 | grep -q "JWT signing key loaded from ENCLII_JWT_PRIVATE_KEY"; then
  echo "✅ All replicas loaded the shared JWT signing key."
else
  echo "⚠️  Could not confirm key loading — check pod logs manually:"
  echo "    kubectl logs -n $NAMESPACE -l app=switchyard-api | grep JWT"
  exit 1
fi
