# Read-only Janua staging addon plan

Last Updated: 2026-10-20

> Boundary checkpoint (2026-10-20, platform preparation): public-safe logical
> names, role names, API contracts and namespace patterns only. Actual object
> IDs, operational receipts, topology and custody remain in `internal-devops`.
> Policy: [Public repository boundary](../PUBLIC_REPO_BOUNDARY.md).

This preview closes the read-only planning step. It does not provision a
database, establish custody, bind a service, run a migration or activate SSO.
Every result has `preview_only: true`, `apply_supported: false`, and
`classification: READ_ONLY_BLOCKED_STAGING_ADDON_PLAN`. Its observation hash
cannot be used as an apply token. No create/apply route is added.

## Operator inspection

After the reviewed API and CLI release is deployed, use an existing platform
operator login:

```bash
enclii addon staging-plan --project janua --plan standard-0 --json
```

This performs one GET:
`/v1/admin/projects/janua/environments/staging/addon-plan?plan=standard-0`.
An optional `--service-id` must name a separately persisted staging API service.
Omitting it reports `EXACT_STAGING_API_SERVICE_REGISTRATION_NOT_PROVEN`; no
production service or identifier is selected automatically. The same project
access and unconditional server-resolved platform operator guard apply, including
when staged tenant enforcement is disabled.

The endpoint accepts only a catalog plan code and optional `service_id`. It
rejects duplicate/unknown query keys and request bodies. It accepts no SQL,
image, namespace, database/role selector, credential, create or apply flag.

## Ownership and metadata read proof

The persisted project must be `janua`. Its persisted `staging` environment must
belong to that project and name exactly `janua-staging`. The completed
`madfam-org/janua` onboarding registration must match the same project. A supplied
service must independently match that project, its persisted staging namespace,
the exact Janua source URL, the `apps/api` path and web service type. A production
namespace refuses before infrastructure reads. A friendly service name, an
environment label or a `ps` summary cannot establish staging identity.

The preview reads the plan catalog and every nondeleted addon row for this
project. `addon_rows_read`, `stage_addon_candidates` and
`shared_addon_rows_excluded` distinguish a read of shared production inventory
from a dedicated staging candidate. A candidate is never adopted or exported;
it becomes a separate identity-review gate. Hostnames and Secret references from
those rows do not appear in the response.

Infrastructure reads use GET/LIST only, and never read Secrets, pods or logs:

- The exact app namespace and the planned `project-<project UUID prefix>` addon
  namespace are read separately. Existing namespaces require an actual UID and
  matching project ownership. Missing objects stay `ABSENT`; no namespace is
  created, relabeled or repaired.
- Each existing namespace's centrally owned DNS-only `default-deny` policy is
  checked against the exact baseline spec. Foreign ownership refuses; missing
  or drifted rules stay unresolved. Other policies are counted and require
  separate review because network policies are additive.
- Legacy `enclii.dev/data-access=true` or
  `enclii.dev/postgres-consumer=true` labels are reported as isolation-review
  gates. Their absence does not prove denied access; their presence does not
  authorize relabeling. The preview changes neither labels nor policies.
- The planned retained StorageClass must exist with an actual UID and Retain
  reclaim policy. This proves only that metadata, never storage capacity,
  admission, placement, successful backups or recovery.

Namespace and baseline object UIDs/resource versions, catalog allocation and
persisted ownership IDs contribute to the fingerprint. `observed_at` is a live
UTC timestamp excluded from the hash. A successful HTTP response establishes an
observed plan, not a ready environment or permission to execute it.

## Closed proposed database and custody contract

| Field | Proposed contract |
| --- | --- |
| Profile | `janua-api-staging-v1` |
| Addon | `janua-staging-db`, fresh dedicated PostgreSQL |
| Database | `janua_staging` |
| Runtime | `janua_staging_runtime`; nonowner, no superuser, BYPASSRLS, role/database creation, replication or inheritance |
| Migrator | `enclii_migrator`; exact Cluster-owned owner custody, server-only fixed migration Job |
| Transport | `verify-full`, exact bound database host and verified Cluster-owned public CA; never a private CA key |
| Runtime intake | `janua-staging/database` only; no owner credential |
| Public CA intake | `janua-staging/database-ca` only |

The preview contains no credential bytes and reads no credential object. It
therefore reports actual dedicated nonowner/public-CA/owner custody as
`NOT_EXERCISED`; source requirements cannot substitute for those receipts.

The existing runtime validator remains synthetic-only (`*_sim`) and refuses the
proposed staging database. This is an explicit prerequisite for a separately
reviewed typed staging provisioning adapter, not a reason to rename staging
silently or weaken the global guard. The legacy addon create command remains a
mutation and is not a substitute for reviewed staging apply.

Before any create, the next adapter must enforce durable operation identity,
the reviewed current plan and actual capacity, exact environment/service and
namespace ownership, dedicated addon identity, nonowner/public-CA/owner custody,
network/image admission, and no credential transport. A catalog price of zero
does not prove provider costs are zero. Existing shared production databases
must never be reused. Fixed staging migration, actual Ready runtime, healthy
soak and canonical production promotion remain separate gates.

See [Isolated staging custody](ISOLATED_TRAINING_CUSTODY.md) and
[Secret intake](SECRET_INTAKE.md).

## Typed server-side dry admission

The separate `staging-admission` adapter closes the source path for a native
admission observation. It adds no apply path and does not invoke legacy addon
`Provision`. The generic `*_sim` runtime validator remains unchanged. Run only
after the released adapter and actual prerequisites have been independently
verified through Enclii:

```bash
enclii addon staging-admission --project janua --plan standard-0 --service-id <STAGING_API_SERVICE_UUID> --json
```

The operator POST is
`/v1/admin/projects/janua/environments/staging/addon-admission`, with required
`plan` and `service_id` query keys only. The existing same-project and unconditional
platform-operator checks apply. Missing or foreign persisted stage API registration
refuses before native metadata or signature verification. Request bodies,
duplicate/unknown keys, caller image/SQL/roles/namespaces/credentials and apply
selectors are rejected.

The closed profile requires both actual owned namespaces and their current
UID/resource versions, signature-admission labels, exactly one owned DNS-only
baseline and no unknown additive policies or broad legacy data-access labels.
The retained StorageClass must have its UID/resource version and Retain policy.
Existing staging addon rows, any addon row in the proposed addon namespace, or
any actual Cluster there refuse; no production/shared database is selected,
copied or adopted. Missing isolation is a prerequisite for separate reviewed
preparation, not authorization to relabel or repair namespaces.

Control-plane destinations come only from the fixed native `default/kubernetes`
Service and Endpoints metadata. Selectorless canonical Service identity, current
UID/resource versions, numeric TCP port mapping, complete ready endpoint subsets
and bounded valid unicast addresses are required. The candidate permits exact
current destination/port pairs as `/32` or `/128` routes; it contains no broad
private range or destination-free API egress. Raw addresses are never returned,
logged or committed. The actual observed CNPG operator namespace and single
available Deployment selector constrain candidate port-8000 ingress. Stage DB
ingress and API egress use the persisted service's canonical `enclii.dev/service`
label plus the exact owned namespace. This is metadata and candidate-policy proof;
actual CNI translation, denied paths and runtime connectivity remain unexercised.

The source-reviewed PG18 descriptor is pinned to the major-18 entry in the
[immutable official catalog](https://github.com/cloudnative-pg/artifacts/blob/61f84bd4493da4e92fce41b79093246ad266a25d/image-catalogs/catalog-standard-trixie.yaml).
This adapter separately verifies that exact digest with the existing API-process
native cosign mechanism, a fixed official GitHub workflow identity/repository and
GitHub Actions issuer. It uses no shell, cluster exec, verifier Job, new RBAC,
registry credential read/copy or workload. Verification has a private temporary
writable cache, empty isolated Docker config, a minimal environment excluding
ambient registry/cloud/API credentials and proxies, bounded output and generic errors. Missing
cosign or unavailable/refused verification stops before admission. The existing
API image bundles `/usr/local/bin/cosign`; a source pin alone is never a live
signature/admission receipt. The official image producer can sign from PR workflows;
this verification does not claim an unverified main-ref SHA.

Every native create request hardcodes `DryRunAll` and strict field validation:

- A fixed `pg-janua-staging-db` Cluster in the owned addon namespace, using
  catalog allocation, retained storage, the immutable image and separate fixed
  `enclii_migrator` owner and nonowner `janua_staging_runtime` flags. Fixed bootstrap
  SQL contains no password; the managed runtime role names only its future Secret.
- Two narrow candidate network policies for that Cluster and the exact staging
  API. No apply, patch, deletion, policy expansion or existing resource adoption
  occurs.
- A representative nonroot Pod with only `postgres --version`, dropped
  capabilities, read-only root filesystem, explicit resources, no token mount,
  credential, environment, volume, SQL or network action. It is never scheduled,
  executed or substituted for the real CNPG-generated Pod admission/soak.

The adapter checks returned identity and preserved critical candidate fields,
then re-reads persisted ownership, namespace/baseline/storage, canonical endpoints
and operator resource versions. Any refusal, partial receipt or changed authority
discards the observation. Returned/defaulted Kubernetes objects are never exported
or reused for apply. A successful receipt is `ADMITTED_DRY_RUN_ONLY`, records four
dry-admitted objects and an observation hash, and always says
`apply_supported: false`. It creates no database/addon row, Secret, Pod, policy,
namespace or credential and performs no migration or SSO activation.

Actual capacity/scheduling, CNPG runtime/role readiness, live CNI allow/deny proof,
verified Cluster-owned public CA and owner/runtime custody, a fixed Janua migration
executor, recovery and staging soak remain required. The official standard image
also requires separately qualified backup-plugin integration; do not copy legacy
in-core backup settings from the shared production Cluster. See the
[official image documentation](https://github.com/cloudnative-pg/postgres-containers/blob/main/README.md).
A future durable typed apply adapter must independently enforce these gates,
review all server-defaulted fields and bind fresh authority and operation identity;
neither this receipt nor the earlier plan hash authorizes apply.

Local fake-native tests cover exact dry-run options, refusal before mutation,
upstream failure stop, no Secret reads or tracker persistence, authority drift,
producer identity/digest and bounded CLI receipt handling. They do not establish
deployed image verification, live namespace policy, PostgreSQL18 runtime/RLS,
custody, backup, migration or production readiness.
