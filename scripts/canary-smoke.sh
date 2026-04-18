#!/usr/bin/env bash
# canary-smoke.sh — smoke test for the canary rollout feature (P2.7).
#
# Deploys a dummy nginx Service at 20% canary against a kind/k3d cluster and
# verifies the full promote path. No real karafiel code is touched — that
# cutover is higher-risk and follows once this proves green.
#
# Prerequisites:
#   - kind or k3d cluster available at $KUBECONFIG
#   - enclii CLI on PATH (built from this repo)
#   - Switchyard API running locally (or via enclii port-forward) and
#     ENCLII_API_ENDPOINT + ENCLII_API_TOKEN set
#
# Usage:
#   ./scripts/canary-smoke.sh [--cluster=<kind-ctx>]
#
# Exit codes:
#   0 — canary succeeded end-to-end
#   1 — setup failed (cluster/CLI/api connectivity)
#   2 — canary failed to promote within the validation window
#   3 — rollback test failed

set -euo pipefail

SVC_NAME="${SVC_NAME:-canary-smoke-nginx}"
PROJECT="${PROJECT:-canary-smoke}"
ENV_NAME="${ENV_NAME:-staging}"
CLUSTER_CTX="${CLUSTER_CTX:-kind-kind}"
VALIDATION_WINDOW="${VALIDATION_WINDOW:-2m}"
CANARY_PCT="${CANARY_PCT:-20}"

log() { echo "[canary-smoke] $*" >&2; }

require() {
  command -v "$1" >/dev/null 2>&1 || { log "missing command: $1"; exit 1; }
}

# -----------------------------------------------------------------------------
# Preflight
# -----------------------------------------------------------------------------
require kubectl
require enclii
require curl
require jq

kubectl --context "$CLUSTER_CTX" cluster-info >/dev/null 2>&1 || {
  log "cluster $CLUSTER_CTX unreachable — start kind/k3d first"
  exit 1
}
: "${ENCLII_API_ENDPOINT:?set ENCLII_API_ENDPOINT}"
: "${ENCLII_API_TOKEN:?set ENCLII_API_TOKEN}"

log "preflight ok (cluster=$CLUSTER_CTX project=$PROJECT svc=$SVC_NAME)"

# -----------------------------------------------------------------------------
# Setup a fake service.yaml backed by a public image so we can skip buildkit.
# (The API's build flow is exercised separately; here we just need to
#  validate the canary reconciler loop.)
# -----------------------------------------------------------------------------
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT
cat > "$WORK_DIR/service.yaml" <<YAML
apiVersion: enclii/v1
kind: Service
metadata:
  name: $SVC_NAME
  project: $PROJECT
spec:
  build:
    type: dockerfile
    dockerfile: Dockerfile
  runtime:
    port: 80
    replicas: 5
    healthCheck: "/"
YAML

# Baseline deploy (stable).
log "initial deploy to $ENV_NAME (5 replicas)"
enclii deploy -f "$WORK_DIR/service.yaml" --env "$ENV_NAME" --wait || {
  log "baseline deploy failed"; exit 1;
}

# Start canary at 20% pointing at a newer nginx tag. The test doesn't really
# care about version difference — we care that the rollout progresses.
log "starting canary rollout at $CANARY_PCT% (window=$VALIDATION_WINDOW)"
enclii deploy -f "$WORK_DIR/service.yaml" \
  --env "$ENV_NAME" \
  --canary "${CANARY_PCT}%" \
  --validation-window "$VALIDATION_WINDOW"

ec=$?
if [[ $ec -eq 0 ]]; then
  log "canary succeeded end-to-end"
else
  log "canary failed (exit=$ec)"
  exit 2
fi

# -----------------------------------------------------------------------------
# Rollback path exercise: start another rollout, then manually rollback.
# -----------------------------------------------------------------------------
log "rollback-path test: starting second canary, then manual rollback"
SERVICE_ID=$(curl -sf -H "Authorization: Bearer $ENCLII_API_TOKEN" \
  "$ENCLII_API_ENDPOINT/v1/services?project=$PROJECT" | \
  jq -r ".services[] | select(.name==\"$SVC_NAME\") | .id")
[[ -n "$SERVICE_ID" ]] || { log "couldn't resolve service id"; exit 3; }

RESP=$(curl -sf -X POST \
  -H "Authorization: Bearer $ENCLII_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"digest\":\"latest\",\"percentage\":$CANARY_PCT,\"validation_window_minutes\":30,\"environment_name\":\"$ENV_NAME\"}" \
  "$ENCLII_API_ENDPOINT/v1/services/$SERVICE_ID/canary" || echo "")
ROLLOUT_ID=$(echo "$RESP" | jq -r '.id // empty')
[[ -n "$ROLLOUT_ID" ]] || { log "failed to start rollback-test rollout: $RESP"; exit 3; }

sleep 5
log "rolling back rollout $ROLLOUT_ID"
curl -sf -X POST \
  -H "Authorization: Bearer $ENCLII_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"reason":"canary-smoke.sh rollback test"}' \
  "$ENCLII_API_ENDPOINT/v1/services/$SERVICE_ID/canary/$ROLLOUT_ID/rollback" >/dev/null

# Poll until terminal, max 2 minutes.
deadline=$(($(date +%s) + 120))
while :; do
  STATE=$(curl -sf -H "Authorization: Bearer $ENCLII_API_TOKEN" \
    "$ENCLII_API_ENDPOINT/v1/services/$SERVICE_ID/canary/$ROLLOUT_ID" | jq -r '.state')
  case "$STATE" in
    manual_rolled_back)
      log "rollback reached terminal state: $STATE"
      exit 0
      ;;
    auto_rolled_back|failed|succeeded)
      log "unexpected terminal state: $STATE (expected manual_rolled_back)"
      exit 3
      ;;
  esac
  if (( $(date +%s) > deadline )); then
    log "rollback timed out in state: $STATE"
    exit 3
  fi
  sleep 3
done
