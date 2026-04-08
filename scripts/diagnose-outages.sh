#!/usr/bin/env bash
# diagnose-outages.sh — Cluster diagnostic runbook for service outages
# Run from foundry-cp or any host with KUBECONFIG set
set -euo pipefail

NAMESPACES=(tezca yantra4d karafiel pravara-mes forgesight madfam-site enclii dhanam janua status)

echo "=== Service Outage Diagnostics ==="
echo "Date: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo ""

# --- Node Resource Usage ---
echo "=== Node Resources ==="
kubectl top nodes 2>/dev/null || echo "(metrics-server not available)"
echo ""

echo "=== Node Allocatable vs Requested ==="
for node in $(kubectl get nodes -o jsonpath='{.items[*].metadata.name}'); do
  echo "--- $node ---"
  kubectl describe node "$node" | grep -A 10 "Allocated resources" || true
  echo ""
done

# --- Per-Namespace Pod Status ---
for ns in "${NAMESPACES[@]}"; do
  echo "================================================================"
  echo "=== Namespace: $ns ==="
  echo "================================================================"

  if ! kubectl get ns "$ns" &>/dev/null; then
    echo "  [MISSING] Namespace $ns does not exist"
    echo ""
    continue
  fi

  echo "--- Pods ---"
  kubectl get pods -n "$ns" -o wide 2>/dev/null || echo "  No pods"
  echo ""

  echo "--- Services ---"
  kubectl get svc -n "$ns" 2>/dev/null || echo "  No services"
  echo ""

  echo "--- Recent Events (last 5) ---"
  kubectl get events -n "$ns" --sort-by='.lastTimestamp' 2>/dev/null | tail -5 || echo "  No events"
  echo ""

  # Show CrashLoopBackOff or ImagePullBackOff pods
  FAILING=$(kubectl get pods -n "$ns" --field-selector=status.phase!=Running,status.phase!=Succeeded -o name 2>/dev/null)
  if [ -n "$FAILING" ]; then
    echo "--- Failing Pods (logs) ---"
    for pod in $FAILING; do
      echo "  $pod:"
      kubectl logs -n "$ns" "$pod" --tail=10 2>/dev/null || echo "    (no logs)"
      echo ""
    done
  fi
done

# --- ArgoCD Application Status ---
echo "================================================================"
echo "=== ArgoCD Applications ==="
echo "================================================================"
kubectl get applications -n argocd -o wide 2>/dev/null || echo "(ArgoCD not found)"
echo ""

# Degraded or OutOfSync apps
echo "--- Degraded/OutOfSync Apps ---"
kubectl get applications -n argocd -o json 2>/dev/null | \
  jq -r '.items[] | select(.status.health.status != "Healthy" or .status.sync.status != "Synced") | "\(.metadata.name): sync=\(.status.sync.status) health=\(.status.health.status)"' 2>/dev/null || echo "(none or jq not available)"
echo ""

# --- Top Pods by Memory ---
echo "=== Top 30 Pods by Memory ==="
kubectl top pods -A --sort-by=memory 2>/dev/null | head -31 || echo "(metrics not available)"
echo ""

# --- Summary ---
echo "=== Quick Health Summary ==="
TOTAL_PODS=$(kubectl get pods -A --no-headers 2>/dev/null | wc -l | tr -d ' ')
RUNNING_PODS=$(kubectl get pods -A --no-headers --field-selector=status.phase=Running 2>/dev/null | wc -l | tr -d ' ')
FAILED_PODS=$(kubectl get pods -A --no-headers --field-selector=status.phase=Failed 2>/dev/null | wc -l | tr -d ' ')
echo "Total pods: $TOTAL_PODS | Running: $RUNNING_PODS | Failed: $FAILED_PODS"
echo ""
echo "Done. Review output above for failing services."
