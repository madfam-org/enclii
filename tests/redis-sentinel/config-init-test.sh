#!/usr/bin/env bash
# Offline test for the redis-ha config-init boot decision (infra/k8s/redis-sentinel/redis-ha.yaml).
#
# Renders the StatefulSet, extracts the REAL init script and ConfigMap templates, and runs
# them (pinned Redis image) against a master, a replica and a Sentinel in docker. Asserts the
# rendered redis.conf and the logged decision for each scenario:
#   S1/S2 no peer name resolves (true cold start)  -> bootstrap at once (ordinal 1 follows 0; 0 leads)
#   S3    a peer refuses, a Sentinel appears later  -> waits, then follows the live master
#   S4    peers refuse for good (LOOKUP_WAIT=6)     -> bootstrap after the wait
#   S5    replica restart                            -> follows the live master at once
#   S6    healthy master restart (SELF_GRACE=3)      -> waits the grace, stays master
#   S7    master dies; its replacement asks mid-failover -> follows the promoted replica
#   S8    Sentinel names a dead master that is not me    -> waits for the switch, then follows
# Needs docker, kustomize, python3 (+PyYAML). About two minutes. Cleans up after itself.
# shellcheck disable=SC2015  # `check && ok || ko`: ok() never fails, so ko() runs only on a failed check
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"; NET="rsinit-$$"; P="$$"
cleanup() { docker rm -f "s-master-$P" "s-replica-$P" "s-sentinel-$P" "s-peer-$P" "s-late-$P" >/dev/null 2>&1 || true; docker network rm "$NET" >/dev/null 2>&1 || true; rm -rf "$TMP"; }
trap cleanup EXIT
for t in docker kustomize python3; do command -v "$t" >/dev/null || { echo "missing dependency: $t" >&2; exit 2; }; done

kustomize build "$ROOT/infra/k8s/redis-sentinel" > "$TMP/rendered.yaml"
python3 - "$TMP" <<'PY'
import sys, yaml
T = sys.argv[1]
yaml.SafeLoader.add_constructor('tag:yaml.org,2002:value', lambda l, n: l.construct_scalar(n))
for d in yaml.safe_load_all(open(T + '/rendered.yaml')):
    if not d: continue
    if d['kind'] == 'StatefulSet' and d['metadata']['name'] == 'redis-ha':
        init = d['spec']['template']['spec']['initContainers'][0]
        assert init['name'] == 'config-init'
        open(T + '/init.sh', 'w').write(init['command'][2])
        open(T + '/image', 'w').write(d['spec']['template']['spec']['containers'][0]['image'])
    if d['kind'] == 'ConfigMap' and d['metadata']['name'] == 'redis-ha-config':
        open(T + '/redis.conf', 'w').write(d['data']['redis.conf']); open(T + '/sentinel.conf', 'w').write(d['data']['sentinel.conf'])
PY
RIMG="$(cat "$TMP/image")"; echo "redis: $RIMG"
cat > "$TMP/test-sentinel.conf" <<CONF
port 26379
dir /tmp
sentinel monitor mymaster s-master-$P 6379 1
sentinel down-after-milliseconds mymaster 2000
sentinel failover-timeout mymaster 5000
sentinel parallel-syncs mymaster 1
sentinel auth-pass mymaster pw
requirepass pw
sentinel resolve-hostnames yes
sentinel announce-hostnames yes
sentinel announce-ip s-sentinel-$P
sentinel announce-port 26379
CONF
docker network create "$NET" >/dev/null
docker run -d --name "s-master-$P" --network "$NET" "$RIMG" redis-server --requirepass pw --masterauth pw --replica-announce-ip "s-master-$P" --replica-announce-port 6379 >/dev/null
docker run -d --name "s-replica-$P" --network "$NET" "$RIMG" redis-server --requirepass pw --masterauth pw --replicaof "s-master-$P" 6379 --replica-announce-ip "s-replica-$P" --replica-announce-port 6379 >/dev/null
docker run -d --name "s-sentinel-$P" --network "$NET" -v "$TMP/test-sentinel.conf:/etc/sentinel.conf:ro" "$RIMG" sh -c 'cp /etc/sentinel.conf /tmp/sentinel.conf && exec redis-sentinel /tmp/sentinel.conf' >/dev/null
docker run -d --name "s-peer-$P" --network "$NET" "$RIMG" sleep 900 >/dev/null   # a peer NAME that resolves but refuses :26379
sleep 4

# run_init <hostname> [ENV=VALUE ...] -> sets DECISION, CONF (replicaof line or empty), TOOK
run_init() {
  local hn="$1"; shift; local envs=(); for e in "$@"; do envs+=(-e "$e"); done
  local out; out="$(docker run --rm --network "$NET" --hostname "$hn" -e REDIS_PASSWORD=pw -e POD_IP=10.0.0.9 "${envs[@]}" \
      -v "$TMP/init.sh:/init.sh:ro" -v "$TMP/redis.conf:/tmp/redis.conf:ro" -v "$TMP/sentinel.conf:/tmp/sentinel.conf:ro" "$RIMG" \
      sh -c 'mkdir -p /etc/redis; t0=$(date +%s); sh /init.sh 2>&1 | grep "config-init:" | tail -1; echo "TOOK=$(( $(date +%s) - t0 ))"; echo "CONF=$(grep -E "^replicaof" /etc/redis/redis.conf || true)"')"
  DECISION="$(echo "$out" | grep 'config-init:' | sed 's/.*config-init: //')"; TOOK="$(echo "$out" | sed -n 's/^TOOK=//p')"; CONF="$(echo "$out" | sed -n 's/^CONF=//p')"
  echo "      -> ${DECISION} (${TOOK}s) [${CONF:-no replicaof}]"
}
fail=0; ok() { echo "  PASS: $*"; }; ko() { echo "  FAIL: $*"; fail=1; }

echo "[S1] cold start, ordinal 1: no peer name resolves"; run_init redis-ha-1 SENTINEL_HOSTS="no-such-a-$P no-such-b-$P"
[[ "$DECISION" == *bootstrap* && "$CONF" == "replicaof redis-ha-0."* && "$TOOK" -le 5 ]] && ok "bootstrap at once, follows ordinal 0" || ko "S1"
echo "[S2] cold start, ordinal 0"; run_init redis-ha-0 SENTINEL_HOSTS="no-such-a-$P no-such-b-$P"
[[ "$DECISION" == *bootstrap* && -z "$CONF" && "$TOOK" -le 5 ]] && ok "bootstrap at once, leads" || ko "S2"
echo "[S3] a peer refuses; a Sentinel appears after 12 s"
( sleep 12; docker run -d --name "s-late-$P" --network "$NET" -v "$TMP/test-sentinel.conf:/etc/sentinel.conf:ro" "$RIMG" sh -c 'cp /etc/sentinel.conf /tmp/sentinel.conf && exec redis-sentinel /tmp/sentinel.conf' >/dev/null ) &
run_init redis-ha-1 SELF_FQDN=redis-ha-1.test SENTINEL_HOSTS="s-peer-$P s-late-$P" LOOKUP_WAIT=60; wait
[[ "$CONF" == "replicaof s-master-$P 6379" && "$TOOK" -ge 10 && "$TOOK" -le 40 ]] && ok "waited, then followed the live master" || ko "S3"
echo "[S4] peers refuse for good, LOOKUP_WAIT=6"; run_init redis-ha-2 SENTINEL_HOSTS="s-peer-$P" LOOKUP_WAIT=6
[[ "$DECISION" == *bootstrap* && "$TOOK" -ge 6 && "$TOOK" -le 15 ]] && ok "bootstrap after the wait" || ko "S4"
echo "[S5] replica restart"; run_init s-replica SELF_FQDN="s-replica-$P" SENTINEL_HOSTS="s-sentinel-$P"
[[ "$CONF" == "replicaof s-master-$P 6379" && "$TOOK" -le 5 ]] && ok "follows the live master at once" || ko "S5"
echo "[S6] healthy master restart, SELF_GRACE=3"; run_init s-master SELF_FQDN="s-master-$P" SENTINEL_HOSTS="s-sentinel-$P" SELF_GRACE=3
[[ "$DECISION" == *"starting as master"* && -z "$CONF" && "$TOOK" -ge 2 ]] && ok "waited the grace, stays master" || ko "S6"
echo "[S7] master dies; its replacement asks mid-failover"; docker stop "s-master-$P" >/dev/null
run_init s-master SELF_FQDN="s-master-$P" SENTINEL_HOSTS="s-sentinel-$P" SELF_GRACE=8
[[ "$CONF" == "replicaof s-replica-$P 6379" && "$TOOK" -le 30 ]] && ok "followed the promoted replica" || ko "S7"
echo "[S8] Sentinel names a dead master that is not me"; docker start "s-master-$P" >/dev/null; sleep 9; docker stop "s-replica-$P" >/dev/null
run_init s-third SELF_FQDN="s-third-$P" SENTINEL_HOSTS="s-sentinel-$P"
[[ "$CONF" == "replicaof s-master-$P 6379" && "$TOOK" -le 30 ]] && ok "waited for the switch, then followed" || ko "S8"

if [ "$fail" = 0 ]; then echo "config-init-test: PASS"; else echo "config-init-test: FAIL" >&2; exit 1; fi
