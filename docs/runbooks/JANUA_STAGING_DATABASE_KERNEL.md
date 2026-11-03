# Janua isolated staging database kernel

Source preparation only. This increment does not activate staging and exposes no
new HTTP route, CLI mutation, credential export or migration runner. The native
identity prerequisite is the retained inert authority from migration046, outside
`services`; `activation_held` stays true and native043 binding stays false.

The `internal/januastagedb` package implements one closed operation: create a
fresh dedicated CNPG Cluster and its separately generated nonowner runtime
Secret. It never calls legacy addon provisioning or inserts a `database_addons`
row. It accepts no caller-selected SQL, image, database, owner, runtime role,
namespace, restore, backup, extension, command or credential. The generic
`*_sim` validator remains unchanged and refuses `janua_staging`.

## Execution boundaries

A plan binds the retained source/native authority, exact server catalog
allocation and price, actor, private reason hash, operation identity, namespace
and retained StorageClass UIDs, StorageClass version, and separate reviewed
network and capacity/budget hashes. Observation timestamps do not change its
stable hash. An arbitrary JSON plan or valid hash is not execution authority.

Every execution/readback requires all server-owned observers:

1. A fresh completed inert-authority observation, at most 30 seconds old, with
   actual app/Redis/Argo UIDs and held source/specs unchanged. Shape validation
   alone cannot qualify it.
2. A current active platform administrator and matching retained authority,
   Janua project/staging ownership, and exact available catalog allocation and
   price, reread from SQL. Existing active addon rows in either the environment
   or dedicated addon namespace refuse; no production database is adopted.
3. The owned separate `project-<project UUID prefix>` addon namespace, exact
   UID, signature-admission label and no broad shared-data consumer labels.
4. The bound retained StorageClass, exact UID/version and Retain reclaim policy.
5. A fresh exact network observer covering both namespaces, canonical control
   plane peers, observed CNPG operator selectors, DNS baseline and absence of
   additive broad policies, bound to `network_sha256`.
6. A fresh capacity/budget observer bound to `capacity_budget_sha256`. A catalog
   row, zero catalog price, read-only plan or StorageClass existence alone proves
   neither actual capacity nor approved infrastructure cost.
7. Verification of the fixed immutable official PG image before strict native
   dry admission. Unknown privilege/import/auth defaults refuse; explicit
   nonroot postgres UID/GID and closed bootstrap/runtime flags remain fixed.
8. A durable unique operation claim before any persistent native write.
   Missing observers, revoked authority, unknown claim outcome or source drift
   refuse. No bool in HTTP input can substitute for any observer.

The concrete fresh network and capacity/budget observers and HTTP orchestration
are **not implemented by this package**. Until those and native-authority wiring
are reviewed, the package must remain unreachable. It is a reusable typed apply
kernel, not a current operator capability or permission to bypass a missing gate.

## Native write and recovery sequence

`DryAdmit` reads no Secret and makes only strict `DryRunAll` Cluster creates.
`Apply` first does that admission, then durably claims migration048's unique
held-service slot. It creates one fixed runtime basic-auth Secret with a fresh
cryptographic password, records its UID, creates the exact Cluster, records its
UID, and CAS-binds only that same runtime Secret to that same Cluster. The final
phase is `CLUSTER_SUBMITTED_RUNTIME_UNQUALIFIED`.

The immutable journal serializes ordered object events. It rejects replay,
replacement operation IDs, rewrites, deletes, truncation and rollback after any
history. Native failure or lost create/commit responses retain a held operation;
there is no automatic retry, resource adoption, deletion, credential rotation or
cleanup. An operator must inspect the same operation and exact native UIDs before
any separately reviewed recovery adapter is allowed. No recovery mutation is
provided here.

The fixed database is `janua_staging`, owner `enclii_migrator`, runtime
`janua_staging_runtime`. Runtime has no ownership, superuser, BYPASSRLS,
role/database creation, replication or inheritance. CNPG controls its login only
after reconciling the generated runtime Secret. Public schema creation and
PUBLIC temporary-database privileges are revoked; fixed default DML/sequence
grants serve the runtime after owner migrations.

## Separate custody

Runtime custody requires the exact successful journal Cluster and runtime
Secret UIDs, closed Cluster spec, all ready replicas, reconciled runtime role,
Cluster-owned Secret metadata and fixed database/user/host/port. A copied object
with matching labels is insufficient. The public CA must be a current CA-only
PEM bundle owned by that Cluster; the Cluster-owned server certificate must
verify against it for the canonical database hostname. CA private key bytes
are never selected for export.

The connection URI is reconstructed for the fixed hostname with `verify-full`;
stored URI fields cannot redirect it. Secret identity/data and actual Cluster
readiness are rechecked before release. Runtime custody does not read the owner
Secret. Owner custody is a distinct fixed-role read usable only by a future
fixed migrator. Neither bundle has a JSON credential representation; neither is
an HTTP response. Logs and errors must never include bundle fields.

Custody qualification still does not submit secret intake, run migrations,
activate API or Redis, establish denied network-path proof, adopt a schedule,
prove real login, qualify a 30-minute soak or authorize production promotion.
The next fixed Alembic executor needs its own immutable operation journal,
reviewed image/source/head proof, verified database identity and lineage, owner
credential isolation, exact network qualification, runtime receipt and cleanup.

## Operator inspection and remaining owner actions

These existing read-only commands can report current capabilities and gates:

```bash
enclii ops capabilities --json
enclii ops apps staging-intent --project janua --operation-id "${JANUA_STAGE_OPERATION_ID:?}" --json
enclii addon staging-plan --project janua --plan standard-0 --json
enclii secrets intake targets --json
```

They do not invoke this package. Do not substitute legacy addon creation,
production service registration, production credential reuse, raw bootstrap or
manual DB grants. Once the missing typed capabilities exist, owner-approved
custody initialization and grants must use their documented Enclii commands;
there is no valid DB-apply owner command in this release. Keep runtime/image/
soak promotion gates closed until actual isolated staging evidence is retained.

## Validation

The unit suite uses synthetic server observations and fake Kubernetes transport
for strict admission, claim-before-write, concurrent/replay refusal, identity
and authority drift, partial retained outcomes, CA/DNS/owner/readback checks and
credential redaction. SQL mocks exercise real journal methods, rank/cost/source
queries and uncertain commits; they do not prove actual Kubernetes admission,
CNI enforcement, provider capacity or a database deployment. Real PostgreSQL
migration proof is recorded separately through the required native target test
suite; a source check or a test fixture is never live runtime authority.
