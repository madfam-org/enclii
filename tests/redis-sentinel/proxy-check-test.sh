#!/usr/bin/env bash
# Offline test for the redis-ha master-routing proxy (infra/k8s/redis-sentinel/redis-ha-proxy.yaml).
#
# Renders the manifests, runs the REAL haproxy.cfg on the pinned HAProxy image against a
# master + replica and a standalone Redis behind one DNS alias (three A records, like the
# headless Service), and asserts:
#   1. exactly one slot is UP: the master (role:master AND connected_slaves >= 1);
#   2. no slot was routable before its first check (init-state down => DOWN/READY at resolution);
#   3. losing the master's only replica takes it DOWN; the replica's return brings it back UP.
# Needs docker, kustomize, python3 (+PyYAML). About one minute. Cleans up after itself.
# shellcheck disable=SC2015  # `check && ok || ko`: ok() never fails, so ko() runs only on a failed check
set -euo pipefail
# Pipelines that test docker logs use `grep -c … >/dev/null`, never `grep -q`: with pipefail, grep -q
# exiting early gives `docker logs` a SIGPIPE and the pipeline fails even though the line was there.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"; NET="rsproxy-$$"; M="rs-m-$$"; R="rs-r-$$"; L="rs-l-$$"; H="rs-h-$$"
cleanup() { docker rm -f "$M" "$R" "$L" "$H" >/dev/null 2>&1 || true; docker network rm "$NET" >/dev/null 2>&1 || true; rm -rf "$TMP"; }
trap cleanup EXIT
for t in docker kustomize python3; do command -v "$t" >/dev/null || { echo "missing dependency: $t" >&2; exit 2; }; done

kustomize build "$ROOT/infra/k8s/redis-sentinel" > "$TMP/rendered.yaml"
python3 - "$TMP" <<'PY'
import sys, yaml
T = sys.argv[1]
yaml.SafeLoader.add_constructor('tag:yaml.org,2002:value', lambda l, n: l.construct_scalar(n))
cfg = himg = rimg = None
for d in yaml.safe_load_all(open(T + '/rendered.yaml')):
    if not d: continue
    if d['kind'] == 'ConfigMap' and d['metadata']['name'] == 'redis-ha-proxy-config': cfg = d['data']['haproxy.cfg']
    if d['kind'] == 'Deployment' and d['metadata']['name'] == 'redis-ha-proxy': himg = d['spec']['template']['spec']['containers'][0]['image']
    if d['kind'] == 'StatefulSet' and d['metadata']['name'] == 'redis-ha': rimg = d['spec']['template']['spec']['containers'][0]['image']
assert cfg and himg and rimg
open(T + '/haproxy.cfg', 'w').write(cfg.replace('redis-ha-headless.data.svc.cluster.local', 'rs-redis'))
open(T + '/images', 'w').write(himg + '\n' + rimg + '\n')
PY
HIMG="$(sed -n 1p "$TMP/images")"; RIMG="$(sed -n 2p "$TMP/images")"
echo "haproxy: $HIMG"; echo "redis:   $RIMG"

docker network create "$NET" >/dev/null
docker run -d --name "$M" --network "$NET" --network-alias rs-redis "$RIMG" redis-server --requirepass pw --masterauth pw --min-replicas-to-write 1 >/dev/null
docker run -d --name "$L" --network "$NET" --network-alias rs-redis "$RIMG" redis-server --requirepass pw >/dev/null
docker run -d --name "$R" --network "$NET" --network-alias rs-redis "$RIMG" redis-server --requirepass pw --masterauth pw --replicaof "$M" 6379 >/dev/null
sleep 3
docker run -d --name "$H" --network "$NET" -e REDIS_PASSWORD=pw -v "$TMP/haproxy.cfg:/usr/local/etc/haproxy/haproxy.cfg:ro" "$HIMG" >/dev/null

stats() {
  docker exec "$H" sh -c 'wget -qO- "http://127.0.0.1:8404/stats;csv"' | python3 -c '
import sys, csv
rows = list(csv.reader(sys.stdin)); hdr = [h.strip("# ") for h in rows[0]]
for d in (dict(zip(hdr, r)) for r in rows[1:]):
    if d.get("pxname") == "redis_master" and d.get("svname", "").startswith("redis"):
        print(d["svname"], d["status"], d.get("check_status", ""))'
}
fail=0; ok() { echo "  PASS: $*"; }; ko() { echo "  FAIL: $*"; fail=1; }

sleep 8
echo "[1] steady state"; s="$(stats)"; echo "$s" | awk '{print "      "$0}'
[ "$(echo "$s" | grep -c ' UP L7OK')" = 1 ] && ok "exactly one slot UP (the master)" || ko "expected exactly one UP/L7OK slot"
[ "$(echo "$s" | grep -c 'DOWN')" = 2 ] && ok "replica and standalone are DOWN" || ko "expected two DOWN slots"

echo "[2] initial state at resolution"; logs="$(docker logs "$H" 2>&1)"
if echo "$logs" | grep -c "is UP/READY (resolves again)" >/dev/null; then ko "a slot was routable before its first check (init-state down missing?)"; else ok "no slot was UP before being checked"; fi
echo "$logs" | grep -c "is DOWN/READY (resolves again)" >/dev/null && ok "slots start DOWN when their record resolves" || ko "expected DOWN/READY at resolution"

# wait_for <expected UP count> <seconds>: polls the stats until the number of UP slots matches
wait_for() { local want="$1" secs="$2" n=0; while [ "$n" -lt "$secs" ]; do s="$(stats)"; [ "$(echo "$s" | grep -c ' UP ')" = "$want" ] && return 0; sleep 1; n=$((n + 1)); done; return 1; }
echo "[3] master loses its only replica, then gets it back"
docker stop "$R" >/dev/null
wait_for 0 20 && ok "master DOWN with connected_slaves 0" || ko "master still UP without a replica after 20 s"
docker start "$R" >/dev/null
wait_for 1 30 && [ "$(echo "$s" | grep -c ' UP L7OK')" = 1 ] && ok "master UP again once the replica returned" || ko "master did not come back UP within 30 s"

if [ "$fail" = 0 ]; then echo "proxy-check-test: PASS"; else echo "proxy-check-test: FAIL" >&2; exit 1; fi
