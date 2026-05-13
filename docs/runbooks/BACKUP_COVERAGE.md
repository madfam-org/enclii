# Backup Coverage Report

**Last updated:** 2026-05-12
**Coverage:** 10/10 for the active production database path plus staged
CNPG HA backups. Legacy `postgres-backup` remains active until PgBouncer
cutover; `postgres-ha-daily` protects the parallel CNPG cluster.

## Schedule Map

```
Daily:
  1:00 AM  github-repos-backup        GitHub org mirror (all repos)
  1:15 AM  cloudflare-config-backup   DNS + tunnels + zone settings
  1:30 AM  k3s-datastore-backup       state.db + TLS + token
  2:00 AM  Longhorn daily-snapshot     Block storage snapshots
  2:30 AM  node-maintenance            Image prune + log rotation
  2:30 AM  postgres-ha-daily           CNPG base backup + continuous WAL
  3:00 AM  postgres-backup             pg_dumpall to R2

Sunday:
  2:00 AM  argocd-secrets-backup      Secrets + configs + apps

Monthly (1st):
  4:00 AM  backup-verify              R2 object integrity check
  4:00 AM  Longhorn weekly-s3-backup  Block storage to R2
  5:00 AM  postgres-restore-drill     Non-destructive restore test
```

## Backup Details

| Backup | R2 Path | Retention | RPO | Secret Required |
|--------|---------|-----------|-----|-----------------|
| PostgreSQL | `postgres/` | 30 days | 24h | `r2-backup-credentials` |
| PostgreSQL HA (CNPG) | `cnpg/postgres-ha/` | 30 days | <=5m WAL target | `cnpg-r2-credentials` |
| K3s Datastore | `k3s-datastore/` | 7 days | 24h | `r2-backup-credentials` |
| GitHub Repos | `github-mirrors/` | 7 days | 24h | `github-backup-credentials` + `r2-backup-credentials` |
| Cloudflare Config | `cloudflare-config/` | 30 days | 24h | `cloudflare-api-credentials` + `r2-backup-credentials` |
| ArgoCD Secrets | `argocd-secrets/` | 12 weeks | 7 days | `r2-backup-credentials` |
| Longhorn | Longhorn internal + R2 | 7 daily / 4 weekly | varies | Longhorn managed |

## Alerting

All backup CronJobs are monitored by Prometheus (`cronjob-health` rule group):

| Alert | Condition | Severity |
|-------|-----------|----------|
| `CronJobFailed` | Any job failure in `enclii\|monitoring\|data` | warning |
| `PostgresBackupMissing` | No successful postgres-backup in 25h | critical |
| `K3sBackupMissing` | No successful k3s-datastore-backup in 25h | critical |
| `GitHubBackupMissing` | No successful github-repos-backup in 25h | critical |
| `CloudflareBackupMissing` | No successful cloudflare-config-backup in 25h | warning |
| `BackupJobFailed` | Any named backup job fails | critical |
| `BackupVerifyFailed` | Backup verification fails | critical |

## Secrets Required (Cluster-Side)

| Secret | Namespace | Keys | Purpose |
|--------|-----------|------|---------|
| `r2-backup-credentials` | data | `account-id`, `access-key-id`, `secret-access-key` | R2 upload (all jobs) |
| `cnpg-r2-credentials` | data | `ACCESS_KEY_ID`, `SECRET_ACCESS_KEY` | CNPG WAL/base backup upload |
| `github-backup-credentials` | data | `github-pat` | GitHub API + clone |
| `cloudflare-api-credentials` | data | `api-token`, `zone-id-enclii`, `zone-id-madfam`, `account-id` | CF API exports |

Templates: `infra/k8s/production/backup/*-secrets.yaml.template`

## Manual Trigger & Verification

```bash
# K3s datastore
kubectl create job --from=cronjob/k3s-datastore-backup test-k3s -n data
kubectl logs -n data job/test-k3s -c backup -f
kubectl logs -n data job/test-k3s -c upload -f

# GitHub repos
kubectl create job --from=cronjob/github-repos-backup test-gh -n data
kubectl logs -n data job/test-gh -c clone-bundle -f
kubectl logs -n data job/test-gh -c upload -f

# Cloudflare config
kubectl create job --from=cronjob/cloudflare-config-backup test-cf -n data
kubectl logs -n data job/test-cf -c backup -f

# ArgoCD secrets
kubectl create job --from=cronjob/argocd-secrets-backup test-argo -n data
kubectl logs -n data job/test-argo -c export -f
kubectl logs -n data job/test-argo -c upload -f

# Verify R2 contents
R2_ENDPOINT="https://<account-id>.r2.cloudflarestorage.com"
aws s3 ls s3://enclii-backups/k3s-datastore/ --endpoint-url $R2_ENDPOINT
aws s3 ls s3://enclii-backups/github-mirrors/ --endpoint-url $R2_ENDPOINT
aws s3 ls s3://enclii-backups/cloudflare-config/ --endpoint-url $R2_ENDPOINT
aws s3 ls s3://enclii-backups/argocd-secrets/ --endpoint-url $R2_ENDPOINT

# Cleanup test jobs
kubectl delete job -n data test-k3s test-gh test-cf test-argo
```

## Deferred: WAL Archiving (Gap 5)

PostgreSQL WAL archiving for point-in-time recovery (PITR) is deferred. Current daily pg_dumpall provides 24h RPO which is adequate for v0.1.0 beta.

WAL archiving requires modifying the live PostgreSQL deployment (`infra/k8s/base/postgres.yaml`) to add `wal_level=replica`, `archive_mode=on`, and a sidecar shipping WALs to R2. This should be done in a dedicated maintenance window with:
1. Staging test first
2. Pre-verified backup
3. Separate PR

## GitOps

All backup resources are managed via ArgoCD Application `backup` (source: `infra/k8s/production/backup/`, destination: `data` namespace, auto-sync with prune + selfHeal).

Kyverno PolicyException: `k3s-backup-policy-exception.yaml` exempts the k3s-datastore-backup CronJob from 5 policies (hostPID, privileged, host-path, run-as-nonroot, capabilities).

NetworkPolicies: 4 new egress policies in `infra/k8s/policies/data-network-policies.yaml` (k3s-backup, github-backup, cloudflare-backup, argocd-backup).
