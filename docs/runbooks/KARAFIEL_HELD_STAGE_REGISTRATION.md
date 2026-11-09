# Held Karafiel staging registration

This human platform-operator endpoint records a separate held staging Service
and its native043 Deployment binding and audit event in one SQL transaction.
It never changes Kubernetes, production Services, auto-deploy flags on existing
Services, repository registrations, Git refs, credentials or database data.
Successful enrollment means `HELD_METADATA_REGISTERED`, not runtime readiness.

The closed identity is registry Service `karafiel-api-staging`, physical
Deployment/Service `karafiel-api`, persisted `staging` environment in
`enclii-karafiel-staging`, source `madfam-org/karafiel` at
`release/karafiel-staging:infra/k8s/overlays/staging`, and AppProject/Application
`karafiel-staging`. The pod label must be
`enclii.dev/service=karafiel-api-staging`. Production rows are never adopted or
renamed. Existing/duplicate IDs, names and namespace claims are refused.

## Order and reviewed plan

Deploy the accepted API source first. Establish the held parent Application and
fixed staging source, then materialize and synchronize the flat quarantine
source. It must declare exactly the five named Deployments at zero replicas,
with no Job, CronJob, HPA, PDB or additional executor workload. The selected live
API Deployment must also be held at zero. This metadata step does not prove all
pods terminated; the native database prepare proof checks that separately.

POST `/v1/admin/projects/karafiel/environments/staging/service-registration`:

```json
{"operation_id":"22000000-0000-4000-8000-000000000001","dry_run":true}
```

Choose a fresh canonical UUID before planning. Review the returned identities,
source revision, native UIDs/resourceVersions and `plan_sha256`. The operation
UUID deterministically identifies the prospective new Service. To apply once,
send the same UUID, `dry_run:false`, `expect_plan_sha256` with that exact hash,
and a bounded nonempty `reason`. Unknown/duplicate keys, arbitrary IDs/source,
commands, environment overrides and credentials are refused. Both route
middleware and the handler require the actual active SQL platform operator;
rollout flags and caller-supplied role labels do not grant this authority.

Apply re-reads intent and native/source evidence while holding existing043
project/service/intent locks. It inserts the new Service with auto-deploy false,
then its immutable binding and audit event before commit. A background writer
cannot observe a committed unbound staging Service. Ordinary deployment,
reconciliation, rollback/canary and callback writers use the same043 fence.
Observational health refreshers can still update descriptive metadata; health
fields are never readiness or ownership proof.

## Pending namespace ownership

Metadata enrollment can record the actual namespace UID/resourceVersion with
the actual project UUID owner label, or with only the exact legacy `karafiel`
label/absent label under this closed held authority. Foreign owners refuse.
The response always says `activation_held:true` and explicitly reports
`namespace_ownership_pending`. The latter is a historical registration fact,
not permission to use the namespace. Enrollment performs no label update and
makes no ownership, signature-enforcement, READY or database claim.

The immutable binding also records the actual parent Application UID and its
reviewed resourceVersion. Parent source, child tracking and the parent's
synced child resource must corroborate the held source. Native043 still
requires an explicitly configured actual Argo tracking method; absent or
unsupported configuration refuses the plan.

Native database prepare must freshly verify the same namespace, Application,
parent and Argo configuration identities, held source, exact quarantine and
all-workload termination. Only that separate bounded operator path may CAS
adopt the project UUID and signature-enforcement labels. READY requires the
fresh actual UUID/signature labels. Later approved adoption of the same
namespace UID does not rewrite the historical pending flag; a replacement UID
or foreign owner remains a refusal.

## Receipts and acceptance boundary

GET the same endpoint with `/<operation_id>` to inspect the immutable receipt.
A reused UUID applies only to the exact request, actor and reviewed hash.
Changed intent conflicts. An uncertain commit triggers one same-UUID receipt
read, never insertion replay or an automatic retry. No live ownership, database
provisioning, release, production enrollment or deployment is established by
the source tests.

This profile does not relax generic production binding prerequisites. The
normal production binding dry plan must independently prove its persisted
registration/environment/source intent for each existing Service; missing
intent requires a separately reviewed diagnosis and bounded remediation.

## Boundary checkpoint

2026-10-25 — Platform engineering. This runbook describes public platform
contracts and source gates. Live custody, identifiers, topology and rollout
evidence remain in private `internal-devops`. See the
[public repository boundary](../PUBLIC_REPO_BOUNDARY.md).
