# Postgres HA via CloudNativePG

Wave 3 / Track 3.3 of the 2026-Q2 stability remediation roadmap.
Manifests for the `postgres-ha` CNPG Cluster that replaces the
single-instance Postgres Deployment in `data/postgres`.

**This directory ships manifests only — no cutover.** The ArgoCD app
[`infra/argocd/apps/postgres-ha.yaml`](../../../argocd/apps/postgres-ha.yaml)
deploys these resources in parallel with the existing single-instance
Postgres. PgBouncer continues routing to the old instance until an
operator-driven maintenance window flips the connection target.

**Current bootstrap state:** `instances: 2`, not the final 3-instance target.
The third instance is deferred until Longhorn has enough schedulable capacity
for another 20Gi two-replica volume. The previous 3-instance attempt left the
third PVC faulted with `ReplicaSchedulingFailure: insufficient storage`.

**Backup state:** native CNPG `barmanObjectStore` backups are configured for
`s3://enclii-backups/cnpg/postgres-ha/` using the ESO-derived
`cnpg-r2-credentials` Secret from `data/r2-backup-credentials`.
`postgres-ha-daily` is enabled with
`immediate: false`, so it will not run on apply; it runs at the next 02:30 UTC
schedule unless an operator creates a manual `Backup` first.

## Files

| File | Purpose |
|---|---|
| `data-kubernetes-secretstore.yaml` | ESO ClusterSecretStore that reads source backup credentials from the `data` namespace |
| `cnpg-r2-credentials.externalsecret.yaml` | ESO-derived R2 credential Secret for CNPG backups |
| `cluster.yaml` | CNPG `Cluster` CR (currently 2 instances, sync replication, R2 backup, pgvector) + custom-queries ConfigMap |
| `scheduled-backup.yaml` | Daily base backup definition, enabled with `immediate: false` |
| `podmonitor.yaml` | Prometheus PodMonitor + 5 alert rules (degraded, sync-replica-lost, failover-in-progress, backup-archive-behind, multiple-writable-primaries) |
| `network-policy.yaml` | Ingress from PgBouncer + ecosystem consumers; egress to R2 + DNS + replication peers |
| `r2-credentials.yaml.template` | Manual break-glass template only; normal production credentials are ESO-managed |
| `kustomization.yaml` | Wires the resources together; consumed by the `postgres-ha` ArgoCD Application |

## Pre-merge checklist

The companion PR **must not be merged** until all of the following are confirmed by the operator:

- [ ] CNPG operator image digest resolved and substituted in `infra/argocd/apps/cnpg-operator.yaml` (replace `1.23.0` tag with `@sha256:...` digest).
- [ ] CNPG Postgres image digest resolved and substituted in `cluster.yaml` (replace `15.6-1` tag with `@sha256:...` digest).
- [ ] Longhorn has enough free schedulable capacity for the third 20Gi/2-replica instance volume, or `instances` intentionally remains `2`.
- [x] R2 bucket prefix `cnpg/postgres-ha/` created and write-access granted via the shared R2 backup credentials.
- [x] `cnpg-r2-credentials` Secret is derived by ESO from `data/r2-backup-credentials`.
- [ ] `postgres-ha-superuser` Secret applied to `data` namespace (auto-managed post-Wave-2 via ESO from Vault; manual until then).
- [ ] Owner has reviewed and approved the cutover plan in [`internal-devops/rfcs/0012-postgres-ha-via-cnpg.md`](https://github.com/madfam-org/internal-devops/blob/main/rfcs/0012-postgres-ha-via-cnpg.md) §5 + §8 owner-action gate.

## Apply order (when the gate is green)

```bash
# 1. CNPG operator first (CRDs must exist before Cluster reconciles)
kubectl get application cnpg-operator -n argocd -o jsonpath='{.status.sync.status}'  # should be Synced

# 2. R2 credentials are managed by ESO in this kustomization.
kubectl -n data get externalsecret cnpg-r2-credentials

# 3. Sync the postgres-ha Application (after operator is healthy)
kubectl -n argocd patch application postgres-ha --type merge -p '{"operation":{"sync":{}}}'

# 4. Watch the cluster come up (currently 2 instances; 3rd waits on Longhorn)
kubectl -n data get cluster postgres-ha -w

# 5. Verify (full pre-flight is in postgres-failover-drill.md §0)
kubectl -n data exec postgres-ha-1 -c postgres -- psql -U postgres \
  -c "SELECT application_name, state, sync_state FROM pg_stat_replication;"

# 6. Confirm the ScheduledBackup is enabled
kubectl -n data get scheduledbackup postgres-ha-daily -o jsonpath='{.spec.suspend}{"\n"}'

# 7. Take a manual base backup to validate the R2 pipeline
kubectl -n data create -f - <<EOF
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  generateName: validation-
  namespace: data
spec:
  cluster:
    name: postgres-ha
EOF
```

After step 6 succeeds, the cluster is ready for the dual-write soak phase
(RFC 0012 §5.3). Cutover happens in a separate maintenance window.

## Reference

- **Decision + cutover plan:** [`internal-devops/rfcs/0012-postgres-ha-via-cnpg.md`](https://github.com/madfam-org/internal-devops/blob/main/rfcs/0012-postgres-ha-via-cnpg.md)
- **Failover drill:** [`internal-devops/runbooks/postgres-failover-drill.md`](https://github.com/madfam-org/internal-devops/blob/main/runbooks/postgres-failover-drill.md)
- **WAL archiving (P1.1, runs in parallel during cutover):** [`internal-devops/runbooks/postgres-wal-archiving.md`](https://github.com/madfam-org/internal-devops/blob/main/runbooks/postgres-wal-archiving.md)
- **Sister Redis-HA project (deployment pattern reference):** [`infra/k8s/redis-sentinel/`](../../redis-sentinel/) + [`infra/argocd/apps/redis-sentinel.yaml`](../../../argocd/apps/redis-sentinel.yaml)
- **Operator Application:** [`infra/argocd/apps/cnpg-operator.yaml`](../../../argocd/apps/cnpg-operator.yaml)
- **Cluster Application (consumes this kustomization):** [`infra/argocd/apps/postgres-ha.yaml`](../../../argocd/apps/postgres-ha.yaml)
