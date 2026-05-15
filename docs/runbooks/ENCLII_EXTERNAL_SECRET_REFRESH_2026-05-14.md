# Enclii ExternalSecret refresh adapter - 2026-05-14

## Status

Implemented in Switchyard API code; production availability depends on the Switchyard API rollout reaching the running service.

## Purpose

`ops.secrets.refresh` gives agents and operators an Enclii-first way to request ExternalSecrets reconciliation without using raw Kubernetes commands against production.

The adapter does not read, print, or write secret values. It only patches reconciliation/audit annotations on the target ExternalSecret.

## Apply behavior

An apply request requires a reason and a target ExternalSecret name:

```text
enclii ops secrets refresh forgesight-secrets \
  --namespace forgesight \
  --apply \
  --reason "retry ExternalSecret reconciliation after provider data update" \
  --idempotency-key refresh-forgesight-secrets-20260514
```

The adapter patches:

```text
force-sync=<unix timestamp>
enclii.dev/last-ops-operation=ops.secrets.refresh
enclii.dev/last-ops-reason=<operator reason>
enclii.dev/last-ops-requested=<RFC3339 timestamp>
enclii.dev/refresh-requested-at=<RFC3339Nano timestamp>
enclii.dev/last-ops-idempotency-key=<optional key>
```

## Limits

This adapter only asks ExternalSecrets to reconcile. If `SecretSyncedError` persists, the backing provider path is still missing or invalid and must be fixed through the approved secret-management workflow.

For the current production blockers, this means:

```text
forgesight/forgesight-secrets: refresh can retry sync, but provider data must exist.
phynd-crm/phynd-crm-secrets: refresh can retry sync, but provider data must exist.
```

## Pod diagnosis dependency

When dependent pods fail before container startup, use:

```text
enclii ops pods diagnose <workload-prefix> --namespace <namespace> --json
```

The local Switchyard API now includes container waiting state, reason, and
message in the response. This is required for failures such as:

```text
CreateContainerConfigError: secret "<name>" not found
```

Without that field, agents were forced into a read-only break-glass Kubernetes
query to discover the missing Secret name.

## Acceptance gate

After the adapter is live, the expected loop is:

```text
enclii ops secrets refresh <name> --namespace <namespace> --apply --reason <reason>
enclii ops secrets external --namespace <namespace> --json
```

The target ExternalSecret must eventually report `Ready=True` before dependent pods or jobs can be treated as production-ready.

## Dry-run contract clarity

Switchyard API now emits an apply-aware dry-run plan for apply-capable ops actions such as `jobs.trigger` and `secrets.refresh`. A configured adapter returns `ready_to_apply`; an unconfigured adapter returns a warning instead of the misleading generic `apply is blocked` fallback.

## 2026-05-14 deployment blocker: Git-source build publication

Current status: the concrete `ops.secrets.refresh` and `ops.apps.retire` adapter implementation is present in the local Enclii source tree, but production Switchyard API is still serving the generic operation-contract handler for those operations.

Evidence gathered through Enclii:

- `enclii ops capabilities --json` advertises `apps.retire` and `secrets.refresh`.
- Dry-runs for `ops.secrets.refresh` and `ops.apps.retire` still report `adapter execution is not wired in this build; dry-run is safe, apply is blocked`.
- `enclii deploy -f .enclii.yml -e prod --wait` from the Enclii repo created release `v20260514-094019-3102a28`, but timed out after 10 minutes.
- Roundhouse worker logs show Kaniko builds failing against Docker Hub with `TOOMANYREQUESTS` for `golang:1.25-alpine` on remote Git-source builds.
- Local Dockerfiles already point the Enclii Go base images at `public.ecr.aws/docker/library/...`; the live builder is cloning Git SHAs that do not include the local unpublished changes.

Remediation path:

1. Commit and push the Enclii changes that replace unauthenticated Docker Hub base-image pulls with non-Docker-Hub mirrors and wire concrete operation adapters.
2. Re-run `enclii deploy -f .enclii.yml -e prod --wait` from the Enclii repo.
3. Confirm dry-run output changes from `planned`/`adapter execution is not wired` to `ready_to_apply` for `ops.secrets.refresh` and `ops.apps.retire`.
4. Apply `ops.secrets.refresh` for `forgesight-secrets` and `phynd-crm-secrets` through Enclii.
5. Apply `ops.apps.retire` for `phyne-crm-production` through Enclii to remove shared-resource ownership conflicts with `phynd-crm-services`.
