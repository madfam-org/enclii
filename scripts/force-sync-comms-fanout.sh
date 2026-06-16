#!/usr/bin/env bash
# Force-sync ExternalSecrets that fan out secret/comms (shared Resend).
# Safe to run after platform/comms-resend-api-key intake or Vault rotation.
#
# Usage:
#   export KUBECONFIG=~/.kube/config-hetzner
#   ./scripts/force-sync-comms-fanout.sh

set -euo pipefail

TS="$(date +%s)"

sync_es() {
  local ns="$1"
  local name="$2"
  if kubectl -n "$ns" get externalsecret "$name" >/dev/null 2>&1; then
    kubectl -n "$ns" annotate externalsecret "$name" "force-sync=$TS" --overwrite >/dev/null
    echo "force-sync $ns/$name"
  else
    echo "skip (missing) $ns/$name"
  fi
}

sync_es enclii enclii-secrets
sync_es janua janua-secrets
sync_es madfam-site madfam-site-secrets
sync_es phynd-crm phynd-crm-secrets
sync_es phynd-crm-staging phynd-crm-staging-secrets

# Legacy bridge — remove from kustomization after secret/comms is populated and verified
sync_es enclii enclii-resend-api-key

echo "Done. Check: kubectl get externalsecret -A | rg -i 'comms|resend|enclii-secrets|janua-secrets'"
