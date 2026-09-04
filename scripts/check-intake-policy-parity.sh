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
#
# The registry is not the only producer of Vault paths. Operator provisioners
# reach Vault directly, with the path as a Go literal and no registry entry:
# `enclii secrets provision kalya-feed` READS secret/kalya:internal_api_key to
# authorize minting, then writes secret/crea-map and secret/nauta. secret/kalya
# had no policy path until 2026-09-04, which is the nauta failure again in a
# lane the registry-only check could not see — a denied READ is as opaque as a
# denied write. So switchyard-api Go sources are scanned as a second source of
# truth alongside the registry.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REGISTRY="$REPO_ROOT/apps/switchyard-api/internal/secretsintake/registry.yaml"
POLICY_SCRIPT="$REPO_ROOT/scripts/provision-switchyard-vault-writer.sh"
GOSRC_DIR="$REPO_ROOT/apps/switchyard-api/internal"

[ -r "$REGISTRY" ] || { echo "FAIL: $REGISTRY not readable"; exit 1; }
[ -r "$POLICY_SCRIPT" ] || { echo "FAIL: $POLICY_SCRIPT not readable"; exit 1; }
[ -d "$GOSRC_DIR" ] || { echo "FAIL: $GOSRC_DIR not a directory"; exit 1; }

# vault_path in the registry is `secret/<name>` (logical); the policy speaks
# KV-v2 API paths, `secret/data/<name>`. Normalize both to `<name>`.
registry_paths=$(grep -oE 'vault_path: *secret/[a-z0-9-]+' "$REGISTRY" \
  | sed 's|.*secret/||' | sort -u)
policy_paths=$(grep -oE 'path "secret/data/[a-z0-9-]+"' "$POLICY_SCRIPT" \
  | sed 's|path "secret/data/||;s|"||' | sort -u)

# Go literals: `= "secret/kalya"` / `VaultPath:  "secret/crea-map"`. Tests are
# excluded — a fixture path is not a live Vault reach.
gosrc_paths=$(grep -rhoE '"secret/[a-z0-9-]+"' --include='*.go' "$GOSRC_DIR" \
  --exclude='*_test.go' \
  | tr -d '"' | sed 's|^secret/||' | sort -u)

reg_count=$(printf '%s\n' "$registry_paths" | grep -c . || true)
pol_count=$(printf '%s\n' "$policy_paths" | grep -c . || true)
src_count=$(printf '%s\n' "$gosrc_paths" | grep -c . || true)

# Read-proof: a parse that finds nothing must fail rather than vacuously pass.
if [ "$reg_count" -eq 0 ]; then
  echo "FAIL: parsed zero vault_path entries from the registry — the parse is broken, not the data."
  exit 1
fi
if [ "$pol_count" -eq 0 ]; then
  echo "FAIL: parsed zero policy paths from the provision script — the parse is broken, not the data."
  exit 1
fi
if [ "$src_count" -eq 0 ]; then
  echo "FAIL: parsed zero Vault paths from switchyard-api Go sources — the parse is broken, not the data."
  echo "      (kalya_feed_provisioner.go holds at least secret/kalya, secret/crea-map, secret/nauta.)"
  exit 1
fi

missing=$(comm -23 <(printf '%s\n' "$registry_paths") <(printf '%s\n' "$policy_paths") || true)
missing_src=$(comm -23 <(printf '%s\n' "$gosrc_paths") <(printf '%s\n' "$policy_paths") || true)

echo "intake-policy parity: registry=$reg_count path(s), gosrc=$src_count path(s), policy=$pol_count path(s)"

if [ -n "$missing" ] || [ -n "$missing_src" ]; then
  echo
  while IFS= read -r m; do
    [ -n "$m" ] || continue
    echo "  MISSING: registry writes secret/$m but switchyard-secret-writer has no path \"secret/data/$m\""
  done <<< "$missing"
  while IFS= read -r m; do
    [ -n "$m" ] || continue
    echo "  MISSING: switchyard-api Go source reaches secret/$m but switchyard-secret-writer has no path \"secret/data/$m\""
  done <<< "$missing_src"
  echo
  echo "FAIL: a Vault path is reached whose read/write the policy will 403."
  echo "Add the path block to scripts/provision-switchyard-vault-writer.sh, and"
  echo "re-apply the policy to the running Vault (POLICY_ONLY=1, admin token):"
  echo "  VAULT_TOKEN=<admin> POLICY_ONLY=1 bash scripts/provision-switchyard-vault-writer.sh"
  exit 1
fi

echo "OK: every registry vault_path and every switchyard-api Vault path is covered by the policy."
