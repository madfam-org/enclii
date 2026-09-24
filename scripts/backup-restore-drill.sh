#!/usr/bin/env bash
set -euo pipefail

# Postgres Backup Restore Drill (logical, pg_dumpall).
# Starts a one-off Job from the monthly CronJob `postgres-restore-drill`, so
# there is exactly ONE drill definition
# (infra/k8s/production/backup/restore-drill-cronjob.yaml), then waits for it
# and prints its logs. Non-destructive: the drill restores into an ephemeral
# Postgres inside its own pod and never connects to the production server.
#
# Break-glass only (see AGENTS.md): routine runs are the CronJob itself.

NAMESPACE="data"
JOB_NAME="postgres-restore-drill-manual-$(date -u +%m%d%H%M%S)"
TIMEOUT="${DRILL_TIMEOUT:-5400s}"

echo "=== Postgres Backup Restore Drill ==="
kubectl -n "${NAMESPACE}" create job --from=cronjob/postgres-restore-drill "${JOB_NAME}"
echo "Job: ${JOB_NAME} (timeout ${TIMEOUT})"

# Wait for either terminal condition; `kubectl wait` on one condition alone
# would sit out the whole timeout on a failed drill.
DEADLINE=$(( $(date +%s) + ${TIMEOUT%s} ))
RESULT=""
while [ "$(date +%s)" -lt "${DEADLINE}" ]; do
  RESULT=$(kubectl -n "${NAMESPACE}" get job "${JOB_NAME}" \
    -o jsonpath='{range .status.conditions[?(@.status=="True")]}{.type}{"\n"}{end}' | grep -E '^(Complete|Failed)$' || true)
  [ -n "${RESULT}" ] && break
  sleep 15
done

echo ""
echo "--- Drill Output ---"
kubectl -n "${NAMESPACE}" logs "job/${JOB_NAME}" --all-containers || true

if [ "${RESULT}" = "Complete" ]; then
  echo ""
  echo "Restore drill PASSED (${JOB_NAME})"
else
  echo ""
  echo "Restore drill FAILED or timed out (${JOB_NAME}: ${RESULT:-no terminal condition})"
  exit 1
fi
