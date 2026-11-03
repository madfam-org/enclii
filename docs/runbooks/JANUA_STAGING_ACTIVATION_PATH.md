# Shortest path from held Janua source to isolated staging

Boundary checkpoint (2026-11-03, platform engineering): source contracts and
synthetic tests only. No native readiness, credential custody, activation or
promotion is claimed. Native identities, costs and receipts belong in the
[private operations record](https://github.com/madfam-org/internal-devops).
See the [public repository boundary](../PUBLIC_REPO_BOUNDARY.md).

## Small prerequisite reader

`observeJanuaStagingPrerequisites` is an internal read-only helper, not an HTTP
execution route, a kernel gate or a runtime qualification receipt. It reuses the
held-native-authority, catalog, namespace/DNS baseline, canonical Kubernetes API
peer, current CNPG operator and retained StorageClass readers. It reads only the
existing project's monthly Waybill budget, throttle and evaluator metadata. Unknown budget/throttle reads are null, never a false not-throttled verdict.
It repeats the observations, rejects stale/foreign authority or catalog, and
withholds its metadata fingerprint on concurrent changes. Unknown reads remain
explicit gates; no raw native errors, endpoint addresses, policy bodies, node
identities, budget amounts or credentials appear in its diagnostic.

It makes no Secret read, dry admission or native mutation, and does not inventory
cluster-wide workloads. A catalog price of zero, generic monthly budget, Retain
StorageClass or exact policy metadata cannot clear packet isolation, actual
placement, current spend or operation-specific cost approval. Those gates stay
blocked. Do not copy its metadata hash into a database/migrator plan and call it
qualification. No existing kernel accepts this diagnostic.

## Existing versus missing operations

The dependency order below is about a real staging API, not a source percentage.
Held source must first be reviewed, merged and deployed; availability in this
branch is not availability on the running Enclii server.

| Step | Existing implementation to reuse | Minimal remaining source or owner action |
| --- | --- | --- |
| Establish fixed staging ownership | Persisted Janua project/environment/repository registration; `ops apps staging-intent`; migration045 journal | Owner reviews/records the exact held intent. Never retarget global production onboarding or create a fake active Service. |
| Prepare the application namespace | `POST /v1/admin/projects/janua/environments/staging/bootstrap`; `PrepareOwnedStagingNamespace` | Owner reviews dry-run before separate apply. This helper accepts only names ending `-staging`; it cannot prepare the addon namespace. |
| Materialize held identity | Closed `POST .../inert-materialization` and `GET .../inert-materialization/{operation_id}`; migration046 | Owner reviews the source revision and exact plan before apply. These create six inert objects only; no published materialization CLI exists. |
| Prepare the addon namespace and database plan | `januastagedb.PlanFor`, current catalog, SQLJournal048; existing owned-namespace primitives | Add a closed Janua-specific orchestration phase for the fixed `project-<project UUID prefix>` namespace, signature label, retained storage and no shared-data labels. Do not call legacy `EnsureNamespace`, which adds shared-data access. Bind owner-approved allocation/cost and current budget/spend evidence to the exact reviewed plan; no general reservation system is needed. |
| Establish narrow networking | Existing canonical API-peer/operator readers and closed NetworkPolicy builders are patterns; DB/migrator require fresh network callbacks | Add one journalled fixed network operation that retains exact namespace/policy UIDs and specs, refuses broad additives and binds the DB/migrator operation. Include API→DB, API→Redis, CNPG peers/operator/control-plane and the exact migrator path. A native allow/deny probe is still needed before runtime qualification; policy admission alone cannot supply it. |
| Establish dedicated image-pull custody | Migrator has a fixed `janua-staging-registry` reference and mandatory registry reader/gate | Add a dedicated repo-scoped custody adapter/receipt with exact namespace/Secret UID/version, then wire the source and registry observers. The held API references `ghcr-credentials` in its own namespace: that name must hold dedicated staging custody, not copied production/shared data. No supported Janua registry intake target exists yet. |
| Create DB and migrate | `januastagedb.Kernel` DryAdmit/Apply/ObserveReadiness/RuntimeCustody/OwnerCustody; `januastagemigrate.Kernel` Start/Inspect/Abort/Cleanup; journals048/049; exact certificate/CI/source observer | Add thin closed platform-operator orchestration and plan/apply/status APIs over these kernels after the mandatory readers qualify. No Janua DB/migration execution command currently exists. Do not weaken the generic `*_sim` rule, add caller SQL or grant privileges manually. |
| Project fresh runtime dependencies | Typed crypto/Redis initialization and existing DB/CA intake targets; Janua's three staging ESO declarations | Owner initializes complete fresh crypto/Redis groups. Add server-to-server verified runtime/public-CA intake from the DB kernel and exact ESO/PVC projection. Never export owner DSNs or CA keys. |
| Release the hold and start | Existing inert Deployment/Service/App identities and held Janua overlay | Add one retained held→running transition: verify exact completed source/DB/migration/custody/network evidence; permit only the reviewed ESO/PVC kinds and replica release; retain new native UIDs/specs. A replicas-only PR cannot do this: the inert AppProject allows only Deployments/Services. The current inert observer deliberately rejects changed specs/nonzero replicas, so it cannot be reused as the running-state observer. |
| Smoke, auth, soak and promote | Janua `scripts/staging/public_probe.py` supplies five public GETs; production promotion hold is explicit | Add staging-only synthetic principal/client setup and auth/refresh tests, actual native network-denial/transport checks, and an append-only continuous runtime observer. Require at least 1800 healthy seconds with ownership/image/readiness/key continuity; gaps or changes reset the interval. The existing public probe cannot substitute for native evidence. Promotion stays a separate owner decision. |

The next source increment that removes the DB blocker should therefore be the
**fixed network operation and narrow addon-namespace/DB orchestration**, with
an exact plan/cost approval bound to the existing operation journal. The separate
registry-custody adapter can proceed independently. Do not expand this reader
into a generic resource dashboard.

A live connection test has a sequencing constraint: a real DB connection cannot
be proven before the DB exists. Before creation, qualify the configured closed
policy set and its native lineage; before activation, prove actual allowed and
denied paths against the created DB/Redis. If the pre-create gate requires packet
proof, use a bounded, credential-free temporary listener/probe operation with its
own retained native UIDs, rather than treating an absent destination/timeout as a
denial proof. No such probe executor is implemented by this change.

## Unavoidable owner review and supported commands

Inspection is available through the released Enclii API/CLI. These do not apply
resources or authorize the held kernels:

```sh
enclii ops capabilities --json
enclii ops apps staging-intent --project janua --operation-id "${JANUA_STAGE_INTENT_ID:?}" --json
enclii addon staging-plan --project janua --plan standard-0 --json
enclii secrets intake targets --json
```

After reviewing an intent plan, its existing CLI can record the same operation:

```sh
enclii ops apps staging-intent --project janua --operation-id "${JANUA_STAGE_INTENT_ID:?}" \
  --record --expect-plan "${JANUA_STAGE_INTENT_PLAN_SHA256:?}" \
  --reason "retain reviewed isolated Janua staging intent" --json
```

Namespace and inert materialization currently have the closed APIs above, not
invented CLI equivalents. Review their dry-run first; apply is a separate owner
action with its reason/exact fingerprint. For custody, review one receipt before
running the next command; refuse existing/partial/concurrent groups:

```sh
enclii secrets intake submit janua-staging/runtime-crypto \
  --generate jwt_private_key,jwt_public_key,jwt_secret_key,secret_key,field_encryption_key \
  --reason "initialize fresh isolated Janua staging crypto"
```

Then, after that separate receipt review:

```sh
enclii secrets intake submit janua-staging/redis \
  --generate redis_password,redis_url \
  --reason "initialize fresh isolated Janua staging Redis"
```

Allocation/cost approval, missing native execution phases, replica release,
synthetic identity creation and promotion still require their concrete reviewed
capabilities. No raw cluster/Vault/SQL fallback or credential-paste step is
provided, and no command here has been executed by this source change.
