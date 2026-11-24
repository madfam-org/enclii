# Fixed held Janua namespace and network preparation

Boundary checkpoint (2026-11-12, platform engineering): public-safe source contract and synthetic fixtures only. Reviewer/owner: platform engineering. See the [public repository boundary](../PUBLIC_REPO_BOUNDARY.md).

This source adapter performs one bounded platform operation before the held
database and migration kernels. It creates an empty owned addon namespace and
six fixed NetworkPolicies. It does not create a Secret, database, volume,
workload, replica, role or grant, nor qualify a running isolated staging API.
The route requires an authenticated current platform operator with project
access; status also requires the original operator. Source publication does not
authorize an actual preparation call.

The exact fixed API is:

- `POST /v1/admin/projects/janua/environments/staging/network-preparation`
- `GET /v1/admin/projects/janua/environments/staging/network-preparation/<OPERATION_UUID>`

The POST accepts no namespace, image, command, SQL, endpoint, selector, catalog
price or uploaded evidence. First send exactly:

```json
{
  "operation_id": "<NEW_OPERATION_UUID>",
  "authority_id": "<COMPLETED_HELD_AUTHORITY_UUID>",
  "dry_run": true
}
```

Review every object key and desired digest in that response, the held authority
and the unresolved gates. In a separately authorized call, send the same IDs,
`dry_run: false`, `expect_plan_sha256` containing that exact reviewed plan hash,
and a bounded nonempty `reason`. There is no combined plan/apply command. Native
source or metadata changes invalidate the fingerprint and require a fresh plan.
Canonical Kubernetes API endpoint identities stay private to the server journal;
responses contain only hashes, object keys and native UID receipts.

The seven fixed objects are:

1. The server-derived `project-<PROJECT_PREFIX>` addon namespace with project,
   signature-admission and restricted Pod Security labels.
2. Its existing-profile DNS-only `default-deny` baseline.
3. Fixed CNPG database ingress/egress for the exact held API, fixed migrator,
   database peers, current operator selector and canonical Kubernetes API peers.
4. Held API egress to that database.
5. Fixed migrator egress to that database.
6. Held Redis ingress from the held API.
7. Held API egress to Redis.

The addon namespace must initially be absent. Any preexisting target object,
even one with identical labels/specification, is refused rather than adopted.
The app namespace keeps its original held identity and DNS baseline; additional
policies are refused unless their exact first UID and configuration are retained
by this operation. The addon namespace is checked for executing/latent workloads
and volumes. This is not a qualification of arbitrary Secret, ConfigMap,
ServiceAccount or RBAC custody.

## Durable history and interruption

Migration052 links a unique immutable operation slot to the completed049 held
authority. The effect session locks current actor/project/environment/repository
access and serializes by held authority. Before each native create, a separate
transaction commits the exact ordered create intent. After a successful native
response, another transaction retains the first actual UID, resource version
and observed configuration digest **before** readback or the next effect.
Admission drift therefore retains its actual UID but blocks continuation and
completion. The per-object commits do not roll back with the effect session.

An existing operation is always inspect-only. Unknown create/commit outcomes,
missing receipts, process interruption and replaced UIDs do not permit a retry,
recreation, adoption, alternate operation UUID or deletion. Inspect the same UUID
and seek a separately reviewed retirement/recovery procedure. No such procedure
is implemented in this increment. Historical rows cannot be changed, deleted or
truncated, and migration rollback refuses retained operations. A completed
operation is also never rerun; GET reobserves its exact live configuration.

`completion_recorded` is immutable history. `configuration_present` additionally
requires a fresh successful observation. Neither means packet isolation or
database readiness. `packet_isolation_proven` and `db_apply_allowed` remain
literal false, and `activation_held` remains true.

## Shortest remaining activation path

This operation closes the missing owned addon namespace and journalled fixed
policy configuration seam. The [database kernel](JANUA_STAGING_DATABASE_KERNEL.md)
can consume its actual namespace UID and a fresh configuration digest, but no
DB Apply call is wired here. The following native evidence is still required:

- Fixed allow/deny packet probes with positive controls and retained Job/Pod
  UIDs. Policy configuration or a timeout alone cannot prove CNI isolation;
  CNPG operator egress/webhook and DNS paths also need actual qualification.
- Exact scheduler allocation/retained-storage placement plus current metered
  spend and approval of that specific quoted operation cost. A project ceiling
  or zero catalog price is insufficient. No new budget system is introduced.
- Dedicated reviewed registry custody; no shared production credential fallback.
- Existing DB kernel admission/apply/readiness and verified runtime/public-CA
  custody, then the fixed initial migration and terminal owner-credential cleanup.
- A separately retained held-to-running activation, reviewed ESO/volume source
  permissions and exact replicas, followed by synthetic auth/refresh, native
  isolation and at least 1800 continuous healthy seconds of smoke/soak.

Owner-required intake and the ordered larger path remain in
[the activation runbook](JANUA_STAGING_ACTIVATION_PATH.md). This source operation
does not execute intake, grants, promotion or any production change.
