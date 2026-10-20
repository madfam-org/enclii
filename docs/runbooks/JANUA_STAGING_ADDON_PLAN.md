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
