#!/usr/bin/env bash
# Finish-line operator orchestration: Vault writer → secret intake → CEQ orchestrator.
#
# Never prints secret values. Requires:
#   VAULT_TOKEN_FILE  — Vault admin token (Bitwarden / offline store)
#   VAST_API_KEY_FILE — optional; if set, completes CEQ orchestrator bridge
#
# Usage:
#   VAULT_TOKEN_FILE=~/.config/madfam/vault-admin.token \
#   VAST_API_KEY_FILE=~/.config/madfam/vast.api.key \
#     ./scripts/finish-line-secret-intake.sh
#
# Last Updated: 2026-06-15
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENCLII_NS="${ENCLII_NS:-enclii}"
CEQ_NS="${CEQ_NAMESPACE:-ceq}"
PF_PORT="${SWITCHYARD_PF_PORT:-14200}"
PF_PID=""

log() { printf '[INFO] %s\n' "$*"; }
ok() { printf '[ OK ] %s\n' "$*"; }
die() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }

cleanup() {
  if [[ -n "$PF_PID" ]] && kill -0 "$PF_PID" 2>/dev/null; then
    kill "$PF_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

require() {
  command -v "$1" >/dev/null || die "$1 is required"
}

require kubectl
require python3

CLI="${ENCLII_BIN:-}"
if [[ -z "$CLI" ]]; then
  if command -v enclii >/dev/null; then
    CLI=enclii
  elif [[ -x "$ROOT/packages/cli/enclii" ]]; then
    CLI="$ROOT/packages/cli/enclii"
  elif [[ -x /tmp/enclii ]]; then
    CLI=/tmp/enclii
  else
    log "Building enclii CLI..."
    (cd "$ROOT/packages/cli" && go build -o /tmp/enclii ./cmd/enclii/)
    CLI=/tmp/enclii
  fi
fi

api_base() {
  local token
  token="$(oauth_token)"
  if curl -sf --max-time 3 "https://api.enclii.dev/health/ready" >/dev/null 2>&1 \
    && curl -sf --max-time 3 -H "Authorization: Bearer ${token}" \
      "https://api.enclii.dev/v1/secrets/intake/targets" >/dev/null 2>&1; then
    echo "https://api.enclii.dev"
    return 0
  fi
  log "Cloudflare or network blocked public API — port-forwarding switchyard-api:${PF_PORT}"
  kubectl -n "$ENCLII_NS" port-forward svc/switchyard-api "${PF_PORT}:4200" >/dev/null 2>&1 &
  PF_PID=$!
  sleep 2
  echo "http://127.0.0.1:${PF_PORT}"
}

oauth_token() {
  python3 - <<'PY'
import json, os, sys
p = os.path.expanduser("~/.enclii/credentials.json")
if not os.path.exists(p):
    sys.exit("Run: enclii login")
with open(p) as f:
    print(json.load(f)["access_token"])
PY
}

step_vault_writer() {
  if kubectl -n "$ENCLII_NS" get secret vault-credentials >/dev/null 2>&1; then
    ok "vault-credentials already exists"
    return 0
  fi
  [[ -n "${VAULT_TOKEN_FILE:-}" || -n "${VAULT_TOKEN:-}" ]] \
    || die "Set VAULT_TOKEN_FILE (Vault admin from offline store) to create vault-credentials"
  log "Provisioning switchyard-secret-writer policy + vault-credentials..."
  VAULT_TOKEN_FILE="${VAULT_TOKEN_FILE:-}" VAULT_TOKEN="${VAULT_TOKEN:-}" \
    "$ROOT/scripts/provision-switchyard-vault-writer.sh"
  ok "vault-credentials provisioned"
}

step_verify_intake_api() {
  local base
  base="$(api_base)"
  log "Verifying intake API at ${base}..."
  python3 - <<PY
import json, os, urllib.request
base = "${base}"
token = """$(oauth_token)"""
req = urllib.request.Request(
    f"{base}/v1/secrets/intake/targets",
    headers={"Authorization": f"Bearer {token}"},
)
with urllib.request.urlopen(req, timeout=20) as resp:
    body = json.loads(resp.read().decode())
assert len(body.get("targets", [])) >= 4
print("targets_ok", len(body["targets"]))
PY
  ok "intake targets reachable (admin auth)"
}

step_intake_ceq_vast() {
  if [[ -n "${SKIP_INTAKE:-}" ]]; then
    log "SKIP_INTAKE set — skipping Vault intake submit"
    return 0
  fi
  if [[ -n "${VAST_API_KEY_FILE:-}" && -r "${VAST_API_KEY_FILE}" ]]; then
    log "Submitting ceq/vast-api-key via intake (value-file)..."
    "$CLI" --api-endpoint "$(api_base)" secrets intake submit ceq/vast-api-key \
      --reason "finish-line orchestrator bootstrap $(date +%Y-%m-%d)" \
      --value-file "$VAST_API_KEY_FILE"
    ok "intake submit complete — tell agents the intake_id only"
    return 0
  fi
  log "No VAST_API_KEY_FILE — run masked intake manually:"
  printf '  %s --api-endpoint %s secrets intake submit ceq/vast-api-key --reason "orchestrator bootstrap"\n' \
    "$CLI" "$(api_base)"
}

step_ceq_bridge() {
  log "Running CEQ kubernetes-store bridge bootstrap..."
  export CEQ_NAMESPACE="$CEQ_NS"
  if [[ -n "${VAST_API_KEY_FILE:-}" && -r "${VAST_API_KEY_FILE}" ]]; then
    export VAST_API_KEY_FILE
  fi
  "$ROOT/scripts/ga-ceq-bridge-bootstrap.sh" || true
  kubectl -n "$CEQ_NS" annotate externalsecret ceq-orchestrator-secrets \
    "force-sync=$(date +%s)" --overwrite >/dev/null 2>&1 || true
  if kubectl -n "$CEQ_NS" wait --for=condition=Ready externalsecret/ceq-orchestrator-secrets --timeout=120s >/dev/null 2>&1; then
    ok "ceq-orchestrator-secrets ExternalSecret Ready"
  else
    log "WARN: ceq-orchestrator-secrets not Ready — VAST_API_KEY still required"
    kubectl -n "$CEQ_NS" get externalsecret ceq-orchestrator-secrets 2>/dev/null || true
  fi
}

step_orchestrator() {
  if ! kubectl -n "$CEQ_NS" get deploy ceq-worker-orchestrator >/dev/null 2>&1; then
    log "No ceq-worker-orchestrator deployment — skipping scale"
    return 0
  fi
  if kubectl -n "$CEQ_NS" get secret ceq-orchestrator-secrets >/dev/null 2>&1; then
    kubectl -n "$CEQ_NS" scale deploy/ceq-worker-orchestrator --replicas=1
    kubectl -n "$CEQ_NS" rollout status deploy/ceq-worker-orchestrator --timeout=180s || true
    ok "orchestrator scaled to 1"
  else
    log "ceq-orchestrator-secrets not synced — orchestrator stays scaled down"
  fi
}

main() {
  log "=== Finish line: secret intake + CEQ orchestrator ==="
  step_vault_writer
  step_verify_intake_api
  step_intake_ceq_vast
  step_ceq_bridge
  step_orchestrator
  ok "Finish-line script complete"
}

main "$@"
