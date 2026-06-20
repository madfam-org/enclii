#!/usr/bin/env bash
# Minimal Vault bootstrap after fresh operator init (audit + KV v2 + eso-reader policy).
# Requires VAULT_TOKEN or VAULT_TOKEN_FILE. Never prints secrets.
#
# Usage:
#   VAULT_TOKEN_FILE=~/.config/madfam/vault-admin.token \
#     ./scripts/vault-rebootstrap-bootstrap.sh
#
# Last Updated: 2026-06-15
set -euo pipefail

VAULT_NS="${VAULT_NS:-vault}"
VAULT_POD="${VAULT_POD:-vault-0}"

log() { printf '[INFO] %s\n' "$*"; }
die() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }

if [[ -z "${VAULT_TOKEN:-}" ]]; then
  [[ -n "${VAULT_TOKEN_FILE:-}" && -r "$VAULT_TOKEN_FILE" ]] \
    || die "Set VAULT_TOKEN or VAULT_TOKEN_FILE"
  VAULT_TOKEN="$(tr -d '\r\n' < "$VAULT_TOKEN_FILE")"
  export VAULT_TOKEN
fi

vault_exec() {
  kubectl exec -i -n "$VAULT_NS" "$VAULT_POD" -- \
    env "VAULT_TOKEN=${VAULT_TOKEN}" vault "$@"
}

log "Enable audit sinks..."
vault_exec audit enable -path=stderr file file_path=/dev/stderr 2>/dev/null \
  || true
vault_exec audit enable -path=file file file_path=/vault/audit/vault_audit.log 2>/dev/null \
  || true
if ! vault_exec audit list 2>/dev/null | rg -q .; then
  die "No audit device enabled — check /dev/stderr and /vault/audit mounts"
fi

log "Enable KV v2 at secret/..."
vault_exec secrets enable -path=secret kv-v2 2>/dev/null || vault_exec secrets list >/dev/null

log "Write eso-reader policy..."
vault_exec policy write eso-reader - <<'EOF'
path "secret/data/*" {
  capabilities = ["read"]
}
path "secret/metadata/*" {
  capabilities = ["read", "list"]
}
EOF

log "Write switchyard-secret-writer policy (also done by provision script)..."
vault_exec policy write switchyard-secret-writer - <<'EOF'
path "secret/data/ceq" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/ceq/*" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/dhanam" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/dhanam/*" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/enclii" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/enclii/*" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/comms" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/comms/*" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/pgbackrest-r2" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/pgbackrest-r2/*" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/karafiel" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/karafiel/*" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/phynd-crm" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/phynd-crm/*" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/coupler" { capabilities = ["create", "update", "patch", "read"] }
path "secret/data/coupler/*" { capabilities = ["create", "update", "patch", "read"] }
path "secret/metadata/ceq" { capabilities = ["read", "list"] }
path "secret/metadata/ceq/*" { capabilities = ["read", "list"] }
path "secret/metadata/dhanam" { capabilities = ["read", "list"] }
path "secret/metadata/dhanam/*" { capabilities = ["read", "list"] }
path "secret/metadata/enclii" { capabilities = ["read", "list"] }
path "secret/metadata/enclii/*" { capabilities = ["read", "list"] }
path "secret/metadata/comms" { capabilities = ["read", "list"] }
path "secret/metadata/comms/*" { capabilities = ["read", "list"] }
path "secret/metadata/pgbackrest-r2" { capabilities = ["read", "list"] }
path "secret/metadata/pgbackrest-r2/*" { capabilities = ["read", "list"] }
path "secret/metadata/karafiel" { capabilities = ["read", "list"] }
path "secret/metadata/karafiel/*" { capabilities = ["read", "list"] }
path "secret/metadata/phynd-crm" { capabilities = ["read", "list"] }
path "secret/metadata/phynd-crm/*" { capabilities = ["read", "list"] }
path "secret/metadata/coupler" { capabilities = ["read", "list"] }
path "secret/metadata/coupler/*" { capabilities = ["read", "list"] }
EOF

log "Bootstrap complete (enable kubernetes auth via repair-vault-eso-auth.sh next)"
