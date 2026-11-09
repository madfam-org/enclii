# Held Janua staging preregistration design

Last Updated: 2026-10-20

Status: held-intent source implemented; deployment and native qualification are
separate. Migration `045` adds inert intent history only. Its API/CLI records no
service row or native resource and does not close staging service-registration,
source-authority, database, workload or SSO gates.
Logical names and model contracts only; operational identities and observations
belong in `internal-devops` under the
[public repository boundary](../PUBLIC_REPO_BOUNDARY.md).

## Model boundary found before implementation

`services` is unique on `(project_id, name)` and has one optional namespace
string, but no environment or source-registration foreign key. The source
staging overlay currently declares `enclii.dev/service: janua-api`; that identity
cannot be duplicated inside a project already registering `janua-api`. Updating
the existing service's namespace would retarget its production identity.

The proposed separate identity is `janua-api-staging`. Its exact staging
Deployment, Service, Pod labels, selectors and consumers need independent source
alignment. A matching friendly name does not prove that alignment. Production
registration, selectors and namespaces remain outside this operation.

`onboarding_registrations.repo_full_name` is globally unique and its row carries
one `argocd_app_name` and `config_snapshot`. Its primary key can prove the
completed repository's project ownership; it cannot also assert a separate
staging Argo Application, namespace or source path. Rewriting that row to make a
new staging foreign key look authoritative would retarget existing intent.

Migration `043_service_deployment_targets` already provides immutable native
service/environment bindings. Its validator requires actual namespace,
Deployment and Argo identities plus the onboarding row's exact source intent.
It therefore cannot be used as an empty preregistration slot before those
objects exist, nor bypassed with labels or invented UIDs.

There is also an activation boundary: `GetByGitRepo` returns one matching row
without resolving ambiguity, and the PR webhook can create a preview from that
row without checking `auto_deploy`. A new service row with `auto_deploy=false`
is consequently insufficient to guarantee a dormant registration. A held intent
must stay outside `services` and every existing build, preview, deploy, domain,
network and discovery consumer.

## Proposed immutable intent

A dedicated intent record expresses the future registration without making
it actionable. Its identity includes:

| Field | Required meaning |
| --- | --- |
| `id` | Canonical caller-chosen operation UUID, retained across an uncertain result |
| `project_id` | Exact existing project foreign key, resolved from `janua` |
| `environment_id` | Exact existing same-project `staging` environment foreign key, with namespace `janua-staging` |
| `repository_registration_id` | Exact completed `madfam-org/janua` repository registration foreign key |
| `repository_registration_scope` | Constant `REPOSITORY_OWNERSHIP_ONLY`; never staging source or Argo authority |
| `proposed_service_name` | Constant `janua-api-staging`; no service row exists as a consequence |
| `proposed_repository` / `app_path` | Fixed Janua source URL and `apps/api` |
| `proposed_namespace` | Constant `janua-staging` |
| `proposed_manifest_path` / `ref` | `k8s/overlays/staging` / `main`, explicitly proposed rather than observed native source |
| `state` | Constant `HELD_PREREGISTRATION_ONLY` |
| `activation_held` | Required `true`, constrained in SQL and in the wire contract |
| `plan_sha256` / `request_sha256` | Exact reviewed identity projection and complete record request, including operation and actor |
| `actor_id` / `reason` | Server-resolved current operator identity and bounded private audit reason |

Use restrictive foreign keys and immutable update/delete/truncate guards.
Uniqueness must prevent a second intent for the same project/environment/fixed
proposed service. A conflicting row must never be silently adopted, replaced or
repaired. No actual service UUID, native UID, credential, URI, image, role grant,
workload readiness or active source authority may be fabricated in its payload.

This repository-ownership-only design can preserve the exact repository row
identity, but cannot satisfy a requirement that a source-registration foreign
key already prove **staging** authority. That requirement needs an explicit
environment-specific source-registration model first. It is the implementation
boundary, not permission to reinterpret the existing global onboarding row.

## Plan, record and read contract

The fixed operator interface accepts only an operation UUID for
planning. Recording additionally requires the reviewed fingerprint and a bounded
reason. There are no service, namespace, source, image, SQL, environment, secret,
workload or role selectors. Unknown/duplicate input keys and aliases refuse.

Planning performs narrow reads only: exact project, environment and repository
ownership IDs/status. It does not assert that a proposed service name is reserved
in `services`, or that materialization is safe. It avoids `GetByRepo` because its
broad SELECT includes the webhook secret, using a narrow joined identity
projection instead. No native
cluster connection or Secret read is needed.

Recording must atomically:

1. Recheck and lock the current server-resolved platform operator and same-project
   ownership rows. Keep a consistent lock order and bound lock/statement time.
2. Recompute the exact plan from the locked identities and compare it with the
   reviewed fingerprint. Changed environment/repository identity or conflicting
   registration refuses before inserting anything.
3. Insert only the immutable held intent and its atomic audit proof. Never call
   generic service creation, onboarding, addon provisioning or mutation helpers.
4. On an exact same-UUID replay, return the retained historical record only if
   the complete request/actor fingerprint matches. A different payload or key
   colliding with the fixed identity refuses. An uncertain commit is inspected
   using that UUID; no automatic replay or fresh UUID is chosen.

Reading one exact operation UUID is nonmutating and must still enforce actual
operator and project access. It returns a historical intent, not current
authority. Responses must use a closed validated projection with no raw reason,
upstream body, source snapshot, credential, subject or node metadata.

Every mode explicitly reports `activation_held:true`,
`service_materialized:false`, `native_binding_proven:false`, and
`materialization_supported:false`. These are required booleans, not defaults for
missing wire fields. Fingerprints cannot authorize materialization or release.

The immutable intent/audit table is separate from all existing services and
activation inventories. No background consumer is added. Recording does not
reserve a service name in other tables and creates no native workload binding.
An exact UUID replay returns retained history; a different UUID colliding with
the held environment slot refuses. Source ownership may later drift; historical
GET and exact replay never claim that their old identities remain current.

After deployment of the reviewed API/CLI and native schema proof, use a managed
existing operator login. The default command plans without writing:

```bash
enclii ops apps staging-intent --project janua --operation-id <OPERATION_UUID> --json
```

Review that fingerprint. To retain only the held intention, repeat the same
arguments with `--record --expect-plan <PLAN_SHA256> --reason "staging intention"`.
There is no apply, activation or materialization flag. Inspect an uncertain
outcome with:

```bash
enclii ops apps staging-intent read <OPERATION_UUID> --project janua --json
```

POST `/v1/admin/projects/janua/environments/staging/preregistration-intents`
accepts only `operation_id`, required boolean `record`, and, only for recording,
`expect_plan_sha256`/`reason`. GET on that path plus the exact UUID reads retained
history. All routes require the actual platform operator and same-project
access. CLI POST uses one request without redirects or automatic replay; both
output modes preserve the unresolved source-stage gate and withhold private
audit reasons and upstream error bodies.

## Remaining dependency order

1. Deploy and independently natively qualify the immutable held-intent store and
   closed plan/record/read interface. Its source implementation still creates no
   service or workload. The fixed PG18/TLS CI proof includes native constraints,
   concurrent fixed-slot arbitration, exact replay, immutable history and absence
   of service creation/production retargeting. A separate concurrency case proves
   rollback locks history before checking emptiness and refuses when an in-flight
   first record commits. The exclusive lock, check and conditional drop share one
   statement/transaction. A source test is not its native run receipt.
2. Define faithful environment-specific source authority without rewriting the
   global production onboarding registration. Repository ownership alone leaves
   `SOURCE_STAGE_REGISTRATION_NOT_PROVEN` unresolved, including after recording.
3. Align and qualify the complete staging-only source identity for
   `janua-api-staging`, including all selectors and consumers; preserve production.
4. Add a separately reviewed materialization operation with fresh identity,
   exact-plan CAS and explicit writer exclusions. Preview/build/deploy/domain
   consumers must not activate a held record. Neither the intent nor
   `auto_deploy=false` is a substitute for these exclusions.
5. After authorized native resources exist, independently qualify actual Argo,
   source, namespace and Deployment ownership for the `043` binding; then database
   runtime/custody/migration, network denials, recovery and staging soak. No step
   in this proposal enables production promotion or SSO.

Source tests cover operator revocation and identity drift, exact record/replay/CAS,
unknown/duplicate input rejection, closed CLI redaction in both modes and absence
of native clients or service-writer calls. The canonical
[OpenAPI contract](../api/openapi.yaml) describes the two closed routes and all
unresolved gates; TypeScript and Python reference artifacts are generated from it.
Python model round trips test the generated closed flags, modes and fields; the
server and official Go CLI additionally validate correlated modes, nonzero and
distinct IDs and actor-bound fingerprints. Mandatory native-tag cases cover
immutability/FK/uniqueness, concurrent fixed-slot arbitration, rollback versus an
in-flight first record and no service materialization. Mocked records and
native-tag compilation cannot establish those
native constraints, namespace ownership or workload readiness. Actual native CI
receipts must be checked independently before any use of the deployed adapter.
