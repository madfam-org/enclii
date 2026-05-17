#!/usr/bin/env bash
# Repair Vault Kubernetes auth for External Secrets Operator.
#
# Requires a Vault token with permission to manage auth/kubernetes, policies,
# and token lookup. The token is read from VAULT_TOKEN and is never printed.

set -euo pipefail

VAULT_NS="${VAULT_NS:-vault}"
VAULT_POD="${VAULT_POD:-vault-0}"
VAULT_AUTH_MOUNT="${VAULT_AUTH_MOUNT:-kubernetes}"
VAULT_ROLE="${VAULT_ROLE:-eso-reader}"
VAULT_POLICY="${VAULT_POLICY:-eso-reader}"
VAULT_KV_PATH="${VAULT_KV_PATH:-secret}"
ESO_NAMESPACE="${ESO_NAMESPACE:-external-secrets}"
ESO_SERVICE_ACCOUNT="${ESO_SERVICE_ACCOUNT:-external-secrets}"
VAULT_AUDIENCE="${VAULT_AUDIENCE:-vault}"
CLUSTER_SECRET_STORE="${CLUSTER_SECRET_STORE:-vault-store}"
PORKBUN_ES_NAMESPACE="${PORKBUN_ES_NAMESPACE:-enclii}"
PORKBUN_ES_NAME="${PORKBUN_ES_NAME:-enclii-porkbun-credentials}"
REFRESH_PORKBUN_ES="${REFRESH_PORKBUN_ES:-true}"

log() { printf '[INFO] %s\n' "$*"; }
ok() { printf '[ OK ] %s\n' "$*"; }
warn() { printf '[WARN] %s\n' "$*" >&2; }
die() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }

vault_exec() {
  kubectl exec -i -n "$VAULT_NS" "$VAULT_POD" -- \
    env "VAULT_TOKEN=${VAULT_TOKEN}" vault "$@"
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

wait_for_store() {
  log "Waiting for ClusterSecretStore/${CLUSTER_SECRET_STORE} to become Ready..."
  if kubectl wait \
    --for=condition=Ready \
    "clustersecretstore/${CLUSTER_SECRET_STORE}" \
    --timeout=60s >/dev/null; then
    ok "ClusterSecretStore/${CLUSTER_SECRET_STORE} is Ready"
    return 0
  fi

  warn "ClusterSecretStore/${CLUSTER_SECRET_STORE} did not become Ready within 60s"
  kubectl get "clustersecretstore/${CLUSTER_SECRET_STORE}" || true
  return 1
}

refresh_porkbun_secret() {
  if [[ "$REFRESH_PORKBUN_ES" != "true" ]]; then
    return 0
  fi

  if ! kubectl -n "$PORKBUN_ES_NAMESPACE" get externalsecret "$PORKBUN_ES_NAME" >/dev/null 2>&1; then
    warn "ExternalSecret ${PORKBUN_ES_NAMESPACE}/${PORKBUN_ES_NAME} does not exist; skipping refresh"
    return 0
  fi

  log "Requesting ExternalSecret refresh for ${PORKBUN_ES_NAMESPACE}/${PORKBUN_ES_NAME}..."
  kubectl -n "$PORKBUN_ES_NAMESPACE" annotate \
    externalsecret "$PORKBUN_ES_NAME" \
    "force-sync=$(date +%s)" \
    --overwrite >/dev/null

  log "Waiting for ${PORKBUN_ES_NAMESPACE}/${PORKBUN_ES_NAME} to become Ready..."
  if kubectl -n "$PORKBUN_ES_NAMESPACE" wait \
    --for=condition=Ready \
    "externalsecret/${PORKBUN_ES_NAME}" \
    --timeout=60s >/dev/null; then
    ok "ExternalSecret ${PORKBUN_ES_NAMESPACE}/${PORKBUN_ES_NAME} is Ready"
    return 0
  fi

  warn "ExternalSecret ${PORKBUN_ES_NAMESPACE}/${PORKBUN_ES_NAME} is still not Ready"
  kubectl -n "$PORKBUN_ES_NAMESPACE" get externalsecret "$PORKBUN_ES_NAME" || true
  return 1
}

main() {
  require_command kubectl

  [[ -n "${VAULT_TOKEN:-}" ]] || die "Set VAULT_TOKEN to an operator-approved Vault token before running"

  log "Checking Vault pod and token..."
  kubectl -n "$VAULT_NS" get pod "$VAULT_POD" >/dev/null
  vault_exec status >/dev/null
  vault_exec token lookup >/dev/null
  ok "Vault pod is reachable and VAULT_TOKEN is valid"

  log "Ensuring ${VAULT_AUTH_MOUNT}/ auth method is enabled..."
  if vault_exec auth list | awk '{print $1}' | grep -qx "${VAULT_AUTH_MOUNT}/"; then
    ok "Vault auth method ${VAULT_AUTH_MOUNT}/ already exists"
  else
    vault_exec auth enable -path="$VAULT_AUTH_MOUNT" kubernetes >/dev/null
    ok "Enabled Vault Kubernetes auth at ${VAULT_AUTH_MOUNT}/"
  fi

  log "Configuring Kubernetes auth endpoint..."
  vault_exec write "auth/${VAULT_AUTH_MOUNT}/config" \
    kubernetes_host="https://kubernetes.default.svc:443" >/dev/null
  ok "Kubernetes auth endpoint configured"

  log "Writing ${VAULT_POLICY} policy for KV path ${VAULT_KV_PATH}/..."
  vault_exec policy write "$VAULT_POLICY" - >/dev/null <<POLICY
path "${VAULT_KV_PATH}/data/*" {
  capabilities = ["read"]
}
path "${VAULT_KV_PATH}/metadata/*" {
  capabilities = ["read", "list"]
}
POLICY
  ok "Vault policy ${VAULT_POLICY} written"

  log "Binding Vault role ${VAULT_ROLE} to ${ESO_NAMESPACE}/${ESO_SERVICE_ACCOUNT}..."
  vault_exec write "auth/${VAULT_AUTH_MOUNT}/role/${VAULT_ROLE}" \
    bound_service_account_names="$ESO_SERVICE_ACCOUNT" \
    bound_service_account_namespaces="$ESO_NAMESPACE" \
    bound_audiences="$VAULT_AUDIENCE" \
    policies="$VAULT_POLICY" \
    ttl=1h >/dev/null
  ok "Vault role ${VAULT_ROLE} written"

  store_ready=true
  wait_for_store || store_ready=false

  porkbun_ready=true
  refresh_porkbun_secret || porkbun_ready=false

  if [[ "$store_ready" == true && "$porkbun_ready" == true ]]; then
    ok "Vault ESO auth repair completed"
  elif [[ "$store_ready" == true ]]; then
    warn "Vault ESO auth is repaired, but Porkbun ExternalSecret is not Ready; check missing Vault properties"
    exit 2
  else
    die "Vault ESO auth repair did not converge; inspect ClusterSecretStore events"
  fi
}

main "$@"
