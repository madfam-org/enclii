#!/usr/bin/env bash
# Provision Vault policy + enclii/vault-credentials for Switchyard secret intake.
#
# Requires a Vault admin token (root or policy writer). Token is read from
# VAULT_TOKEN or VAULT_TOKEN_FILE and is never printed.
#
# Usage:
#   VAULT_TOKEN_FILE=~/.config/madfam/vault-admin.token \
#     ./scripts/provision-switchyard-vault-writer.sh
#
# Last Updated: 2026-06-15
set -euo pipefail

VAULT_NS="${VAULT_NS:-vault}"
VAULT_POD="${VAULT_POD:-vault-0}"
ENCLII_NS="${ENCLII_NS:-enclii}"
SECRET_NAME="${SECRET_NAME:-vault-credentials}"
VAULT_ADDR="${VAULT_ADDR:-http://vault.vault.svc.cluster.local:8200}"
POLICY_NAME="${POLICY_NAME:-switchyard-secret-writer}"
TOKEN_DISPLAY_NAME="${TOKEN_DISPLAY_NAME:-switchyard-api-intake}"
TTL="${TTL:-8760h}"

log() { printf '[INFO] %s\n' "$*"; }
die() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }

command -v kubectl >/dev/null || die "kubectl required"

if [[ -z "${VAULT_TOKEN:-}" ]]; then
  if [[ -n "${VAULT_TOKEN_FILE:-}" ]]; then
    [[ -r "$VAULT_TOKEN_FILE" ]] || die "VAULT_TOKEN_FILE not readable"
    VAULT_TOKEN="$(tr -d '\r\n' < "$VAULT_TOKEN_FILE")"
    export VAULT_TOKEN
  else
    die "Set VAULT_TOKEN or VAULT_TOKEN_FILE"
  fi
fi

vault_exec() {
  kubectl exec -n "$VAULT_NS" "$VAULT_POD" -- \
    env "VAULT_TOKEN=${VAULT_TOKEN}" vault "$@"
}

POLICY_HCL="$(mktemp)"
trap 'rm -f "$POLICY_HCL"' EXIT
cat >"$POLICY_HCL" <<'EOF'
# Switchyard secret intake + vault-backfill writer (P0)
# Merge into platform secret paths; list/read metadata for merge semantics.
path "secret/data/ceq" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/ceq/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/dhanam" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/dhanam/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/enclii" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/enclii/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/comms" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/comms/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/pgbackrest-r2" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/pgbackrest-r2/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/karafiel" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/karafiel/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/phynd-crm" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/phynd-crm/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/metadata/ceq" {
  capabilities = ["read", "list"]
}
path "secret/metadata/ceq/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/dhanam" {
  capabilities = ["read", "list"]
}
path "secret/metadata/dhanam/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/enclii" {
  capabilities = ["read", "list"]
}
path "secret/metadata/enclii/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/comms" {
  capabilities = ["read", "list"]
}
path "secret/metadata/comms/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/pgbackrest-r2" {
  capabilities = ["read", "list"]
}
path "secret/metadata/pgbackrest-r2/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/karafiel" {
  capabilities = ["read", "list"]
}
path "secret/metadata/karafiel/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/phynd-crm" {
  capabilities = ["read", "list"]
}
path "secret/metadata/phynd-crm/*" {
  capabilities = ["read", "list"]
}
EOF

log "Writing Vault policy ${POLICY_NAME}..."
kubectl cp "$POLICY_HCL" "${VAULT_NS}/${VAULT_POD}:/tmp/${POLICY_NAME}.hcl"
vault_exec policy write "$POLICY_NAME" "/tmp/${POLICY_NAME}.hcl"

log "Creating scoped token (TTL=${TTL})..."
token_json="$(vault_exec token create \
  -policy="$POLICY_NAME" \
  -display-name="$TOKEN_DISPLAY_NAME" \
  -ttl="$TTL" \
  -renewable=true \
  -format=json)"

writer_token="$(printf '%s' "$token_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['auth']['client_token'])")"
[[ -n "$writer_token" ]] || die "failed to parse writer token from vault response"

log "Upserting Kubernetes secret ${ENCLII_NS}/${SECRET_NAME}..."
kubectl -n "$ENCLII_NS" create secret generic "$SECRET_NAME" \
  --from-literal=address="$VAULT_ADDR" \
  --from-literal=token="$writer_token" \
  --dry-run=client -o yaml | kubectl apply -f -

log "Rolling switchyard-api to pick up vault-credentials..."
kubectl -n "$ENCLII_NS" rollout restart deploy/switchyard-api
kubectl -n "$ENCLII_NS" rollout status deploy/switchyard-api --timeout=180s

log "Done. Verify: enclii secrets intake targets (requires admin ENCLII_TOKEN)"
