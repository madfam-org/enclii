# Witnessed portal close after reconstruction

Last Updated: 2026-10-28

Boundary checkpoint: public platform implementation and synthetic proof only.
Private deployment identities, custody and operational receipts remain in
`internal-devops`; see [PUBLIC_REPO_BOUNDARY.md](../PUBLIC_REPO_BOUNDARY.md).

`portal-sim-cierre-reconstruccion` closes a previously fenced synthetic
reservation after its kernel has been rebuilt on a new dedicated addon.
The HTTP caller supplies identifiers and a reference to server-retained
observation; the server constructs the product's exact close payload.

## Before the kernel is touched

Use [reset-fence observation](PORTAL_RESET_FENCE_READ.md). Its signed producer
must contain the exact reader, fixed schema002, fixed step004 and close entry
point. An applied/collected step004 receipt on the current portal addon, Cluster
and binding is required before observation admission.

Enclii resolves the original kernel's owned Ready addon, active runtime binding,
Deployment/Service/Cluster UIDs and signed image while they still exist. It
retains that origin witness with the observation's immutable intent. The runtime
reader then verifies the reservation's retained reset attempt/fence/generation
under the kernel lock. The collected native receipt carries
`reconstruction_origin_sha256`, binding the observation to that server witness.

Only then may a separately authorized reconstruction change the kernel.
An older observation lacking this witness remains readable but cannot be used
for closing; the server never infers the old addon from the current binding.

## Closed request and reviewed plan

The existing platform-operator routes serve the profile:

- `POST /v1/admin/projects/:slug/addons/:id/initial-bootstrap`
- `GET /v1/admin/projects/:slug/addons/:id/initial-bootstrap/:job_name`

The POST fields are `profile`, `service_id`, `nucleo`, `fence_read_job`, explicit
`dry_run`, and `idempotency_key`. `nucleo` is `01`, `02` or `03`;
`fence_read_job` is the retained `read-<24 lowercase hex>` operation collected
before reconstruction. No reservation ID, original addon, fence hash, seed,
image, namespace, SQL, command or owner credential is caller-supplied.

1. Dry-run with the intended operation key. The server returns the complete
   fixed-Job admission result and `plan_sha256` after checking the evidence below.
   It persists no Job or database write.
2. Apply with the same identifiers/key, `dry_run: false`, a reason and
   `expect_plan_sha256` equal to the reviewed hash. Changed authority is refused.
3. Poll and collect using the identical apply body/key. A separate key never
   adopts or rewrites the first operation's history.

Only this profile accepts reconstruction fields; even empty/null reconstruction
fields are refused by the other initial/fixed profiles.

## Server evidence chain

Admission and pre-activation revalidation require:

- The current portal's exact source/signature/CI/pin, Ready workload, binding,
  namespace identities and server-resolved owner/runtime/public-CA custody.
- The collected fence read belongs to this portal addon/service/Cluster/binding
  and the requested kernel, with its immutable original-kernel witness.
- The original addon remains a historical same-project kernel addon and has no
  nondeleted bindings; a suspended binding still counts and blocks closing.
- The successor is a different dedicated addon and Cluster on a different active
  binding of the same kernel Service/Deployment UIDs. Its creation and fixed
  initial seed receipt both postdate the pre-cut witness.
- The exact retained `INITIAL_ONLY` receipt for that successor's owned initial
  profile matches the live image, Cluster, binding, Service and Deployment. It
  accredits the product's seven-role seed and a recognized fixture cohort.
- A collected `APLICADO`/`YA_APLICADO` step004 receipt names the same portal
  addon/Cluster/binding and exact fixed002 schema hash. Partial/failed evidence
  listings fail closed.

From those records Enclii builds `crea-sim-cierre-reconstruccion-v1`, with a
stable close UUID derived from the operation key, original reservation/fence,
original/new addon IDs, native binding/seed evidence hashes and current portal
destination. Recursively sorted JSON bytes match the producer contract; a
cross-language synthetic golden vector guards its payload hash.

## Fixed execution and collection

The Job mounts only the server-built immutable request and owned custody. Its
only command is the signed image's fixed `cerrar-por-reconstruccion.ts` entry
point, with stdin from that mount. It performs no migration, seeding, reset or
scale operation. The product revalidates its exact native catalogue and retained
reservation/fence under its custody/kernel locks, then atomically appends its
immutable close and closes that reservation. An identical payload is
`YA_CERRADO`; another payload for the reservation/fence is refused.

Collection requires the accredited native Pod/image/command/exit and exact typed
output. `cierreId`, `reservaId`, canonical payload hash, schema and SQL database
identity must match the server-built plan and its step004 evidence. A malformed
result is held; an integrity/correlation refusal does not become success.
Completed identical-key replay reads the original retained receipt before
resolving present-day credentials, so rotation or a later turn cannot start a
second execution or rewrite the old result.

`CERRADO` proves the historical append only. The gateway independently verifies
the successor's empty state, revision/epoch0 and matching seed before permitting
a new reservation. This source adds no fleet-policy activation, permanent
workload grant, CronJob or autonomous recovery of uncertain/orphaned turns.
Runtime rollout, schema application and native acceptance are separate evidence.
