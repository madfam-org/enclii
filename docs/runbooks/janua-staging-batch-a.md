# Janua staging BatchA: source review checkpoint

Status: **source only; ApplySupported is false and the new prepare route is not registered**. This document does not authorize any staging or production mutation.

BatchA supplies a closed prerequisite adapter for `janua-api-staging-v1`. Existing generic addon creation and credential binding retain their synthetic `_sim` constraint. The reserved `janua-staging-db` name cannot enter generic provisioning, backup convergence or network convergence, even if its configuration drifts.

The proposed phases are:

1. `reserve`: atomically reserve a new `janua-api-staging` service with `AutoDeploy=false`, its exact `janua-staging` namespace, a catalog-derived PostgreSQL addon and the existing append-only addon event. The transaction locks the persisted Janua project, staging environment, completed repository registration and catalog row. Existing staging rows are conflicts, never adoption candidates.
2. `namespaces`: create fresh application and addon namespaces with signature enforcement, the exact project UUID, all three actual shared-data consumer labels false, and a default-deny policy allowing only DNS to the cluster resolver. No serving or existing namespace is relabeled. Both server-assigned namespace UIDs are retained in an immutable control-namespace receipt.
3. `provision`: require those exact live namespace UIDs, retained storage, current native API endpoint identities and operator selectors; strictly dry-admit the fixed Cluster and policies; then create exact policies, a fresh immutable runtime Secret and the fixed Cluster. Bind the exact Secret create result to the returned Cluster UID using resourceVersion CAS and retain the resulting Cluster, Secret and policy UIDs. Existing objects or uncertain writes hold the operation without rollback, adoption or blind retry.
4. `custody`: require the retained birth receipt, current persisted ownership, effective isolation, CNPG Ready/runtime-role reconciliation and an owned, valid public server CA. Reconstruct only the nonowner runtime URL using `sslmode=verify-full` and the overlay's fixed public CA file. Initialize `database_url` and `database_ca` together at the existing fixed intake path using the native Vault initializer, preserving existing keys atomically. Retain only the public CA hash, native IDs and Vault version in the custody receipt.

The runtime role has no superuser, BYPASSRLS, role creation, database creation, replication, inheritance or schema creation authority. BatchA grants no default table CRUD privileges. It creates no tables, users, organizations, OAuth clients, hosted auth flows or mail transports. Owner credentials remain inside CNPG; they are not returned, projected into the application or written to runtime Vault custody.

Each phase has one immutable execution claim in the control namespace, bound to the operation, actor, project, environment, repository registration, service, addon and catalog fingerprint. Exact replays inspect metadata only. A lost response never supplies permission to repeat a side effect. Control receipts have no disposable namespace owner reference, so deleting/recreating staging cannot erase their lineage.

The server re-resolves the platform rank and persisted ownership before effects. Caller-supplied namespace, image, SQL, role, credential and destination overrides are rejected. Neither an old staging intent, preview hash, matching label nor an operator-supplied acknowledgement is authorization. Native endpoint, operator, namespace and storage identities are rechecked around effects. The adapter does not write `argocd-cm`.

## Local source proof

The bounded one-thread proof passed the addon, namespace and repository unit packages. The positive custody case verifies exact native birth, managed-role readiness, public CA and verify-full while the public generic parser still refuses non-synthetic Janua configuration. The integration-tagged PostgreSQL proof passed against all current migrations in a fresh disposable database; it verifies current actor authority inside the transaction, held metadata, original reasons, duplicate refusal and zero Ready events. Full API compilation/runtime and ordinary CI remain required. No native staging or production operation was performed.

## Required review and proof before activation

- Review the source adapter, transaction, one-execution claims, unknown-outcome behavior and custody boundary. Keep the new route unregistered and ApplySupported false until that review completes.
- Run the focused Go tests for Janua, generic runtime provisioning, namespace ownership and the existing Karafiel staging profile. The fake clients exercise dry-run boundaries, mutated admission, partial failures, exact replay, changed identity, no adoption, atomic metadata rollback and secret-free serialization. They do not prove live Kubernetes, CNPG, CNI or Vault behavior.
- Prove the new reservation query against an isolated PostgreSQL fixture with the current control-plane schema. Require all ordinary CI gates.
- Before any actual prepare, establish the persisted Janua environment/repository binding and inspect exact namespace absence. An existing or partially created namespace needs a separate identity review; this profile does not adopt or repair it.
- Verify the source Janua staging Deployment, Service and selectors all use `janua-api-staging`. Repository/container image names may remain `janua-api`. Production is never retargeted.

BatchB remains required for the fixed signed-image migration, reviewed per-table runtime grants (including append-only audit/policy tables), actual runtime custody/readiness and isolation evidence, and the required healthy staging soak before production promotion. BatchA has no migration or promotion bypass and provides no auth or email activation.
