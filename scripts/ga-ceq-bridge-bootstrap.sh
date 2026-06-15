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

if kubectl -n "$CEQ_NS" get externalsecret ceq-janua-client-secret >/dev/null 2>&1; then
  kubectl -n "$CEQ_NS" annotate externalsecret ceq-janua-client-secret \
    "force-sync=$(date +%s)" --overwrite >/dev/null
  sleep 5
  reason="$(kubectl -n "$CEQ_NS" get externalsecret ceq-janua-client-secret \
    -o jsonpath='{.status.conditions[0].reason}' 2>/dev/null || echo unknown)"
  echo "ceq-janua-client-secret ESO status: ${reason}"
fi
