# Karafiel staging database and fixed schema migration

This is the source contract for the native staging adapter. A passing source test, dry plan, completed schema Job, and full staging acceptance are separate evidence. Publication of this adapter does not provision a database, enroll a client, adopt an Argo Application, enable a financial feature, or establish Mexican legal compliance.

## Fixed identities and custody

The held native service registration is `karafiel-api-staging`; the physical Deployment and Service are `karafiel-api` in `enclii-karafiel-staging`. It must use the persisted Karafiel staging environment, native deployment-target binding, `auto_deploy=false`, and `release/karafiel-staging`. No production service row is retargeted. The historical registration flag `namespace_ownership_pending=true` is metadata about the original observation; fresh actual Namespace ownership is proved separately.

The existing native PostgreSQL addon machinery supplies one dedicated `karafiel-staging-db` addon in `project-<project UUID prefix>`, database `karafiel_staging_sim`, nonowner runtime role `karafiel_staging_runtime`, and separate owner/migrator `enclii_migrator`. The generic `*_sim` validation remains in force. The closed profile uses the reviewed immutable PostgreSQL image, retained StorageClass, native runtime-role bootstrap, CNPG-owned credential/CA checks, and no restore, cloned source, backup provider, extra membership, or runtime owner credential.

The stage projection Secret is `karafiel-staging-database-runtime`. Its only keys are `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`, and public `ca.crt`. The dynamic owned native host is projected from verified custody; there is no placeholder host or production bridge. Backend containers reference the five runtime fields explicitly, mount only the public CA at `/var/run/karafiel-staging-db/ca.crt`, and use libpq `verify-full`. The two separate staging application keys come from the exact ESO `karafiel-staging-core-source` mapping. Fresh generation/origin of those keys and dedicated Janua client custody still require the separate platform prerequisite proof; matching a Secret name is insufficient.

## First quarantine adoption under the restricted AppProject

The legacy stage Application tracked its Namespace and a PolicyException. Argo checks prune-task permissions even when `Prune=false`; removing those kinds from the restricted AppProject therefore requires a separately reviewed platform handoff before the first quarantine sync.

The human-only `POST /v1/admin/projects/karafiel/environments/staging/quarantine-adoption` accepts only `operation_id`, `dry_run` (default true), `expect_plan_sha256`, and a reviewed `reason`. A reason is required for the dry plan too. It reads the persisted project/environment and actual held Application, exact fixed source, restricted AppProject, strict root/child tracking and Namespace UID/version without requiring a Service enrollment or database. Missing actual Namespace `Prune=false`, foreign ownership/tracking, extra PolicyExceptions or unexpected tracked resources refuse the operation.

Review the returned `plan.plan_sha256` and repeat the same canonical operation/reason with `dry_run=false` and `expect_plan_sha256`. The only non-audit writes are a Namespace UID/version CAS that removes the exact Argo tracking markers while preserving all other labels/annotations/specification, and a UID/version-precondition delete of `kyverno.io/v2beta1` PolicyException `karafiel-staging-gitops-rollout-signature` in that exact namespace. It never creates a waiver or deletes a Namespace, hook Job, Deployment, Secret or database.

An immutable single-operation claim, reviewed intent, per-mutation attempt records and bounded receipt are retained as Namespace-owned ConfigMaps. An uncertain update/delete is not reissued; the same key may finish only from independently observed matching poststate. A new key cannot bypass an unresolved operation. A crash between the attempt record and its write remains an explicit operator hold. `GET /.../quarantine-adoption/<operation_id>` reads the retained receipt with fresh state qualification and performs no write. Receipt hashes exclude resourceVersion/managed-field/GC timestamps from the expected metadata poststate while retaining all other Namespace labels, annotations, owner/finalizer and spec fields.

The read-only native observation separately proves `argo_tracking_detached` and exact legacy-exception absence. Native quarantine also requires the complete stage PolicyException list to be empty. This metadata handoff establishes no all-zero workload, network, database, provider, financial or fiscal acceptance; those gates follow through the normal quarantine protocol.

## Human preparation: dry plan, review, bound apply, readback

Only an actual platform operator with Karafiel project access may use the admin routes:

- `GET /v1/admin/projects/karafiel/environments/staging/database/quarantine?service_id=<UUID>`
- `POST /v1/admin/projects/karafiel/environments/staging/database`
- `GET /v1/admin/projects/karafiel/environments/staging/database?service_id=<UUID>&addon_id=<UUID>`
- `POST /v1/admin/projects/karafiel/environments/staging/database/migrate`
- `GET /v1/admin/projects/karafiel/environments/staging/database/migrate/<job>?service_id=<UUID>&addon_id=<UUID>`

The preparation body accepts only `service_id`, catalog `plan`, canonical UUID `idempotency_key`, `dry_run`, `expected_plan`, and `reason`. Dry run is the default. Review the returned phase and `plan_sha256` before applying; apply must repeat the same operation and bind `expected_plan` to that fresh fingerprint. The authenticated actor, exact plan, phase, service/addon IDs, and reason are retained in a Namespace-owned immutable intent. Use a distinct reviewed operation for each later phase.

The server selects a phase from actual evidence:

1. `PREPARE_OWNED_NAMESPACE`: revalidate the same registered Namespace UID, held source, exact effective stage policy set, and all-zero execution state; CAS only the absent/known legacy ownership label to the actual project UUID, enforce image-signature labeling, and set the three known shared-data consumer labels to false. The all-zero metadata plan may observe known legacy true values solely as `OWNERSHIP_ADOPTION_REQUIRED`; serving and every READY proof require actual false values. The returned `namespace_label_targets` makes these five exact targets reviewable. All other labels are preserved. A new native addon namespace receives a deny baseline with scoped DNS. Existing shared addon namespaces are never rewritten by this bootstrap.
2. `PROVISION_DATABASE`: qualify the persisted catalog plan, actual Kubernetes API endpoints, CNPG operator selector and retained StorageClass UID/version; verify the fixed PostgreSQL producer; perform strict server-side dry admission of Cluster, exact scoped policies, and a credential-free representative image Pod. Revalidate the authority before each create. A new native addon identity is reserved before any Cluster create; unknown outcomes remain held.
3. `WAIT_FOR_NATIVE_READY`: no apply is accepted. Wait for actual owned CNPG/runtime-role/CA readiness.
4. `BIND_RUNTIME_CUSTODY`: qualify actual Cluster identity and the original immutable provisioning intent, create the existing native binding, and project only verified runtime custody. Read back through the database endpoint. A matching familiar database name cannot adopt a foreign Cluster or Secret.

A changed fingerprint requires a new dry plan/review. An uncertain create, partial namespace baseline, conflicting object, failed migration, or missing receipt is a hold for bounded operator investigation, never permission to reset, recreate, erase, adopt, or retry with a fresh key to bypass the guard.

## Quarantine and effective policy admission

Actual staging must use the root-tracked `karafiel-staging` Application, the exact fixed release ref/path/repository/destination, held automation, effective replica enforcement, and the restricted actual `karafiel-staging` AppProject. The shared AppProject reader also refuses inherited global permissions. Actual namespace UUID/signature labels and immutable registration UIDs are required for readiness.

All five app Deployments are zero before migration; worker and beat remain zero throughout this protocol. Pending/unknown/nonterminal Pods, executable Jobs, CronJobs, StatefulSets, DaemonSets, and unexpected HPAs refuse quarantine. Historical terminal hook Pod objects do not establish fresh acceptance and do not alone imply executable work. Every one of the seven reviewed stage NetworkPolicy specifications is compared exactly. A native binding policy is accepted only after its own exact spec/UID/ownership readback, and participates in the observation hash. The `native_binding_policies` array is empty before first provisioning and otherwise contains only this server-qualified binding identity, actual Cluster/Namespace UIDs and exact policy. Its spec is included with the seven Git policies in the full effective policy fingerprint, including on later quarantine runs. Other broad policies must actually be pruned before acceptance.

The dedicated CNPG policies select the exact new cluster and verified project namespace, not every pod in a potentially shared addon namespace. They admit only the exact stage runtime, fixed migrator, CNPG operator/control-plane peers, scoped DNS and bounded Switchyard metadata probe. Foreign additive policies that could broaden this cluster's access refuse readiness; this adapter never repairs another addon's policy set. Backend actual env/secret references, TLS/CA mount and the entire inert sandbox ConfigMap are checked against reviewed public descriptors. The stage audience is `karafiel-staging-api` with no inherited production additional audience.

Policy fingerprints use SHA-256 over compact UTF-8 JSON with recursively sorted object keys, array order preserved and no trailing newline. The public `policy_set_sha256` hashes the name-sorted `[{name,spec}]` projection. Private control-plane addresses and Secret values are not returned.

## Build, fixed migration, serving, and reuse

The release pipeline first completes `.github/workflows/release-build-candidate.yml` successfully at attempt 1 on exact main source S. The native producer verifier binds the API digest to its OCI source/revision and `io.karafiel.candidate-run`, `candidate-<run>` tag, signed `source_sha`/`candidate_run_id` annotations and fixed main workflow identity. Normal `ci.yml` must itself be a completed successful first attempt with the exact five required jobs. The reviewed staging guards in the historical identity seed migrations must be present byte for byte at S. A still-running stage acceptance workflow is not used as the completed build proof.

The stage release ref advances first to the reviewed PREPARE projection (exact S and all three immutable image pins, canonical zero-app render and bound receipt). Actual all-zero/quarantine readback precedes the single native migration profile `karafiel-staging-migrate`. There is no normal stage Argo migration hook and no duplicate migrator.

The narrow release API uses operation-specific GitHub OIDC authorization, not a global platform-admin CI token. It may read bounded proof and invoke this fixed migrator only through its reviewed root-owned route. The native kernel compares the selected producer source to that typed release principal before any Job mutation and again during revalidation. Human provisioning remains outside that principal.

The fixed native Job reuses the existing suspended-Job/intent/admission/receipt kernel, backoff 0, bounded deadline and UID/image/custody checks. Only this Python image profile uses UID/GID/fsGroup 1001. It receives only its scoped migrator credential, owned public CA and the same staging core two keys. All four financial/fiscal feature flags are false; broker and file settings are inert; no arbitrary command, SQL, namespace, image, owner export, reset or provider credential is accepted. After bounded network establishment it performs only the reviewed Django schema graph under `ENVIRONMENT=staging`. Output is one closed typed schema receipt; application/provider output is withheld.

Fresh initialization requires zero user tables. The native provenance row and immutable successful receipt retain that lineage. Later candidates may reuse the same database only with the same Cluster UID, CA, binding and accepted prior native schema receipt plus fresh matching bounded catalog descriptors. The prior receipt need not match the new PREPARE image; the new successful receipt must match it. No database is reset to make a later candidate look empty.

Database readback returns `enclii-karafiel-staging-database-v1` with phase `PRE_BUILD_DATABASE_READY` for a fresh empty database or `CANDIDATE_MIGRATION_READY` for an accepted initialized cohort. Counts and hashes describe PostgreSQL metadata, not an audit of tenant rows or original financial evidence. Runtime nonownership, absence of elevated flags/memberships/schema-create privileges, actual CA/runtime projection, and protected schema lineage are required.

The migration envelope is `{job, producer, scope, runtime}`; readiness `migration` is the bare native Job receipt. A retained `KARAFIEL_SCHEMA_APPLIED` receipt binds source/image/build/DB identities, graph/catalog hashes, and actual validated Pod name/UID, Job UID, image ID, zero exit/restarts and start/finish times. GET status never reads stdout or writes receipts. Repeat POST with the same operation UUID to collect the completed Job; it does not rerun schema work. Missing or ambiguous actual runtime evidence cannot become accepted output.

Only after native schema acceptance may the exact SERVE projection start API/web/admin, leaving worker/beat zero. The native readback accepts that coherent serving topology without pretending it remains at zero. The later fresh 30-minute same-cohort smoke/production promotion is a separate release gate; no executor soak, production readiness, allocation activation or legal-compliance conclusion follows from this database receipt.

## Validation and remaining runtime gates

Focused Go tests cover closed configuration/credentials, source/CI bindings, exact policies, runtime injection refusals, actual client-go synthetic Pod/receipt capture and read-only retained history. The existing `TestRuntimeBootstrapOnNativePostgres` PG16 CI proof now executes this exact catalog SQL on a fresh owned synthetic database, verifies initialized schema/hash behavior, owner/runtime hash parity, DML versus DDL/owner restrictions, and ownership/privilege drift refusals. It remains skipped locally without the explicitly owned disposable loopback fixture URL; a skipped local SQL proof is not a PostgreSQL acceptance result.

Full runtime acceptance still requires the normal CI receipts, reviewed platform deployment, actual root/ref/AppProject adoption and writer exclusion, effective policy prune/readback, actual dedicated native DB/CA/role and fresh core-key/Janua custody, fixed signed candidate migration, serving observations, and the normal soak/promotion receipts. No manual boolean or source artifact substitutes for those observations. Original bank/taxpayer archives and financial/fiscal execution are outside this protocol.

## Boundary checkpoint

2026-10-25 — Platform engineering. This runbook describes public platform
contracts and source gates. Live custody, identifiers, topology and rollout
evidence remain in private `internal-devops`. See the
[public repository boundary](../PUBLIC_REPO_BOUNDARY.md).
