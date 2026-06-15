#!/usr/bin/env bash
# Bootstrap pgbackrest-r2-credentials-source for kubernetes-store merge ESO.
# Copies the live data/pgbackrest-r2-credentials secret (no values printed).
#
# Usage:
#   ./scripts/ga-pgbackrest-bridge-bootstrap.sh
#
# After success:
#   kubectl -n data annotate externalsecret pgbackrest-r2-credentials \
#     force-sync=$(date +%s) --overwrite

set -euo pipefail

DATA_NS="${DATA_NAMESPACE:-data}"
SOURCE_NAME="${PGBACKREST_SOURCE_SECRET:-pgbackrest-r2-credentials}"
BRIDGE_NAME="${PGBACKREST_BRIDGE_SECRET:-pgbackrest-r2-credentials-source}"

if ! kubectl -n "$DATA_NS" get secret "$SOURCE_NAME" >/dev/null 2>&1; then
  echo "Source secret $DATA_NS/$SOURCE_NAME not found" >&2
  exit 1
fi

kubectl -n "$DATA_NS" get secret "$SOURCE_NAME" -o json \
  | DATA_NS="$DATA_NS" BRIDGE="$BRIDGE_NAME" python3 -c "
import json, sys, base64, subprocess, tempfile, os

obj = json.load(sys.stdin)
data = obj.get('data') or {}
if not data:
    raise SystemExit('no data keys on source secret')

data_ns = os.environ['DATA_NS']
bridge = os.environ['BRIDGE']

tmpdir = tempfile.mkdtemp()
args = [
    'kubectl', '-n', data_ns, 'create', 'secret', 'generic',
    bridge, '--dry-run=client', '-o', 'yaml',
]
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
"

echo "Bridge $DATA_NS/$BRIDGE_NAME populated from $SOURCE_NAME (no values printed)"

if kubectl -n "$DATA_NS" get externalsecret pgbackrest-r2-credentials >/dev/null 2>&1; then
  kubectl -n "$DATA_NS" annotate externalsecret pgbackrest-r2-credentials \
    "force-sync=$(date +%s)" --overwrite >/dev/null
  sleep 5
  reason="$(kubectl -n "$DATA_NS" get externalsecret pgbackrest-r2-credentials \
    -o jsonpath='{.status.conditions[0].reason}' 2>/dev/null || echo unknown)"
  echo "pgbackrest-r2-credentials ESO status: ${reason}"
fi
