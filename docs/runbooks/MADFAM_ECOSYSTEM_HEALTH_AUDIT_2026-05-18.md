# MADFAM Ecosystem Health Audit - 2026-05-18

Snapshot time: 2026-05-18T23:46:53Z

## Bottom Line

Do not claim 100% ecosystem health yet.

Production Enclii is serving and the project-card service-row projection was
previously verified all green, but the wider evidence set still has one
confirmed degraded Argo application and several places where the dashboard can
produce false positives or false negatives.

Confirmed facts from this wrap-up:

- `https://app.enclii.dev/projects` returned HTTP 200.
- `https://api.enclii.dev/health` returned `healthy` for API, database, cache,
  and Kubernetes.
- Argo CD showed every application `Synced/Healthy` except
  `phynd-crm-staging`, which is `Synced/Degraded`.
- `ExternalSecret/phynd-crm-staging-secrets` is `Ready=False` with
  `SecretSyncedError: could not get secret data from provider`.
- `phynd-crm-staging` web and worker pods are pending because the target secret
  has not materialized.
- The zero-touch boundary check passed with
  `scripts/check-zero-touch-boundaries.sh`.
- Latest GitHub checks observed for commit `531c1b4d` were green for Public
  hygiene, CI Pipeline, and Integration Tests.

## What Is Healthy

- `core-services` is `Synced/Healthy` at recovery revision
  `c484c83d0ac6582c3536044dc0fa18deff31e840`.
- Live Switchyard images were verified earlier in the session:
  - `switchyard-api`:
    `ghcr.io/madfam-org/enclii/switchyard-api@sha256:7ae754e2288b6f8e857dd41d3bd3be317c695403b03b165c27e98d5fd7459bb5`
  - `switchyard-ui`:
    `ghcr.io/madfam-org/enclii/switchyard-ui@sha256:6a7fc48df7618f7aa676537310ecd96fd185b16e1089b718c6c967dfa9c215ff`
- Production MADFAM apps including ForgeSight, Tulana, and Phynd CRM were
  verified `Synced/Healthy` after the earlier remediation.
- The Switchyard card-facing service projection was verified earlier as
  `27` projects and `87` services, all `healthy/running`.

## What Is Not Healthy

`phynd-crm-staging` remains the hard blocker for strict all-Argo green.

The immediate cause is missing Vault provider data for
`secret/phynd-crm-staging`. ExternalSecrets cannot read the source data, so the
Kubernetes target Secret is absent and the staging web/worker pods cannot start.

Required operator action:

1. Populate `secret/phynd-crm-staging` with staging-only values.
2. Refresh `ExternalSecret/phynd-crm-staging-secrets`.
3. Verify `Ready=True`.
4. Reconcile `phynd-crm-staging`.
5. Confirm staging web and worker rollouts complete.

Do not copy production values into staging.

## False Positive Risks

These are places where Enclii can look healthier than reality:

- The project-card database projection can show all service rows healthy while
  Argo/Kubernetes still has a degraded workload outside that projection.
- `phynd-crm-staging` proves that Argo health must remain part of the truth
  contract, not just card-facing service rows.
- Some workloads are represented unevenly between Argo/Kubernetes and the
  Switchyard service table. Examples observed during the audit include
  `autoswarm-services`, `blueprint-harvester-services`, and
  `converge-dash-services`.
- Historical `deployments` rows include contradictory states such as
  `failed/healthy` and `running/unhealthy`. A card that trusts the latest raw
  deployment row can show the wrong label.
- Some service health timestamps observed earlier were stale even while
  reconciliation timestamps were fresh. Freshness must be explicit.

## False Negative Risks

These are places where Enclii can look worse than reality:

- Historical failed build pods in `enclii-builds` are numerous. They should not
  mark a current project blocked unless tied to an active, unsuperseded process.
- Historical backup/check pods in `data` can make cluster-wide pod scans look
  unhealthy even when current stateful services are ready.
- Scale-to-zero deployments must not be treated as broken just because ready
  replicas are zero.
- Browser tabs with old Switchyard UI chunks may still call older project
  endpoints after a deployment and show stale card facts until refreshed.

## Project Cards And Process Feed

The current card/process-feed model is not yet trustworthy enough to call
truthful under incident conditions.

Required remediation:

1. Cards must display a timestamped evidence model that separates:
   service-row health, Argo app health, Kubernetes rollout health, and active
   process state.
2. Process feeds must classify terminal historical failures as terminal or
   superseded, not active blockers.
3. Raw deployment status must be normalized before display. Contradictory
   states like `failed/healthy` cannot map directly to user-facing labels.
4. Freshness must be enforced. Stale health checks should become `stale`, not
   silently remain `healthy`.
5. The backend must expose coverage gaps where Argo/Kubernetes workloads do not
   have matching Switchyard service rows.
6. `/` and `/projects` must continue using the same `/v1/projects/cards`
   aggregate, with tests proving both dashboards render identical truth.

## Zero-Touch Boundary

The zero-touch policy check passed:

```bash
scripts/check-zero-touch-boundaries.sh
```

This verifies the current repo-level boundary invariants for avoiding
client-repo onboarding changes inside Enclii itself. It does not prove runtime
truthfulness of every card; it only validates that the checked code paths still
respect the zero-touch boundary.

## Verification Commands Used

```bash
curl -sS -o /dev/null -w '%{http_code}\n' https://app.enclii.dev/projects
curl -sS https://api.enclii.dev/health
kubectl -n argocd get applications -o custom-columns=NAME:.metadata.name,SYNC:.status.sync.status,HEALTH:.status.health.status,REV:.status.sync.revision --sort-by=.metadata.name
kubectl get pods -A --field-selector=status.phase!=Running,status.phase!=Succeeded -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,PHASE:.status.phase,REASON:.status.reason --sort-by=.metadata.namespace
kubectl -n phynd-crm-staging get externalsecret phynd-crm-staging-secrets -o 'custom-columns=NAME:.metadata.name,READY:.status.conditions[0].status,REASON:.status.conditions[0].reason,MESSAGE:.status.conditions[0].message'
scripts/check-zero-touch-boundaries.sh
gh -R madfam-org/enclii run list --limit 10
```

No secret values belong in this document.
