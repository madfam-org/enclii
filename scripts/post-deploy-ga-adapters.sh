#!/usr/bin/env bash
# Post-deploy smoke for Commercial GA adapter routes (Wave 0–1.5).
#
# Confirms the deployed Switchyard API advertises and accepts dry-run contracts
# for GA-critical ops/provider adapters. Run after Argo syncs enclii-infrastructure.
#
# Usage:
#   export ENCLII_API_TOKEN=...   # admin token
#   ./scripts/post-deploy-ga-adapters.sh
#   API_URL=https://api.enclii.dev ./scripts/post-deploy-ga-adapters.sh

set -euo pipefail

API_URL="${API_URL:-https://api.enclii.dev}"
TOKEN="${ENCLII_API_TOKEN:-${ENCLII_SYNTHETICS_TOKEN:-}}"

if [ -z "$TOKEN" ]; then
  echo "ENCLII_API_TOKEN (admin) is required" >&2
  exit 2
fi

if ! command -v enclii >/dev/null 2>&1; then
  echo "enclii CLI not found in PATH" >&2
  exit 2
fi

export ENCLII_API_ENDPOINT="${ENCLII_API_ENDPOINT:-$API_URL}"
export ENCLII_API_TOKEN="$TOKEN"

PASS=0
FAIL=0

check() {
  local name="$1"
  shift
  if "$@"; then
    echo "[pass] $name"
    PASS=$((PASS + 1))
  else
    echo "[fail] $name"
    FAIL=$((FAIL + 1))
  fi
}

ops_has_action() {
  local domain="$1"
  local action="$2"
  enclii ops capabilities --json | python3 -c "
import json, sys
domain, action = sys.argv[1:3]
payload = json.load(sys.stdin)
for cap in payload.get('capabilities', []):
    if cap.get('name') == domain and action in (cap.get('actions') or []):
        sys.exit(0)
sys.exit(1)
" "$domain" "$action"
}

providers_has_action() {
  local domain="$1"
  local action="$2"
  enclii providers capabilities --json | python3 -c "
import json, sys
domain, action = sys.argv[1:3]
payload = json.load(sys.stdin)
for cap in payload.get('capabilities', []):
    if cap.get('name') == domain and action in (cap.get('actions') or []):
        sys.exit(0)
sys.exit(1)
" "$domain" "$action"
}

dry_run_ok() {
  local path="$1"
  local payload="$2"
  local code
  code="$(curl -sS -o /dev/null -w "%{http_code}" \
    -X POST "${API_URL%/}${path}" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "$payload")" || true
  case "$code" in
    200|202|400|503) return 0 ;;
    *) return 1 ;;
  esac
}

echo "=== Post-deploy GA adapter smoke ==="
echo "API_URL=$API_URL"

check "ops capabilities include storageclass-apply" ops_has_action storage storageclass-apply
check "ops capabilities include cosign-enable" ops_has_action policy cosign-enable
check "ops capabilities include apps sync-sweep" ops_has_action apps sync-sweep
check "ops capabilities include secrets sync-sweep" ops_has_action secrets sync-sweep
check "provider capabilities include tunnels-apply" providers_has_action cloudflare tunnels-apply

check "dry-run storage settings-apply" \
  dry_run_ok /v1/ops/storage/settings-apply '{"operation":"ops.storage.settings-apply","dry_run":true}'
check "dry-run storage storageclass-apply" \
  dry_run_ok /v1/ops/storage/storageclass-apply '{"operation":"ops.storage.storageclass-apply","dry_run":true}'
check "dry-run policy cosign-enable" \
  dry_run_ok /v1/ops/policy/cosign-enable '{"operation":"ops.policy.cosign-enable","dry_run":true}'
check "dry-run apps sync-sweep" \
  dry_run_ok /v1/ops/apps/sync-sweep '{"operation":"ops.apps.sync-sweep","dry_run":true,"scope":{"namespace":"argocd"}}'
check "dry-run secrets sync-sweep" \
  dry_run_ok /v1/ops/secrets/sync-sweep '{"operation":"ops.secrets.sync-sweep","dry_run":true}'
check "dry-run cloudflare tunnels-apply" \
  dry_run_ok /v1/providers/cloudflare/tunnels-apply '{"operation":"providers.cloudflare.tunnels-apply","dry_run":true,"scope":{"project":"example"}}'
check "admin db schema" \
  sh -c 'enclii db schema --json | python3 -c "import json,sys; p=json.load(sys.stdin); sys.exit(0 if p.get(\"healthy\") else 1)"'

echo
echo "passed=$PASS failed=$FAIL"
if [ "$FAIL" -gt 0 ]; then
  echo "Deploy may be stale — sync enclii-infrastructure from main and re-run." >&2
  exit 1
fi

echo "GA adapters reachable. Run ./scripts/wave0-ga-ops.sh then ./scripts/wave1-ga-ops.sh."
