# Karafiel production writer exclusion enrollment

This closed human platform-operator path enrolls each of the five existing
production Services in native043 without editing those Services, their
auto-deploy flags, environment/onboarding configuration, namespace labels,
Kubernetes resources, images or Git refs. Its purpose is to exclude independent
native deployment/retarget writers before the controlled source handoff.
`WRITER_EXCLUSION_REGISTERED` is not application, database, release or compliance
readiness. It does not disable Argo automation or suspend financial/fiscal work.

## Legacy prerequisite and bounded compatibility

Generic native043 binding requires persisted repository intent. Imported
Services may have an empty `git_repo` despite a completed exact
`madfam-org/karafiel` registration. The generic validator continues to refuse
that gap. This separate profile accepts only a blank legacy repository field,
or an already canonical matching repository, corroborated by that unchanged
completed registration and the actual tracked main source. Foreign nonempty
repositories and arbitrary missing metadata remain refusals. No import record
or onboarding repair is created.

The exact existing Service names and physical Deployment names are
`karafiel-api`, `karafiel-web`, `karafiel-admin`, `karafiel-worker` and
`karafiel-beat`, all in the actual persisted `production` environment and
`karafiel` namespace. The actual `karafiel-services` Application must be synced
to coherent `main:infra/k8s/production` source and tracked by the observed
`enclii-infrastructure` parent through the actual `project-applications`
ApplicationSet. The Application's sole controller owner must match that native
ApplicationSet UID. Its exact Enclii/main Git-file generator and template must
resolve the fixed Karafiel source/destination. The fixed project config is
read at the observed root's immutable synced revision and its hash is bound;
direct-root fallback, foreign generators/templates and config overrides refuse.
ApplicationSet and root UIDs/resourceVersions, config revision, Argo
configuration, namespace and Application identities are bound. The immutable declared revision
must explicitly reference flat resources corroborating every Deployment's
selector/container identity and actual immutable image. All five existing
Service IDs, Deployment UIDs and declared identities enter the fleet
fingerprint. Any missing, duplicate, foreign or incoherent member refuses.

Namespace owner labels may be the actual project UUID, absent, or the exact
legacy `karafiel` value only. Legacy/absent labels are recorded as historical
`namespace_ownership_pending`; they never grant namespace ownership or READY.
Foreign labels refuse. No namespace label changes occur. The actual App's
automation state is observed through its native resourceVersion; this
data-only fence does not require changing it first.

## Plan, review and apply each existing target

POST `/v1/admin/projects/karafiel/environments/production/target-enrollment`
using the existing closed native043 request schema:

```json
{
  "dry_run": true,
  "scope": {"project": "karafiel", "service": "<EXISTING_SERVICE_UUID>", "environment": "production"},
  "args": {"namespace": "karafiel", "deployment": "karafiel-api"}
}
```

Use the actual existing Service UUID and its exact physical name. Review all
identity/source fields, `source_fleet_sha256`, actual images and the returned
`plan_sha256`. Apply the same scope/args with `dry_run:false`, that exact
`expect_plan_sha256`, a fresh canonical `idempotency_key` and a bounded nonempty
`reason`. There are no caller-selected refs, repository/ownership overrides,
repair flags, credentials, commands or provider operations.

Apply checks the actual active platform operator independently of rollout
flags. It locks the project and all current child Services, locks the existing
environment/registration, then freshly reads native/source evidence and
compares the reviewed plan. Only the selected target's immutable043 binding
and audit event are inserted atomically. An audit failure rolls both back.
Repeated intent requires the same actor/request/hash; unknown commit performs
one same-UUID receipt read with no automatic insertion retry.

Read each immutable event through the existing
`GET /v1/ops/apps/bind-target/<idempotency_key>` route. Complete and verify all
five receipts before the source/main handoff; one receipt does not prove the
other four are fenced. Source CI/deployment and actual receipt/readback remain
required. No live enrollment is established by synthetic source tests.

## What the fence does

Existing native043 locks exclude ordinary automatic deployment callbacks,
queued/controller deployment/rollback/canary and runtime retarget paths for a
bound Service, even while its original auto-deploy flag remains true. The
generic admin reconciliation path also refuses bound namespace repair and
observational health refresh. Other cluster observation can still refresh
descriptive health metadata; it cannot create or retarget runtime and is not
ownership/readiness proof. In-flight remote operations and independent Argo
automation require their own observed hold/convergence checks.

The closed source authority is for current-main enrollment only. After a
reviewed fixed-ref handoff, the binding still excludes legacy native writers;
generic bound runtime mutations fail closed when source intent no longer
matches. No automatic rebind, source-authority refresh or activation exists.

Estate-wide Argo `apps.sync`, `sync-sweep`, `retire` and `rollback` routes also
require the actual database-resolved platform rank, independent of tenant-scope
rollout flags. A tenant admin role alone cannot use those general routes to
bypass the narrow release capability. Platform operators retain their reviewed
ordinary adapter paths; project-scoped native generation rules are unchanged.

## Boundary checkpoint

2026-10-25 — Platform engineering. This runbook describes public platform
contracts and source gates. Live custody, identifiers, topology and rollout
evidence remain in private `internal-devops`. See the
[public repository boundary](../PUBLIC_REPO_BOUNDARY.md).
