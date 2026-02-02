#!/usr/bin/env bash
# Production Audit Feb 2, 2026 — Cluster Operations
# Run with: KUBECONFIG=~/.kube/config-hetzner bash scripts/audit-feb2-fixes.sh
set -euo pipefail

echo "=== Production Audit Feb 2 — Cluster Fixes ==="
echo ""

# -----------------------------------------------
# Fix 1: Purge faulted Longhorn backup records
# -----------------------------------------------
echo "--- Fix 1: Longhorn backup cleanup ---"
echo "Step 1: Port-forward Longhorn UI to delete Error backup entries:"
echo "  kubectl port-forward svc/longhorn-frontend -n longhorn-system 8081:80"
echo "  → Open http://localhost:8081 → Backup tab → Delete all Error entries"
echo ""
echo "Step 2: After purge, restart Longhorn manager:"
echo "  kubectl rollout restart ds/longhorn-manager -n longhorn-system"
echo ""
echo "Step 3: Wait 60s, then trigger manual backup:"
echo "  kubectl create job manual-backup-\$(date +%s) --from=cronjob/daily-s3-backup -n longhorn-system"
echo ""

# -----------------------------------------------
# Fix 3: Delete faulted arc-docker-cache-green PVC
# -----------------------------------------------
echo "--- Fix 3: Recreate arc-docker-cache-green as local-path ---"
echo "The manifest in infra/k8s/production/arc/storage.yaml already specifies local-path."
echo "The faulted PVC was created from a previous Longhorn-based definition."
echo ""
echo "Delete the faulted PVC and let ArgoCD recreate it from the correct manifest:"
echo "  kubectl delete pvc arc-docker-cache-green -n arc-runners"
echo "  # ArgoCD will recreate from storage.yaml (local-path)"
echo ""

# -----------------------------------------------
# Fix 4: Clean up legacy data namespace
# -----------------------------------------------
echo "--- Fix 4: Legacy data namespace cleanup ---"

if kubectl get namespace data &>/dev/null; then
    PODS=$(kubectl get pods -n data --no-headers 2>/dev/null | wc -l | tr -d ' ')
    if [ "$PODS" -eq 0 ]; then
        echo "No pods in data namespace. Safe to delete."
        read -p "Delete data namespace and its PVCs? [y/N] " -n 1 -r
        echo ""
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            kubectl delete pvc --all -n data
            kubectl delete namespace data
            echo "Data namespace deleted."
        else
            echo "Skipped."
        fi
    else
        echo "WARNING: data namespace has $PODS running pods. Investigate before deleting."
        kubectl get pods -n data
    fi
else
    echo "data namespace does not exist. Nothing to clean up."
fi
echo ""

# -----------------------------------------------
# Verification
# -----------------------------------------------
echo "--- Verification ---"
echo ""

echo "1. Pod health (non-Running/Completed):"
UNHEALTHY=$(kubectl get pods -A --no-headers | grep -v -E 'Running|Completed' || true)
if [ -z "$UNHEALTHY" ]; then
    echo "   ✅ All pods healthy"
else
    echo "   ⚠️  Unhealthy pods:"
    echo "$UNHEALTHY" | sed 's/^/   /'
fi
echo ""

echo "2. PVC health:"
UNBOUND=$(kubectl get pvc -A --no-headers | grep -v Bound || true)
if [ -z "$UNBOUND" ]; then
    echo "   ✅ All PVCs bound"
else
    echo "   ⚠️  Unbound PVCs:"
    echo "$UNBOUND" | sed 's/^/   /'
fi
echo ""

echo "3. Endpoint health:"
for url in api.enclii.dev/health enclii.dev app.enclii.dev docs.enclii.dev status.enclii.dev; do
    CODE=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 "https://$url" 2>/dev/null || echo "000")
    if [[ "$CODE" =~ ^(200|301|302|307)$ ]]; then
        echo "   ✅ $url: HTTP $CODE"
    else
        echo "   ❌ $url: HTTP $CODE"
    fi
done
echo ""

echo "4. Longhorn volumes:"
kubectl get volumes.longhorn.io -n longhorn-system -o custom-columns=NAME:.metadata.name,STATE:.status.state,ROBUSTNESS:.status.robustness 2>/dev/null || echo "   (could not query Longhorn)"
echo ""

echo "=== Done ==="
