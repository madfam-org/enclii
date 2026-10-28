# Portal reset fence observation

Last Updated: 2026-10-28

Boundary checkpoint: public platform contract only. Deployment identities,
operator receipts and private operational evidence belong in `internal-devops`.
See [PUBLIC_REPO_BOUNDARY.md](../PUBLIC_REPO_BOUNDARY.md).

`portal-sim-reset-fence-read` runs a fixed, runtime-only observation of one
synthetic kernel's retained reservation, reset attempt and fence. It extends
the same custody and immutable receipt machinery as
`portal-sim-generation-read` without adding database writes, owner credentials,
reset authority or a generic command surface.

## Closed request

Platform operators use:

- `POST /v1/admin/projects/:slug/addons/:id/retained-reset-fence-read`
- `GET /v1/admin/projects/:slug/addons/:id/retained-reset-fence-read/:job_name`

The POST accepts `profile`, `service_id`, `pedido`, `dry_run`,
`idempotency_key`, and, on apply, `expect_plan_sha256` plus `reason`.
`service_id` and `idempotency_key` are canonical nonzero UUIDs. `pedido` has
exactly two fields:

```json
{
  "version": "crea-sim-fence-kernel-read-v1",
  "nucleoId": "crea-sim-map01"
}
```

The three registered kernel IDs are the only admitted values. No reservation
ID, fence hash, identity, SQL, image, command, credential or namespace is supplied
by the caller. Duplicate and unknown JSON fields are rejected.

1. Dry-run with explicit `dry_run: true`. It verifies owned runtime custody,
   producer source/signature/CI/pin, exact binding, converged gateway, applied004
   and original kernel authority. It
   performs Kubernetes admission checks with `dryRun=All`; it runs no database
   query and persists no Job or credential. Inspect `plan_sha256`.
2. Apply with `dry_run: false`, that `expect_plan_sha256`, the same operation
   key and a reason. The native observation Job receives only the runtime URI,
   verified public CA, database namespace and immutable two-field input. The
   fixed signed reader executes a `READ ONLY` transaction, takes the kernel's
   advisory lock, resolves its unique nonclosed reservation and checks the
   retained attempt/fence/generation chain. Zero, ambiguous, closed or
   unaccredited results fail visibly.
3. Poll and collect using the same operation key. The typed twelve-field
   result is retained as `job.native_receipt.fence_read`. An identical replay
   reads that operation; a changed kernel under the same key is refused.

Producer source admission pins `leer-freno-reset.ts` byte for byte, including
its new kernel-lookup DTO. An image lacking that source is refused before a
Job is admitted. It must be released by the producer before this operation
can run successfully; source tests do not establish deployed availability.

## Scope of the receipt

`PROVENANCE_ONLY` records a snapshot of retained synthetic metadata. Identity
values are hashes; raw user/member identities are not emitted. The result
contains the reservation and attempt IDs, kernel, role, fenced revision/epoch,
fence hash and verified-state hash. Malformed output becomes
`PROVENANCE_ONLY_HELD` without returning raw logs. A failed native Job is held
without reading its stdout.

The separate [witnessed close profile](PORTAL_RECONSTRUCTION_CLOSE.md) consumes
this immutable observation after reconstruction. New observation admissions
require the collected fixed step004 on the current portal database and retain
the original kernel addon/binding/UID witness server-side. The native receipt
adds its `reconstruction_origin_sha256`; no raw credential or identity is added
to the product reader's twelve fields. Legacy observations without that witness
remain observation-only. A historical read alone authorizes no scaling, closing,
release or reservation admission. Fleet-policy and runtime acceptance remain
separate from these source contracts.
