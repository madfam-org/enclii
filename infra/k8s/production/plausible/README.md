# Plausible Community Edition (SPEC ONLY)

> **SPEC ONLY. Nothing deploys these manifests.** No Argo Application, no
> `infra/argocd/projects/*/config.json` and no parent kustomization references
> this directory, and `tests/scripts/test_plausible_spec_only.py` fails if one
> does. Wiring is a separate, reviewed PR; the order is in
> [`RUNBOOK.md`](RUNBOOK.md).

First-party, cookieless web analytics for the MADFAM public sites, self-hosted
on Enclii at `https://plausible.madfam.io`.

## What is here

| File | What |
| --- | --- |
| `namespace.yaml` | Namespace `plausible` (platform tool, like `npm-registry`) |
| `externalsecrets.yaml` | `plausible-app-secrets`, `plausible-db-credentials`, `plausible-backup-credentials`, all from Vault |
| `postgres-cluster.yaml` | CNPG `plausible-db` (PostgreSQL 16.15, 1 instance, 2 Gi) plus a daily `ScheduledBackup` |
| `clickhouse.yaml` | `plausible-events-db` StatefulSet (ClickHouse 24.12.6, 5 Gi) and its config |
| `plausible.yaml` | Plausible CE v3.2.1 Deployment (createdb and migrate init containers), Service, config |
| `networkpolicies.yaml` | Default deny plus one allow policy per workload |
| `kustomization.yaml` | Renders the set (`kubectl kustomize infra/k8s/production/plausible`) |
| `RUNBOOK.md` | Wiring order, smoke test, backups, privacy and compliance |

## Why it lives in this repo

Self-hosted third-party tools the platform runs live in `enclii` with their own
namespace and are wired by one ApplicationSet project config, as Verdaccio is
(`infra/k8s/base/verdaccio`, `infra/argocd/projects/npm-registry/config.json`).
Plausible follows the same shape. A new repository would only hold upstream
manifests, and Plausible is not a tenant product with its own code.

## Design

- **Versions, verified 2026-11-10, pinned by digest:**
  - Plausible CE **v3.2.1**: the latest stable release, and the first without
    the unauthenticated `/storybook` RCE (CVE-2026-8467, affecting 3.0.0 up to
    3.2.0). That is the project's only published advisory.
  - ClickHouse **24.12.6-alpine** and PostgreSQL **16**, the majors CE
    v3.2.1 ships with. 24.12.6 includes the CVE-2025-1385 fix. 24.12 no
    longer gets upstream patches, so it is reachable only from the Plausible
    pod.
- **Sizing for tiny traffic.** All three pods together request 0.2 CPU and
  about 1 Gi of memory, with limits of 2.5 CPU and about 3 Gi. Storage is
  2 Gi for Postgres and 5 Gi for ClickHouse.
- **State.** Postgres holds the dashboard accounts, sites and goals.
  ClickHouse holds the events. The Plausible pod itself is stateless (its
  cache is an emptyDir).
- **Own databases.** Plausible never joins the shared Postgres, so its
  connections stay out of the shared budget.
- **Secrets.** Every secret is an ExternalSecret with explicit `data`
  remoteRefs, so a missing Vault key fails the sync visibly. Values are
  generated server-side by intake targets; no person types one.
- **Network.** Default deny. The tunnel reaches Plausible on 8000, Plausible
  reaches its two databases, and the CNPG operator reaches the instance on
  8000. Plausible has no internet egress.
- **Backups:**
  - Postgres: CNPG Barman base backups (daily) plus continuous WAL archiving
    to R2, under the `plausible/plausible-db` prefix of the addon backup bucket.
  - Both volumes: the Longhorn `high-churn` group, which keeps 2 local
    snapshots plus the daily R2 volume backup.
  - Losing ClickHouse data loses analytics history, not money or accounts.
    The volume backup is the recovery path.

## Before wiring

1. **Image admission.** Kyverno `restrict-image-registries` does not admit
   `ghcr.io/plausible/*`. Choose one:
   - **(a)** Copy the exact index digest into `ghcr.io/madfam-org` (for
     example `crane copy ghcr.io/plausible/community-edition@sha256:33e6…
     ghcr.io/madfam-org/enclii/plausible-community-edition:v3.2.1`). The
     digest is unchanged, so only the repository in `plausible.yaml` changes.
   - **(b)** Add `ghcr.io/plausible/*` to the policy.

   (a) keeps the allowlist as it is and is recommended. The guard test pins
   the gap, so it has to change in the same PR.
2. **Vault writer policy.** Add `secret/data/plausible` to
   `scripts/provision-switchyard-vault-writer.sh`, and re-run it, so intake can
   write there.
3. **Intake targets.** Add the three targets in [the runbook](RUNBOOK.md#1-intake-the-keys-operator-then-server-side-generation) to
   `apps/switchyard-api/internal/secretsintake/registry.yaml`, then mint them.
   That PR rebuilds switchyard-api.
4. **CNPG.** native `barmanObjectStore` is removed in CNPG 1.30. This cluster
   moves to the Barman Cloud Plugin with every other cluster.

## Wiring (one file, after the steps above)

`infra/argocd/projects/plausible/config.json`:

```json
{
  "name": "plausible",
  "repoURL": "https://github.com/madfam-org/enclii.git",
  "branch": "main",
  "manifestPath": "infra/k8s/production/plausible",
  "namespace": "plausible",
  "partOf": "enclii-platform"
}
```

The same PR updates `tests/scripts/test_plausible_spec_only.py`. It replaces
the not-wired assertions with a check that exactly this project config points
here.

## Route (described, not created)

`plausible.madfam.io` → `http://plausible.plausible.svc.cluster.local:8000`
through the shared Cloudflare tunnel. The route goes through the Enclii
tunnel-route procedure (dry run, review, apply with `--expect-plan`). The DNS
record is an owner step. Create the first dashboard account BEFORE the route
exists, because Plausible opens registration on its first launch.
