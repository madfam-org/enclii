# Production Drift Prevention

## Policy

Every production Argo application that tracks `main` must satisfy both checks:

- `status.sync.revision` equals the current GitHub `main` SHA for its repo.
- Argo reports `Synced` and `Healthy` after reconciliation.

## Prevention controls

- Run a scheduled drift audit from the cluster using Argo application state plus GitHub `main` SHAs.
- Alert separately for commit drift, sync drift, health drift, and stale failed operations.
- Treat immutable Kubernetes objects as recreate-on-change hooks, not ordinary resources.
- Keep admission-policy requirements in source manifests and Helm values, not live patches.
- Vendor required CRDs in GitOps before any app declares CRs that depend on them.
- Block promotion when a deploy produces an unsigned image digest or an image from a disallowed registry.

## Remediation order

1. Fix source so Argo can render and apply cleanly.
2. Recreate only immutable live resources that are already superseded by source.
3. Trigger Argo sync.
4. Verify revision parity, sync, health, and public health endpoints where present.

## Current guardrail gaps closed

- Prometheus Operator `PrometheusRule` and `PodMonitor` CRDs are GitOps-owned before apps apply those resources.
- Redpanda Console uses `docker.io/redpandadata/console`, which is accepted by the cluster registry policy.
