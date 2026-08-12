#!/usr/bin/env bash
#
# The secrets-intake registry and the switchyard-secret-writer Vault policy are
# two copies of one truth: every vault_path a registry target writes to must be
# writable under the policy. They drifted exactly once — nauta/oidc-janua landed
# in the registry (enclii#379) with no matching policy path — and the failure
# shape was maximally misleading: Janua reconciled fine, the Vault merge got a
# 403, and the CLI surfaced "API error 500: failed to write to Vault" on the
# FIRST live use, days after the green merge.
#
# This check makes that drift a CI failure at the PR that introduces it.
# It intentionally checks one direction only: extra policy paths (janua,
# madfam-site, pgbackrest-r2, staging variants) are legitimate — other flows
# write there. A registry target without a policy path is always a bug.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REGISTRY="$REPO_ROOT/apps/switchyard-api/internal/secretsintake/registry.yaml"
POLICY_SCRIPT="$REPO_ROOT/scripts/provision-switchyard-vault-writer.sh"

[ -r "$REGISTRY" ] || { echo "FAIL: $REGISTRY not readable"; exit 1; }
[ -r "$POLICY_SCRIPT" ] || { echo "FAIL: $POLICY_SCRIPT not readable"; exit 1; }

# vault_path in the registry is `secret/<name>` (logical); the policy speaks
# KV-v2 API paths, `secret/data/<name>`. Normalize both to `<name>`.
registry_paths=$(grep -oE 'vault_path: *secret/[a-z0-9-]+' "$REGISTRY" \
  | sed 's|.*secret/||' | sort -u)
policy_paths=$(grep -oE 'path "secret/data/[a-z0-9-]+"' "$POLICY_SCRIPT" \
  | sed 's|path "secret/data/||;s|"||' | sort -u)

reg_count=$(printf '%s\n' "$registry_paths" | grep -c . || true)
pol_count=$(printf '%s\n' "$policy_paths" | grep -c . || true)

# Read-proof: a parse that finds nothing must fail rather than vacuously pass.
if [ "$reg_count" -eq 0 ]; then
  echo "FAIL: parsed zero vault_path entries from the registry — the parse is broken, not the data."
  exit 1
fi
if [ "$pol_count" -eq 0 ]; then
  echo "FAIL: parsed zero policy paths from the provision script — the parse is broken, not the data."
  exit 1
fi

missing=$(comm -23 <(printf '%s\n' "$registry_paths") <(printf '%s\n' "$policy_paths") || true)

echo "intake-policy parity: registry=$reg_count path(s), policy=$pol_count path(s)"

if [ -n "$missing" ]; then
  echo
  while IFS= read -r m; do
    [ -n "$m" ] || continue
    echo "  MISSING: registry writes secret/$m but switchyard-secret-writer has no path \"secret/data/$m\""
  done <<< "$missing"
  echo
  echo "FAIL: an intake target exists whose Vault write the policy will 403."
  echo "Add the path block to scripts/provision-switchyard-vault-writer.sh, and"
  echo "re-apply the policy to the running Vault (POLICY_ONLY=1, admin token):"
  echo "  VAULT_TOKEN=<admin> POLICY_ONLY=1 bash scripts/provision-switchyard-vault-writer.sh"
  exit 1
fi

echo "OK: every registry vault_path is writable under the policy."
