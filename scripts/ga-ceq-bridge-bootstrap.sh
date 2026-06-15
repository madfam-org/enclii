#!/usr/bin/env bash
# Bootstrap CEQ kubernetes-store bridge secrets from live cluster secrets.
# Does not print secret values.
#
# Usage:
#   ./scripts/ga-ceq-bridge-bootstrap.sh
#
# After success:
#   kubectl -n ceq annotate externalsecret ceq-janua-client-secret \
#     force-sync=$(date +%s) --overwrite

set -euo pipefail

CEQ_NS="${CEQ_NAMESPACE:-ceq}"

copy_secret_key() {
  local source_name="$1"
  local source_key="$2"
  local bridge_name="$3"
  local bridge_key="${4:-$source_key}"

  if ! kubectl -n "$CEQ_NS" get secret "$source_name" >/dev/null 2>&1; then
    echo "Source secret $CEQ_NS/$source_name not found" >&2
    return 1
  fi
  if ! kubectl -n "$CEQ_NS" get secret "$source_name" \
    -o "jsonpath={.data.${source_key}}" 2>/dev/null | grep -q .; then
    echo "Source key $source_key missing on $CEQ_NS/$source_name" >&2
    return 1
  fi

  local tmpdir
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' RETURN

  kubectl -n "$CEQ_NS" get secret "$source_name" \
    -o "jsonpath={.data.${source_key}}" | base64 -d >"$tmpdir/${bridge_key}"

  kubectl -n "$CEQ_NS" create secret generic "$bridge_name" \
    --from-file="${bridge_key}=$tmpdir/${bridge_key}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  echo "Bridge $CEQ_NS/$bridge_name <= $source_name.$source_key"
}

copy_whole_secret() {
  local source_name="$1"
  local bridge_name="$2"

  if ! kubectl -n "$CEQ_NS" get secret "$source_name" >/dev/null 2>&1; then
    echo "Source secret $CEQ_NS/$source_name not found" >&2
    return 1
  fi

  kubectl -n "$CEQ_NS" get secret "$source_name" -o json \
    | python3 -c "
import json, sys, base64, subprocess, tempfile, os

obj = json.load(sys.stdin)
data = obj.get('data') or {}
if not data:
    raise SystemExit('no data keys')

tmpdir = tempfile.mkdtemp()
args = ['kubectl', '-n', os.environ['CEQ_NS'], 'create', 'secret', 'generic', os.environ['BRIDGE'], '--dry-run=client', '-o', 'yaml']
for key, value in data.items():
    path = os.path.join(tmpdir, key)
    with open(path, 'wb') as fh:
        fh.write(base64.b64decode(value))
    args.extend(['--from-file', f'{key}={path}'])

proc = subprocess.run(args, capture_output=True, text=True)
if proc.returncode != 0:
    print(proc.stderr, file=sys.stderr)
    raise SystemExit(proc.returncode)
subprocess.run(['kubectl', 'apply', '-f', '-'], input=proc.stdout, text=True, check=True)
" CEQ_NS="$CEQ_NS" BRIDGE="$bridge_name"

  echo "Bridge $CEQ_NS/$bridge_name <= full copy of $source_name"
}

echo "=== CEQ Janua client secret bridge ==="
copy_secret_key \
  ceq-janua-client-secret \
  JANUA_CLIENT_SECRET \
  ceq-janua-client-secret-source \
  JANUA_CLIENT_SECRET

echo "=== CEQ orchestrator secrets bridge ==="
ORCH_TMP="$(mktemp -d)"
trap 'rm -rf "$ORCH_TMP"' EXIT

if kubectl -n "$CEQ_NS" get secret ceq-fal-credentials >/dev/null 2>&1 \
  && kubectl -n "$CEQ_NS" get secret ceq-fal-credentials -o "jsonpath={.data.FAL_API_KEY}" 2>/dev/null | grep -q .; then
  kubectl -n "$CEQ_NS" get secret ceq-fal-credentials \
    -o "jsonpath={.data.FAL_API_KEY}" | base64 -d >"$ORCH_TMP/FAL_API_KEY"
else
  echo "WARN: ceq-fal-credentials.FAL_API_KEY missing — orchestrator bridge skipped" >&2
fi

if kubectl -n "$CEQ_NS" get secret ceq-secrets >/dev/null 2>&1; then
  CEQ_NS="$CEQ_NS" python3 - <<'PY' >"$ORCH_TMP/CEQ_WORKER_REDIS_URL"
import base64, json, os, subprocess, urllib.parse
raw = subprocess.check_output([
    'kubectl', '-n', os.environ['CEQ_NS'], 'get', 'secret', 'ceq-secrets',
    '-o', 'jsonpath={.data.REDIS_URL}',
])
url = base64.b64decode(raw).decode()
parsed = urllib.parse.urlparse(url)
# Worker queue uses Redis DB 14 (see docs/GPU_COMPUTE_STRATEGY.md)
path = '/14' if not parsed.path or parsed.path == '/' else parsed.path
print(urllib.parse.urlunparse(parsed._replace(path=path)))
PY
else
  echo "WARN: ceq-secrets missing — cannot derive CEQ_WORKER_REDIS_URL" >&2
fi

if [[ -n "${VAST_API_KEY:-}" ]]; then
  printf '%s' "$VAST_API_KEY" >"$ORCH_TMP/VAST_API_KEY"
elif [[ -n "${VAST_API_KEY_FILE:-}" && -r "$VAST_API_KEY_FILE" ]]; then
  tr -d '\r\n' <"$VAST_API_KEY_FILE" >"$ORCH_TMP/VAST_API_KEY"
else
  echo "WARN: VAST_API_KEY not set — set VAST_API_KEY or VAST_API_KEY_FILE to complete orchestrator bridge" >&2
fi

if [[ -f "$ORCH_TMP/FAL_API_KEY" && -f "$ORCH_TMP/CEQ_WORKER_REDIS_URL" && -f "$ORCH_TMP/VAST_API_KEY" ]]; then
  kubectl -n "$CEQ_NS" create secret generic ceq-orchestrator-secrets-source \
    --from-file=FAL_API_KEY="$ORCH_TMP/FAL_API_KEY" \
    --from-file=CEQ_WORKER_REDIS_URL="$ORCH_TMP/CEQ_WORKER_REDIS_URL" \
    --from-file=VAST_API_KEY="$ORCH_TMP/VAST_API_KEY" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "Bridge $CEQ_NS/ceq-orchestrator-secrets-source ready (3 keys)"
  if kubectl -n "$CEQ_NS" get externalsecret ceq-orchestrator-secrets >/dev/null 2>&1; then
    kubectl -n "$CEQ_NS" annotate externalsecret ceq-orchestrator-secrets \
      "force-sync=$(date +%s)" --overwrite >/dev/null
  fi
else
  echo "Orchestrator bridge incomplete — ESO remains blocked until VAST_API_KEY is supplied"
fi

if kubectl -n "$CEQ_NS" get externalsecret ceq-janua-client-secret >/dev/null 2>&1; then
  kubectl -n "$CEQ_NS" annotate externalsecret ceq-janua-client-secret \
    "force-sync=$(date +%s)" --overwrite >/dev/null
  sleep 5
  reason="$(kubectl -n "$CEQ_NS" get externalsecret ceq-janua-client-secret \
    -o jsonpath='{.status.conditions[0].reason}' 2>/dev/null || echo unknown)"
  echo "ceq-janua-client-secret ESO status: ${reason}"
fi
