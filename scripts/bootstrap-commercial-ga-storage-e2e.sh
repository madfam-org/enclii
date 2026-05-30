#!/usr/bin/env bash
# Bootstrap Bet C storage E2E target (tulana-api → development) before staging proof.
#
# Copies gitops project secrets into the dev namespace and configures service
# health probes + ENCLII_PORT via the Enclii API (no secrets in git).
#
# Usage:
#   export STORAGE_E2E_TOKEN='...'   # admin-capable token on throwaway project
#   ./scripts/bootstrap-commercial-ga-storage-e2e.sh
#
# Optional:
#   ENCLII_API_URL=http://127.0.0.1:14200  (port-forward when Cloudflare blocks JWT)
#   KUBE_CONTEXT=foundry
#   STORAGE_E2E_SERVICE_ID=2e0cf4c9-7afc-4cf3-9207-ec68a8b37a56
#   STORAGE_E2E_ENVIRONMENT_NAME=development
#   PROJECT_SLUG=tulana
#   PROD_NAMESPACE=tulana
#   DEV_NAMESPACE=tulana-dev

set -euo pipefail

API_URL="${ENCLII_API_URL:-https://api.enclii.dev}"
KUBE_CONTEXT="${KUBE_CONTEXT:-foundry}"
SERVICE_ID="${STORAGE_E2E_SERVICE_ID:-2e0cf4c9-7afc-4cf3-9207-ec68a8b37a56}"
ENV_NAME="${STORAGE_E2E_ENVIRONMENT_NAME:-development}"
PROJECT_SLUG="${PROJECT_SLUG:-tulana}"
PROD_NS="${PROD_NAMESPACE:-tulana}"
DEV_NS="${DEV_NAMESPACE:-tulana-dev}"
SECRET_NAME="${PROJECT_SLUG}-secrets"

if [ -z "${STORAGE_E2E_TOKEN:-}" ]; then
  echo "STORAGE_E2E_TOKEN is required" >&2
  exit 2
fi

auth=(-H "Authorization: Bearer ${STORAGE_E2E_TOKEN}" -H "Content-Type: application/json")

echo "=== Bootstrap storage E2E target ==="
echo "api=$API_URL service=$SERVICE_ID env=$ENV_NAME dev_ns=$DEV_NS"

# 1. Copy gitops project secret into dev namespace (reconciler mounts via envFrom).
if command -v kubectl >/dev/null 2>&1; then
  if kubectl --context "$KUBE_CONTEXT" get secret "$SECRET_NAME" -n "$DEV_NS" >/dev/null 2>&1; then
    echo "Secret ${SECRET_NAME} already exists in ${DEV_NS}; skip copy"
  elif kubectl --context "$KUBE_CONTEXT" get secret "$SECRET_NAME" -n "$PROD_NS" >/dev/null 2>&1; then
    echo "Copying ${SECRET_NAME} ${PROD_NS} → ${DEV_NS}..."
    DEV_NS="$DEV_NS" kubectl --context "$KUBE_CONTEXT" get secret "$SECRET_NAME" -n "$PROD_NS" -o json \
      | python3 -c "import json,sys,os; s=json.load(sys.stdin); m=s['metadata']; m.pop('resourceVersion',None); m.pop('uid',None); m.pop('creationTimestamp',None); m['namespace']=os.environ['DEV_NS']; print(json.dumps(s))" \
      | kubectl --context "$KUBE_CONTEXT" apply -f -
  else
    echo "WARN: secret ${SECRET_NAME} not found in ${PROD_NS}; skip copy" >&2
  fi
else
  echo "WARN: kubectl not available; skip secret copy" >&2
fi

# 2. Resolve development environment UUID via project slug.
svc_body="$(curl -sf "${auth[@]}" "${API_URL}/v1/services/${SERVICE_ID}")"
PROJECT_SLUG_FROM_API="$(python3 - <<PY
import json
svc = json.loads('''${svc_body}''')
print(svc.get("project_slug") or svc.get("project", {}).get("slug") or "${PROJECT_SLUG}")
PY
)"
env_body="$(curl -sf "${auth[@]}" "${API_URL}/v1/projects/${PROJECT_SLUG_FROM_API}/environments")"
ENV_ID="$(ENV_NAME="$ENV_NAME" python3 - <<PY
import json, os, sys
data = json.loads('''${env_body}''')
name = os.environ["ENV_NAME"]
for e in data.get("environments", data if isinstance(data, list) else []):
    if e.get("name") == name:
        print(e["id"])
        break
PY
)"
if [ -z "$ENV_ID" ]; then
  echo "Environment '${ENV_NAME}' not found for service ${SERVICE_ID}" >&2
  exit 1
fi
echo "environment_id=$ENV_ID"

# 3. Configure health probes on the service (global; applies to all env deploys).
health_payload='{
  "health_check": {
    "path": "/api/v1/health/",
    "port": 8000,
    "http_headers": {
      "Host": "tulana-api.madfam.io",
      "X-Forwarded-Proto": "https"
    },
    "initial_delay_seconds": 15,
    "period_seconds": 10,
    "timeout_seconds": 3
  }
}'
echo "PATCH service health_check..."
curl -sf "${auth[@]}" -X PATCH "${API_URL}/v1/services/${SERVICE_ID}" \
  -d "$health_payload" >/dev/null

# 4. Set ENCLII_PORT for development environment.
port_payload="$(ENV_ID="$ENV_ID" python3 - <<'PY'
import json, os
print(json.dumps({
    "environment_id": os.environ["ENV_ID"],
    "variables": [{"key": "ENCLII_PORT", "value": "8000", "is_secret": False}],
}))
PY
)"
echo "POST env-vars/bulk ENCLII_PORT=8000..."
curl -sf "${auth[@]}" -X POST "${API_URL}/v1/services/${SERVICE_ID}/env-vars/bulk" \
  -d "$port_payload" >/dev/null

echo
echo "Bootstrap complete. Re-deploy tulana-api to development, then run:"
echo "  gh workflow run commercial-ga-staging-proof -f bets=storage"
