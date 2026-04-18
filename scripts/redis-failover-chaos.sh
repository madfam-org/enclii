#!/usr/bin/env bash
#
# redis-failover-chaos.sh — Kill the current Redis master and measure how long
# Sentinel takes to elect a new one.
#
# Success criterion: failover completes in < 20s. Exits 0 on pass, 1 on fail.
#
# Appends one row to docs/runbooks/redis-failover-log.md using the schema
# documented there.
#
# Usage:
#   ./scripts/redis-failover-chaos.sh [--kind drill|incident] [--dry-run]
#
# Prerequisites:
#   - kubectl context points at the target cluster (production by default)
#   - The `redis-ha` StatefulSet in namespace `data` is Synced+Healthy in ArgoCD
#   - jq, date (GNU or BSD), timeout
#
# The script:
#   1. Asks Sentinel who the current master is.
#   2. Records a start timestamp.
#   3. Deletes the current master pod (kubectl delete pod).
#   4. Polls Sentinel every 1s for a new master-addr-by-name response.
#   5. Computes elapsed wall-clock time.
#   6. Verifies new master accepts a write (SET/GET round-trip).
#   7. Appends one row to docs/runbooks/redis-failover-log.md.
#   8. Exits 0 if failover_s < 20, else 1.

set -euo pipefail

NAMESPACE="${NAMESPACE:-data}"
STATEFULSET="${STATEFULSET:-redis-ha}"
MASTER_NAME="${MASTER_NAME:-mymaster}"
SENTINEL_PORT="${SENTINEL_PORT:-26379}"
REDIS_PORT="${REDIS_PORT:-6379}"
TARGET_SECONDS="${TARGET_SECONDS:-20}"
MAX_WAIT_SECONDS="${MAX_WAIT_SECONDS:-60}"

KIND="drill"
DRY_RUN="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --kind)      KIND="$2"; shift 2 ;;
    --dry-run)   DRY_RUN="true"; shift ;;
    -h|--help)
      sed -n '2,20p' "$0"
      exit 0
      ;;
    *) echo "Unknown arg: $1" >&2; exit 2 ;;
  esac
done

# Resolve repo root so we can find the log file regardless of cwd.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
LOG_FILE="${REPO_ROOT}/docs/runbooks/redis-failover-log.md"

log()  { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
die()  { printf '[FAIL] %s\n' "$*" >&2; exit 1; }

require() {
  command -v "$1" >/dev/null 2>&1 || die "missing dependency: $1"
}
require kubectl
require jq

# ---------------------------------------------------------------------------
# Helper: run a redis-cli command inside the sentinel container of a given pod.
# Falls back across all 3 pods in case the target pod is the one being killed.
# ---------------------------------------------------------------------------
sentinel_cmd() {
  local -r args=("$@")
  local pod
  for i in 0 1 2; do
    pod="${STATEFULSET}-${i}"
    if kubectl -n "${NAMESPACE}" get pod "${pod}" \
         -o jsonpath='{.status.phase}' 2>/dev/null | grep -q Running; then
      if result="$(kubectl -n "${NAMESPACE}" exec "${pod}" -c sentinel -- \
                    redis-cli -p "${SENTINEL_PORT}" "${args[@]}" 2>/dev/null)"; then
        printf '%s' "${result}"
        return 0
      fi
    fi
  done
  die "no Sentinel pod reachable; cluster may be down entirely"
}

# ---------------------------------------------------------------------------
# 1. Who's the master right now?
# ---------------------------------------------------------------------------
log "Querying Sentinel for current master of '${MASTER_NAME}'..."
master_info="$(sentinel_cmd SENTINEL get-master-addr-by-name "${MASTER_NAME}")"
master_ip="$(printf '%s\n' "${master_info}" | head -1)"
[[ -n "${master_ip}" ]] || die "Sentinel returned empty master address"
log "Current master IP: ${master_ip}"

# Map IP -> pod name by listing all redis-ha pods and finding the match.
old_master=""
for i in 0 1 2; do
  pod="${STATEFULSET}-${i}"
  pod_ip="$(kubectl -n "${NAMESPACE}" get pod "${pod}" \
              -o jsonpath='{.status.podIP}' 2>/dev/null || true)"
  if [[ "${pod_ip}" == "${master_ip}" ]]; then
    old_master="${pod}"
    break
  fi
done
[[ -n "${old_master}" ]] || die "could not map master IP ${master_ip} to a pod"
log "Current master pod: ${old_master}"

if [[ "${DRY_RUN}" == "true" ]]; then
  log "--dry-run: would delete pod ${old_master}; exiting without action"
  exit 0
fi

# ---------------------------------------------------------------------------
# 2. Kill the master.
# ---------------------------------------------------------------------------
start_epoch="$(date +%s)"
start_iso="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
log "Deleting pod ${old_master}..."
kubectl -n "${NAMESPACE}" delete pod "${old_master}" --grace-period=0 --force \
  >/dev/null 2>&1 || true

# ---------------------------------------------------------------------------
# 3. Poll Sentinel every 1s for a new master.
# ---------------------------------------------------------------------------
log "Polling Sentinel for new master (target < ${TARGET_SECONDS}s, max ${MAX_WAIT_SECONDS}s)..."
new_master=""
elapsed=0
while (( elapsed < MAX_WAIT_SECONDS )); do
  sleep 1
  elapsed=$(( $(date +%s) - start_epoch ))

  # Query any surviving Sentinel.
  if new_info="$(sentinel_cmd SENTINEL get-master-addr-by-name "${MASTER_NAME}" 2>/dev/null)"; then
    new_ip="$(printf '%s\n' "${new_info}" | head -1)"
    if [[ -n "${new_ip}" && "${new_ip}" != "${master_ip}" ]]; then
      # Map new IP back to pod name.
      for i in 0 1 2; do
        pod="${STATEFULSET}-${i}"
        pod_ip="$(kubectl -n "${NAMESPACE}" get pod "${pod}" \
                    -o jsonpath='{.status.podIP}' 2>/dev/null || true)"
        if [[ "${pod_ip}" == "${new_ip}" ]]; then
          new_master="${pod}"
          break
        fi
      done
      [[ -n "${new_master}" ]] && break
    fi
  fi

  printf '.'
done
printf '\n'

failover_s="${elapsed}"

if [[ -z "${new_master}" ]]; then
  log "NO new master elected after ${MAX_WAIT_SECONDS}s"
  result="fail"
  notes="Sentinel did not elect a new master within ${MAX_WAIT_SECONDS}s. Manual intervention required."
else
  log "New master: ${new_master} (elected in ${failover_s}s)"

  # ------------------------------------------------------------------
  # 4. Write/read sanity check against the new master.
  # ------------------------------------------------------------------
  log "Validating new master accepts writes..."
  probe_key="chaos-probe-$(date +%s)"
  probe_val="ok-${RANDOM}"

  # Pull password from the redis-auth secret.
  redis_auth="$(kubectl -n "${NAMESPACE}" get secret redis-auth \
                      -o jsonpath='{.data.redis-password}' | base64 -d)"

  write_ok="false"
  if kubectl -n "${NAMESPACE}" exec "${new_master}" -c redis -- \
       redis-cli -a "${redis_auth}" -p "${REDIS_PORT}" \
       SET "${probe_key}" "${probe_val}" EX 60 >/dev/null 2>&1; then
    read_val="$(kubectl -n "${NAMESPACE}" exec "${new_master}" -c redis -- \
                  redis-cli -a "${redis_auth}" -p "${REDIS_PORT}" \
                  GET "${probe_key}" 2>/dev/null || true)"
    [[ "${read_val}" == "${probe_val}" ]] && write_ok="true"
  fi

  if (( failover_s < TARGET_SECONDS )) && [[ "${write_ok}" == "true" ]]; then
    result="pass"
    notes="Chaos drill: old master (${old_master}) killed, new master (${new_master}) accepting writes."
  else
    result="fail"
    reason=""
    (( failover_s >= TARGET_SECONDS )) && reason+="failover_s=${failover_s}s >= target ${TARGET_SECONDS}s. "
    [[ "${write_ok}" != "true" ]]      && reason+="new master did not accept SET/GET probe. "
    notes="${reason}"
  fi
fi

# ---------------------------------------------------------------------------
# 5. Append to the log.
# ---------------------------------------------------------------------------
# Count client errors in consumer logs over the failover window. We look at
# the top-3 Redis-consuming services only; full inventory is in the runbook.
client_errors=0
for ns_app in \
    "dhanam:deployment/dhanam-api" \
    "karafiel:deployment/karafiel-api" \
    "janua:deployment/janua"; do
  ns="${ns_app%%:*}"
  app="${ns_app##*:}"
  # --since approximates the failover window; we over-read by a few seconds.
  if count="$(kubectl -n "${ns}" logs "${app}" --since="$(( failover_s + 10 ))s" 2>/dev/null \
                 | grep -iE 'redis.*(error|timeout|connection refused|loss)' \
                 | wc -l | tr -d ' ')"; then
    client_errors=$(( client_errors + count ))
  fi
done

# Ensure log file exists with the header (first run after a fresh clone).
if [[ ! -f "${LOG_FILE}" ]]; then
  die "log file missing at ${LOG_FILE} — expected template to be committed"
fi

row="| ${start_iso} | ${KIND} | ${old_master} | ${new_master:-—} | ${failover_s} | ${client_errors} | ${result} | ${notes} |"
printf '%s\n' "${row}" >> "${LOG_FILE}"
log "Appended to ${LOG_FILE}:"
log "  ${row}"

if [[ "${result}" == "pass" ]]; then
  log "PASS — failover completed in ${failover_s}s (target < ${TARGET_SECONDS}s)"
  exit 0
else
  log "FAIL — see log for details"
  exit 1
fi
