#!/usr/bin/env bash
# Break-glass: store Verdaccio htpasswd credential for npm-token-rotation CronJob re-login.
# Requires NPM_REGISTRY_PASS in the environment (never commit). Enclii adapter gap until Vault UI.
set -euo pipefail

NS="${NPM_REGISTRY_NAMESPACE:-npm-registry}"
CREDS_NAME="${NPM_ROTATION_CREDS:-npm-token-rotation-creds}"
K8S_KEY="password"

if [ -z "${NPM_REGISTRY_PASS:-}" ]; then
  echo "ERROR: export NPM_REGISTRY_PASS for the registry bot account" >&2
  exit 1
fi

kubectl create secret generic "${CREDS_SECRET}" \
  --namespace "${NS}" \
  --from-literal="${K8S_KEY}=${NPM_REGISTRY_PASS}" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "OK: merged ${K8S_KEY} into ${NS}/${CREDS_SECRET}"
echo "Next: sync Vault npm-madfam-token property ${K8S_KEY} (ExternalSecrets)."
