# MADFAM Slice Full Remediation - 2026-05-18

## Scope

Target: `https://app.enclii.dev/projects` shows the MADFAM slice with all
listed services synced and healthy after the Vault reboot recovery.

Services in scope:

- `forgesight-services`
- `tulana-services`
- `phynd-crm-services`
- `digifab-quoting-services`
- `rondelio-services`
- `ceq-services`
- `phynd-crm-staging`

## Current State

- `https://app.enclii.dev/projects` returns HTTP 200.
- `core-services` is `Synced/Healthy` at recovery revision
  `c484c83d0ac6582c3536044dc0fa18deff31e840`, with the verified
  `switchyard-api` and `switchyard-ui` image digests deployed.
- `forgesight-services`, `tulana-services`, and `phynd-crm-services` are
  `Synced/Healthy`.
- `digifab-quoting-services`, `rondelio-services`, and `ceq-services` are
  `Synced/Healthy`.
- `ExternalSecret/forgesight-secrets` is `Ready=True` and ForgeSight runtime
  pods are running.
- The Switchyard database service projection has zero unhealthy service rows:
  all services with card-facing health facts are `healthy`, `running`, and have
  `ready_replicas >= desired_replicas`.
- `phynd-crm-staging` is `Synced/Degraded`. As of commit
  `1add140` in `madfam-org/phynd-crm`, the staging overlay owns
  `ExternalSecret/phynd-crm-staging-secrets`; ESO now reports
  `SecretSyncedError` because Vault path `secret/phynd-crm-staging` does not
  exist yet. Pods remain blocked by `CreateContainerConfigError` until ESO
  materializes the target Secret. Do not copy production values into staging.
- ARC blue is `Synced/Healthy` after removing unsupported listener HTTP probes,
  preserving controller-generated listener RBAC with
  `argocd.argoproj.io/compare-options: IgnoreExtraneous`, and updating the
  stuck-runner watchdog for ARC v0.14 `.status.workflowRunId`, pod phase, and
  `pods/log` inspection.
- `enclii-infrastructure` and `external-secrets-config` are `Healthy` but
  `OutOfSync`; these are infrastructure drift items, not project-card health
  blockers.

### Distance to 100% MADFAM-slice health

- **Production project-card health:** 3/3 named projects are `Synced/Healthy`
  (`forgesight`, `tulana`, and `phynd-crm`).
- **Wider MADFAM slice service health:** zero unhealthy Switchyard service rows
  remain in the database projection.
- **Blocking item:** 1 (`phynd-crm-staging`), caused by missing Vault source
  material for `secret/phynd-crm-staging`.
- **Deck and /projects truth status:** both `/` and `/projects` are wired to the
  same `/v1/projects/cards` backend aggregate. The aggregate uses backend
  service/release facts and rollout inspection, not UI-side MADFAM product
  inference. Production now runs the matching `switchyard-api` and
  `switchyard-ui` images; already-open browser tabs may need a hard refresh to
  discard older client chunks.
- **Status catalog ownership:** runtime status projection is now the default,
  and ArgoCD ignores only the runtime-owned `services-config` key on
  `status-config-enclii` and `status-config-madfam`, preventing self-heal from
  undoing zero-touch service catalog projection.
- **Runtime ESO refresh ownership:** ArgoCD also ignores Enclii's operator
  refresh annotations on project `ExternalSecret` resources, so approved
  runtime secret refreshes do not leave otherwise healthy client apps
  permanently `OutOfSync`.
- **Readiness for full green:** production project cards are green; strict
  all-Argo green still requires PhyndCRM staging Vault backfill and cleanup of
  the two healthy-but-`OutOfSync` infrastructure apps.

### What “fully healthy and truthful” means for `/projects`

`/projects` is not complete until all production card-backed applications are
`Synced/Healthy` and their cards show:

1. No `degraded`, `blocked`, `progressing`, or `unknown` aggregate status.
2. A valid `latest_deployment_id` and rollout metadata for services that are in
   the rollout path.
3. No break-glass-only dependency; secret material must come from Vault-backed
   ESO sources.

## Remediation Plan

1. Install PhyndCRM staging secret.
   - Generate staging-only values for every required key in
     `phynd-crm/infra/k8s/staging-secrets-template.yaml`.
   - Write those values to Vault path `secret/phynd-crm-staging` using
     lower-snake-case property names that match the staging ExternalSecret.
     From the `phynd-crm` repo, use the PP.5 writer so values are validated and
     not printed:
     ```bash
     VAULT_TOKEN_FILE=/secure/vault-token \
       node scripts/pp5-write-staging-vault.mjs \
         /secure/path/phynd-crm-staging.env
     ```
   - Refresh `ExternalSecret/phynd-crm-staging-secrets` and verify it reports
     `Ready=True`.
   - Keep staging DB, Redis, Janua/OIDC, webhook, provider API, SMTP, and
     payment/billing credentials distinct from production.
   - Reconcile the Argo CD Application and wait for web and worker rollouts.

2. Validate PhyndCRM PP.5 bootstrap.
   - Confirm `https://staging-phynd.app/api/health` returns HTTP 200.
   - Run the PP.5 wave-0 checks in the PhyndCRM repo.
   - Run synthetic webhook probes only after staging provider destinations and
     distinct HMAC secrets exist.
   - Capture proof that staging probes create no production rows, jobs, emails,
     payment events, grants, or provider artifacts.

3. Reconcile healthy infrastructure drift.
   - Inspect and intentionally resolve `external-secrets-config`, currently
     `OutOfSync` on `Secret/digifab-quoting/digifab-quoting-secrets`.
   - Inspect and intentionally resolve `enclii-infrastructure`, currently
     `OutOfSync` on child app declarations including `external-secrets`,
     `external-secrets-config`, `network-policies`, `vault`, and
     `project-applications`.
   - Do not overwrite runtime-owned secret refresh annotations or status
     projections while clearing this drift.

4. Clear pipeline health gates.
   - Resolve the GitHub-hosted runner billing/spending-limit blocker that causes
     ForgeSight `CI`, `Test Suite`, and `Test Automation and CI/CD` jobs to fail
     before starting.
   - Keep `madfam-runners-blue` listener probes disabled unless the listener
     image exposes a real health endpoint.
   - Keep the Kyverno ARC listener RBAC annotation policy installed so generated
     listener RBAC stays ignored by Argo CD pruning.
   - Keep the stuck-runner watchdog RBAC and JSONPath current with ARC v0.14
     status fields, including `pods/log` access for cancellation detection.
   - Re-run failed workflows after billing/runners are healthy.
   - Keep self-hosted deploy workflows on the MADFAM runner pool.

5. Lock in UI/UX truthfulness checks.
   - Keep the shared rollout transform unchanged and validated by unit tests.
   - Require a periodic regression check after each prod rollout:
     - `https://app.enclii.dev` and `https://app.enclii.dev/projects` show identical aggregate states for MADFAM cards.
     - A project marked unhealthy must show an explicit rollout state reason.
     - A service blocked at pod level must not appear healthy in either screen.

## Verification Commands

```bash
curl -I --max-time 10 https://app.enclii.dev/projects
kubectl -n argocd get applications forgesight-services tulana-services phynd-crm-services phynd-crm-staging -o wide
kubectl -n forgesight get externalsecret forgesight-secrets
kubectl -n phynd-crm-staging get externalsecret phynd-crm-staging-secrets
kubectl -n phynd-crm-staging get secret phynd-crm-staging-secrets
kubectl -n phynd-crm-staging get pods
kubectl -n argocd get application core-services phynd-crm-staging external-secrets-config enclii-infrastructure -o wide
kubectl -n enclii get deployment switchyard-api switchyard-ui -o wide
```

## Bitwarden Safe Note Template

Populate one entry per credential set. Do not store placeholder values as if
they were active credentials. Store only environment-specific values.

```text
Safe Note: MADFAM Vault Reboot Recovery

Credential #1: Vault root/operator token
Vault address:
Vault path / provider account:
Token type / purpose:
Policy or scope name:
TTL / expiry:
Created:
Last rotated:
Rotation / revocation notes:
Unseal / recovery key custody notes:
Verification command:

Credential #2: Vault Enclii/Ops tokens (secret-refresh, if any)
Vault address:
Vault path / provider account:
Token type / purpose:
Policy or scope name:
TTL / expiry:
Created:
Last rotated:
Rotation / revocation notes:
Unseal / recovery key custody notes:
Verification command:

Credential #3: Cloudflare API token
Vault address:
Vault path / provider account:
Token type / purpose:
Policy or scope name:
TTL / expiry:
Created:
Last rotated:
Rotation / revocation notes:
Unseal / recovery key custody notes:
Verification command:

Credential #4: Porkbun API credentials
Vault address:
Vault path / provider account:
Credential type / purpose:
TTL / expiry:
Created:
Last rotated:
Rotation / revocation notes:
Unseal / recovery key custody notes:
Verification command:

Credential #5: GitHub deploy/admin tokens
Vault address:
Vault path / provider account:
Token type / purpose:
Policy or scope name:
TTL / expiry:
Created:
Last rotated:
Rotation / revocation notes:
Unseal / recovery key custody notes:
Verification command:

Credential #6: Database admin credentials
Vault address:
Vault path / provider account:
Environment:
Credential type / purpose:
TTL / expiry:
Created:
Last rotated:
Rotation / revocation notes:
Unseal / recovery key custody notes:
Verification command:

Credential #7: Redis admin credentials
Vault address:
Vault path / provider account:
Environment:
Credential type / purpose:
TTL / expiry:
Created:
Last rotated:
Rotation / revocation notes:
Unseal / recovery key custody notes:
Verification command:

Credential #8: Janua/SSO client secrets
Vault address:
Vault path / provider account:
Tenant / client id:
Token type / purpose:
Policy or scope name:
TTL / expiry:
Created:
Last rotated:
Rotation / revocation notes:
Unseal / recovery key custody notes:
Verification command:

Credential #9: SMTP/provider keys
Vault address:
Vault path / provider account:
Provider:
Credential type / purpose:
TTL / expiry:
Created:
Last rotated:
Rotation / revocation notes:
Unseal / recovery key custody notes:
Verification command:

Credential #10: Payment/billing provider keys
Vault address:
Vault path / provider account:
Provider:
Credential type / purpose:
TTL / expiry:
Created:
Last rotated:
Rotation / revocation notes:
Unseal / recovery key custody notes:
Verification command:

Credential #11: ForgeSight recovery values
Vault address:
Vault path:
Keys:
- API_KEY_SALT
- JANUA_JWT_SECRET
TTL / expiry:
Created:
Last rotated:
Rotation / revocation notes:
Unseal / recovery key custody notes:
Verification command:

Credential #12: PhyndCRM staging values
Vault address:
Vault path:
Namespace:
Depends on:
- DB
- Redis
- Janua/OIDC
- webhook
- provider API
- SMTP
- payment/billing
TTL / expiry:
Created:
Last rotated:
Rotation / revocation notes:
Unseal / recovery key custody notes:
Verification command:
```

## Completion Criteria

- All production project-card applications in scope are `Synced/Healthy`.
- `ExternalSecret/forgesight-secrets` is `Ready=True`; no break-glass-only
  ForgeSight secret material remains outside Vault.
- `ExternalSecret/phynd-crm-staging-secrets` is `Ready=True` from
  `secret/phynd-crm-staging`; no manual staging Secret is required.
- PhyndCRM staging web and worker pods are running, and PP.5 wave-0 passes.
- The projects page returns HTTP 200 and displays no unhealthy production
  MADFAM-slice services.
- Strict all-Argo green has no `Degraded` or `OutOfSync` applications.
- Safe-note custody metadata is complete for every credential set involved in
  the Vault reboot and staging/provider recovery.
