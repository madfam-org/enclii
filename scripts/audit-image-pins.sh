#!/bin/bash
# audit-image-pins.sh - Audit container images and suggest SHA digest pins
# Usage: ./scripts/audit-image-pins.sh [--dry-run|--output FILE]

set -e

KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config-hetzner}"

echo "=== Container Image Audit ==="
echo "Date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo ""

# Get all deployments and their images
echo "## Deployments using :latest tag (HIGH PRIORITY):"
kubectl --kubeconfig "$KUBECONFIG" get deployments -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.metadata.name}{"\t"}{.spec.template.spec.containers[0].image}{"\n"}{end}' 2>/dev/null | grep ":latest" || echo "None found"

echo ""
echo "## Deployments without SHA digest pins:"
kubectl --kubeconfig "$KUBECONFIG" get deployments -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.metadata.name}{"\t"}{.spec.template.spec.containers[0].image}{"\n"}{end}' 2>/dev/null | grep -v "sha256:" | sort

echo ""
echo "## StatefulSets:"
kubectl --kubeconfig "$KUBECONFIG" get statefulsets -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.metadata.name}{"\t"}{.spec.template.spec.containers[0].image}{"\n"}{end}' 2>/dev/null | grep -v "sha256:" | sort

echo ""
echo "## Summary:"
TOTAL_DEPLOYMENTS=$(kubectl --kubeconfig "$KUBECONFIG" get deployments -A --no-headers 2>/dev/null | wc -l | tr -d ' ')
LATEST_COUNT=$(kubectl --kubeconfig "$KUBECONFIG" get deployments -A -o jsonpath='{range .items[*]}{.spec.template.spec.containers[0].image}{"\n"}{end}' 2>/dev/null | grep ":latest" | wc -l | tr -d ' ')
UNPINNED_COUNT=$(kubectl --kubeconfig "$KUBECONFIG" get deployments -A -o jsonpath='{range .items[*]}{.spec.template.spec.containers[0].image}{"\n"}{end}' 2>/dev/null | grep -v "sha256:" | wc -l | tr -d ' ')

echo "- Total deployments: $TOTAL_DEPLOYMENTS"
echo "- Using :latest tag: $LATEST_COUNT (HIGH PRIORITY - pin to specific version)"
echo "- Without SHA digest: $UNPINNED_COUNT"
echo ""

echo "## Recommendations:"
echo "1. Replace :latest tags with specific versions or SHA digests"
echo "2. Configure argocd-image-updater to automatically track and pin digests"
echo "3. Use crane or skopeo to look up image digests:"
echo "   crane digest ghcr.io/madfam-org/enclii/admin-console:latest"
echo "   skopeo inspect docker://ghcr.io/madfam-org/enclii/admin-console:latest | jq .Digest"
