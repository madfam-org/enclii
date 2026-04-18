# DR Drill — Operator Runbook

`scripts/dr-drill.sh` is the P0.1 remediation-plan deliverable. It produces the
evidence that backs every RPO/RTO claim in
[`internal-devops/runbooks/disaster-recovery.md`][dr-runbook].

[dr-runbook]: https://github.com/madfam-org/internal-devops/blob/main/runbooks/disaster-recovery.md

## What the script does

1. Validates kubectl + cluster + R2 creds.
2. (Re)creates an ephemeral `dr-test` namespace.
3. Replicates the production R2 credentials secret into `dr-test` (read-only scope).
4. Applies `infra/k8s/dr-test/ephemeral-postgres.yaml` — a single `postgres:16-alpine`
   pod on emptyDir (no Longhorn) with a NetworkPolicy that pins traffic to `dr-test`.
5. Discovers the latest Postgres backup in `s3://enclii-backups/postgres/` and
   downloads it (skipping the `latest.sql.gz` rolling copy for timestamp accuracy).
6. Auto-detects dump format (plain `pg_dumpall` vs custom `pg_dump -Fc`) and
   restores into the ephemeral DB.
7. Runs 5 sanity SELECTs against real tables: `projects`, `services`, `deployments`,
   `releases`, `daily_usage` (Waybill cost equivalent).
8. Emits structured JSON + appends a row to
   `internal-devops/runbooks/dr-log.md`.
9. Tears down `dr-test` unless `--keep` was passed.

## Non-goals

- Does not write to production R2 (only list + download).
- Does not touch production Postgres, PVCs, or services.
- Does not test failover, chaos, or multi-region. Those are Phase 1 items.

## Prerequisites

- kubectl configured against the Hetzner k3s cluster (`KUBECONFIG` pointing to
  `~/.kube/config-hetzner` or equivalent).
- Production secret `enclii/r2-backup-credentials` present (it is — this is the
  same secret the daily `postgres-backup` CronJob uses).
- Production Postgres still running (so backups are being produced — the drill
  needs at least one backup to exist).
- Local clone of `internal-devops` at `/Users/aldoruizluna/labspace/internal-devops`
  (override via `INTERNAL_DEVOPS_ROOT`).
- shellcheck installed if you plan to edit the script (`brew install shellcheck`).

### Preflight checklist

```bash
# 1. cluster reachable
kubectl cluster-info

# 2. R2 secret exists
kubectl get secret r2-backup-credentials -n enclii

# 3. at least one backup exists
kubectl get cronjob postgres-backup -n data
kubectl get jobs -n data -l app=postgres-backup --sort-by=.metadata.creationTimestamp | tail -5

# 4. dr-test namespace is safe to (re)create
kubectl get namespace dr-test  # expect "not found" or to be tear-downable
```

## Usage

### First live run (recommended flow)

```bash
# 1. Dry-run first — prints every action without touching anything.
./scripts/dr-drill.sh --dry-run

# 2. Live run with your identity for the log row.
./scripts/dr-drill.sh --operator "$USER"

# 3. Review the dr-log row that was appended.
tail -5 /Users/aldoruizluna/labspace/internal-devops/runbooks/dr-log.md
```

### Post-mortem / debug mode

If anything looks off in the output, re-run with `--keep`:

```bash
./scripts/dr-drill.sh --operator "$USER" --keep
# Then inspect:
kubectl exec -n dr-test deploy/dr-postgres -- psql -U drpg -d enclii_dr
# When done:
kubectl delete namespace dr-test
```

### Pinning a specific backup (RPO investigation)

```bash
./scripts/dr-drill.sh \
    --operator "$USER" \
    --backup-key postgres/20260414_030000.sql.gz
```

## Expected duration

| Phase | Expected | Alert if |
|---|---|---|
| Preflight | <2s | — |
| Namespace setup | <15s | >60s (stuck finalizers on prior dr-test) |
| Postgres ready | <30s | >120s (image pull, node pressure) |
| Download | 10-120s | >300s (R2 throttling or huge dump) |
| Restore | 30-600s | >1800s (investigate dump size + pod CPU) |
| Sanity queries | <5s | >60s (indexes not rebuilt, try `ANALYZE`) |

Total: expect **2-15 minutes** per run, depending on dump size.

## Common failure modes

| Symptom | Root cause | Fix |
|---|---|---|
| `R2 credentials secret enclii/r2-backup-credentials not found` | Wrong kubeconfig, or secret rotated | `kubectl config current-context`; re-auth |
| `Namespace dr-test exists — deleting` then hangs | Previous drill left finalizers | `kubectl get ns dr-test -o yaml`; remove stuck finalizer with `kubectl patch` |
| `downloaded backup is empty` | R2 list returned nothing, or `latest.sql.gz` matched by accident | re-run with `--backup-key` set to an explicit timestamped key |
| `psql: role "..." does not exist` during restore | Using a `pg_dumpall` dump that includes role grants the ephemeral DB does not know about | Harmless — restore continues with `ON_ERROR_STOP=0`. Verify sanity counts are non-zero |
| `ERROR: relation "public.daily_usage" does not exist` | The dump pre-dates the migration that added this table | Backup is older than 2025-01 — use `--backup-key` on a newer dump, or open an issue to update the sanity-query list |
| Restore pod OOM-killed | Dump too large for 2Gi limit | Increase Deployment resources in `infra/k8s/dr-test/ephemeral-postgres.yaml` |

## Safety notes

1. **Namespace scope**: the script hard-codes `DR_NAMESPACE=dr-test` and never
   references `enclii`, `data`, or any other production namespace for writes.
   Only reads: the single R2 credentials secret, copied into `dr-test`.
2. **R2 scope**: the aws-cli operations are `aws s3 ls` + `aws s3 cp` (GET).
   No `cp` in the other direction, no `rm`, no `rb`.
3. **Ephemeral password**: the drill Postgres boots with a password regenerated
   every run (`drpg-ephemeral-$(date +%s)`). Not suitable for shared access;
   that's the point.
4. **Cleanup**: `kubectl delete namespace dr-test` at the end. If you pass
   `--keep`, you are responsible for manual cleanup.
5. **Never run this on a cluster you haven't verified with
   `kubectl config current-context`**. A misrouted drill against a customer's
   cluster would fail-closed (namespace doesn't exist, R2 secret doesn't exist)
   but the principle stands.

## Rollback

Nothing to roll back — the script's only production side-effect is the appended
row in `internal-devops/runbooks/dr-log.md`. If the run was bogus, delete the
row manually and note why in the commit message.

## After the first real run

1. Update `internal-devops/runbooks/disaster-recovery.md`:
   - Set `Last Tested` to the drill date.
   - Replace the `Never` row in the DR Testing Log with the real result.
   - If observed RPO/RTO differ from the claimed numbers, update the RPO/RTO
     header values — that's the whole point of this exercise.
2. Update `status.madfam.io` trust-center page (P0.3) with the evidenced numbers.
3. Schedule the monthly cadence (first of each month) — either via calendar
   reminder or by wiring a CronJob wrapper later.

## CI / automation path (future)

This script is intentionally designed to be wrapped by a K8s CronJob later
(similar to the existing `postgres-restore-drill` CronJob in `data` namespace).
Phase 1 of the remediation plan will convert the operator-run flow to a
self-service monthly cadence, logging to the same dr-log.md.

## Related

- Manifest: [`infra/k8s/dr-test/ephemeral-postgres.yaml`](../infra/k8s/dr-test/ephemeral-postgres.yaml)
- Existing K8s Job-based drill: [`infra/k8s/production/backup/postgres-restore-drill.yaml`](../infra/k8s/production/backup/postgres-restore-drill.yaml)
  (this validates the `data` namespace Postgres is restorable into itself;
  `dr-drill.sh` validates the full end-to-end R2 → fresh cluster path)
- DR runbook: `internal-devops/runbooks/disaster-recovery.md`
- Drill log: `internal-devops/runbooks/dr-log.md`
- Remediation plan: `internal-devops/roadmaps/2026-04-enclii-remediation-plan.md` (P0.1)
