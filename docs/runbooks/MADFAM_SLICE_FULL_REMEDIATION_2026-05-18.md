# MADFAM Slice Full Remediation - 2026-05-18

## Scope

Target: `https://app.enclii.dev/projects` shows the MADFAM slice with all
listed services synced and healthy after the Vault reboot recovery.

Services in scope:

- `digifab-quoting-services`
- `rondelio-services`
- `ceq-services`
- `forgesight-services`
- `phynd-crm-staging`

## Current State

- `https://app.enclii.dev/projects` returns HTTP 200.
- `digifab-quoting-services`, `rondelio-services`, and `ceq-services` are
  `Synced/Healthy`.
- `forgesight-services` is `Synced/Degraded` at revision `b09d1e6`. Runtime
  pods are up, and fixes have been deployed for discovery JSONB serialization,
  production `vendor_type` enum mapping, manual-review JSONB parameter casting,
  and Docker Hub pull-rate avoidance via the public ECR Python base image
  mirror. Discovery logs on the current image processed the previously failing
  candidate without repeating the SQL parameter type error. Its runtime secret
  is temporarily working from a break-glass Kubernetes Secret patch, but
  `ExternalSecret/forgesight-secrets` remains `Ready=False` until Vault
  contains `secret/forgesight.api_key_salt` and
  `secret/forgesight.janua_jwt_secret`.
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

### Distance to 100% MADFAM-slice health

- **Service-level health:** 3/5 services are currently `Synced/Healthy` (**60%**).
- **Blocking items:** 2 (`forgesight-services`, `phynd-crm-staging`).
- **Deck and /projects truth status:** both `/` and `/projects` now use the same
  rollout-aware project-card transform and now reflect rollout blocking states
  (for example `CreateContainerConfigError`, `ImagePullBackOff`, and `CrashLoopBackOff`)
  instead of masking them behind stale `healthy` rows.
- **Readiness for full green:** not yet true until both blockers are fixed and
  verified through ArgoCD + ESO reconciliation.

### What “fully healthy and truthful” means for `/projects`

`/projects` is not complete until all five target ArgoCD applications are
`Synced/Healthy` and their cards show:

1. No `degraded`, `blocked`, `progressing`, or `unknown` aggregate status.
2. A valid `latest_deployment_id` and rollout metadata for services that are in
   the rollout path.
3. No break-glass-only dependency (secret material must come from Vault-backed
   ESO sources for both ForgeSight and PhyndCRM staging).

## Remediation Plan

1. Backfill ForgeSight Vault source of truth.
   - Write real production values for `secret/forgesight.api_key_salt` and
     `secret/forgesight.janua_jwt_secret` through the approved Vault/Enclii
     secret workflow.
   - If the break-glass Kubernetes Secret is present and approved as the source
     for recovery, backfill Vault without printing values:
     ```bash
     VAULT_TOKEN_FILE=/secure/vault-token \
       scripts/backfill-vault-path-from-k8s-secret.sh \
         --namespace forgesight \
         --secret forgesight-secrets \
         --vault-path secret/forgesight
     ```
   - Refresh `ExternalSecret/forgesight-secrets`.
   - Verify `kubectl -n forgesight get externalsecret forgesight-secrets`
     reports `Ready=True`.
   - Restart the API and discovery Deployments only after Vault is authoritative.

2. Install PhyndCRM staging secret.
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

3. Validate PhyndCRM PP.5 bootstrap.
   - Confirm `https://staging-phynd.app/api/health` returns HTTP 200.
   - Run the PP.5 wave-0 checks in the PhyndCRM repo.
   - Run synthetic webhook probes only after staging provider destinations and
     distinct HMAC secrets exist.
   - Capture proof that staging probes create no production rows, jobs, emails,
     payment events, grants, or provider artifacts.

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
kubectl -n argocd get applications digifab-quoting-services rondelio-services ceq-services forgesight-services phynd-crm-staging -o wide
kubectl -n forgesight get externalsecret forgesight-secrets
kubectl -n forgesight logs deploy/forgesight-discovery --since=5m --tail=120
kubectl -n phynd-crm-staging get externalsecret phynd-crm-staging-secrets
kubectl -n phynd-crm-staging get secret phynd-crm-staging-secrets
kubectl -n phynd-crm-staging get pods
kubectl -n argocd get application forgesight-services phynd-crm-staging -o wide
kubectl -n enclii get deployment switchyard-api -o wide
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

- All five Argo CD Applications in scope are `Synced/Healthy`.
- `ExternalSecret/forgesight-secrets` is `Ready=True`; no break-glass-only
  secret material remains outside Vault.
- `ExternalSecret/phynd-crm-staging-secrets` is `Ready=True` from
  `secret/phynd-crm-staging`; no manual staging Secret is required.
- PhyndCRM staging web and worker pods are running, and PP.5 wave-0 passes.
- The projects page returns HTTP 200 and displays no unhealthy MADFAM-slice
  services.
- Safe-note custody metadata is complete for every credential set involved in
  the Vault reboot and staging/provider recovery.
