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

# The switchyard-secret-writer policy is NOT re-inlined here.
#
# It used to be: this script kept a third copy of the policy alongside
# scripts/provision-switchyard-vault-writer.sh and the intake registry. Copies
# drift, and this one had — by 2026-09-03 it was missing janua, lexidrop,
# madfam-site, nauta and phynd-crm-staging, every one of which the provision
# script already granted. A stale copy is worse than no copy: it looks like the
# policy is handled while quietly under-granting, and the failure surfaces days
# later as an opaque "500: failed to write to Vault" on first live use.
#
# scripts/check-intake-policy-parity.sh guards the registry against the
# provision script. It cannot guard a copy it does not know about, so the copy
# is gone and the provision script is the single source.
log "Write switchyard-secret-writer policy (delegating to the provision script)..."
POLICY_ONLY=1 VAULT_TOKEN="$VAULT_TOKEN" \
  "$(dirname "${BASH_SOURCE[0]}")/provision-switchyard-vault-writer.sh"

log "Bootstrap complete (enable kubernetes auth via repair-vault-eso-auth.sh next)"
