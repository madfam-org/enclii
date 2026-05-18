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
- `phynd-crm-staging` is `Synced/Degraded` because
  `phynd-crm-staging-secrets` is not installed. Pods are blocked by
  `CreateContainerConfigError`. Do not copy production values into staging.
- ARC blue is `Synced/Healthy` after removing unsupported listener HTTP probes,
  preserving controller-generated listener RBAC with
  `argocd.argoproj.io/compare-options: IgnoreExtraneous`, and updating the
  stuck-runner watchdog for ARC v0.14 `.status.workflowRunId`, pod phase, and
  `pods/log` inspection.

## Remediation Plan

1. Backfill ForgeSight Vault source of truth.
   - Write real production values for `secret/forgesight.api_key_salt` and
     `secret/forgesight.janua_jwt_secret` through the approved Vault/Enclii
     secret workflow.
   - Refresh `ExternalSecret/forgesight-secrets`.
   - Verify `kubectl -n forgesight get externalsecret forgesight-secrets`
     reports `Ready=True`.
   - Restart the API and discovery Deployments only after Vault is authoritative.

2. Install PhyndCRM staging secret.
   - Generate staging-only values for every required key in
     `phynd-crm/infra/k8s/staging-secrets-template.yaml`.
   - Install `phynd-crm-staging-secrets` in namespace `phynd-crm-staging`.
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

## Verification Commands

```bash
curl -I --max-time 10 https://app.enclii.dev/projects
kubectl -n argocd get applications digifab-quoting-services rondelio-services ceq-services forgesight-services phynd-crm-staging -o wide
kubectl -n forgesight get externalsecret forgesight-secrets
kubectl -n forgesight logs deploy/forgesight-discovery --since=5m --tail=120
kubectl -n phynd-crm-staging get secret phynd-crm-staging-secrets
kubectl -n phynd-crm-staging get pods
```

## Bitwarden Safe Note Template

Populate one entry per credential set. Do not store placeholder values as if
they were active credentials.

```text
Credential:
Vault address:
Vault path / provider account:
Token type / purpose:
Policy or scope name:
TTL / expiry:
Created:
Last rotated:
Rotation owner:
Revocation notes:
Unseal / recovery key custody notes:
Dependent Kubernetes Secret / ExternalSecret:
Verification command:
```

Credential sets to record:

- Vault root/operator token and any scoped Enclii secret-refresh tokens.
- Cloudflare API token.
- Porkbun API credentials.
- GitHub deploy/admin tokens.
- Database admin credentials.
- Redis admin credentials.
- Janua/SSO client secrets.
- SMTP/provider keys.
- Payment/billing provider keys.
- ForgeSight break-glass replacement values for `API_KEY_SALT` and
  `JANUA_JWT_SECRET`, after they are written to Vault.
- PhyndCRM staging-only DB, Redis, Janua/OIDC, webhook, provider API, SMTP, and
  payment/billing values.

## Completion Criteria

- All five Argo CD Applications in scope are `Synced/Healthy`.
- `ExternalSecret/forgesight-secrets` is `Ready=True`; no break-glass-only
  secret material remains outside Vault.
- PhyndCRM staging web and worker pods are running, and PP.5 wave-0 passes.
- The projects page returns HTTP 200 and displays no unhealthy MADFAM-slice
  services.
- Safe-note custody metadata is complete for every credential set involved in
  the Vault reboot and staging/provider recovery.
