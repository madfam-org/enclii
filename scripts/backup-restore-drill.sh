#!/usr/bin/env bash
set -euo pipefail

# Postgres Backup Restore Drill
# Applies the restore drill Job and tails logs until completion.
# Non-destructive: never touches the production database.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
JOB_MANIFEST="${SCRIPT_DIR}/../infra/k8s/production/backup/postgres-restore-drill.yaml"
NAMESPACE="enclii"
JOB_NAME="postgres-restore-drill"

echo "=== Postgres Backup Restore Drill ==="
echo ""

# Clean up previous run if exists
if kubectl get job "${JOB_NAME}" -n "${NAMESPACE}" >/dev/null 2>&1; then
  echo "Cleaning up previous drill job..."
  kubectl delete job "${JOB_NAME}" -n "${NAMESPACE}" --wait=true
fi

# Apply the job
echo "Applying restore drill job..."
kubectl apply -f "${JOB_MANIFEST}"

# Wait for pod to start
echo "Waiting for pod to start..."
kubectl wait --for=condition=ready pod -l app=postgres-restore-drill -n "${NAMESPACE}" --timeout=120s 2>/dev/null || true

# Tail logs
echo ""
echo "--- Drill Output ---"
kubectl logs -n "${NAMESPACE}" "job/${JOB_NAME}" -f

# Check result
EXIT_CODE=$(kubectl get job "${JOB_NAME}" -n "${NAMESPACE}" -o jsonpath='{.status.succeeded}')
if [ "${EXIT_CODE}" = "1" ]; then
  echo ""
  echo "✅ Restore drill completed successfully"
else
  echo ""
  echo "❌ Restore drill failed"
  exit 1
fi
