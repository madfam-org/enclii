# Retained synthetic-generation observation

Last Updated: 2026-10-20

The platform-operator API can run a fixed, independent, runtime-only provenance
reader for an existing synthetic exercise. It does not stop the gateway, reset or
reseed a kernel, renew an exercise lease, change a database catalogue or activate
SSO. The profile is `portal-sim-generation-read`; it is not an initial-bootstrap
or migration profile. The initial-bootstrap POST explicitly rejects it.

The API refuses until the deployed gateway is Ready, available and fully
converged on its verified signed image. The image's exact source revision must
pass the portal checks and carry the byte-exact reviewed reader and custody SQL.
The same-project dedicated addon, active runtime binding, native CNPG readiness,
namespace ownership and public-CA custody must independently verify. Cached
addon readiness alone is insufficient. There is no owner credential in this path.

## Managed CLI

Use an existing managed Enclii operator profile. No token, cookie or private HTTP
request needs to be copied between machines. The command defaults to admission
dry run and requires an explicit operation UUID before planning:

```bash
enclii ops jobs generation-read --project crea-sim \
  --addon <PORTAL_ADDON_UUID> --service <GATEWAY_SERVICE_UUID> \
  --reservation <EXERCISE_UUID> --kernel <OWNED_KERNEL_ID> \
  --idempotency-key <OPERATION_UUID> --json
```

After reviewing the returned Job name and fingerprint, repeat those same flags
with `--apply --expect-plan <PLAN_SHA256> --reason "provenance observation"`.
The CLI sends one request with redirect replay disabled. An unconfirmed response
requires inspecting that same operation; the CLI never retries or picks a new
key. Read status without submitting or collecting work:

```bash
enclii ops jobs generation-read status <READ_JOB_NAME> \
  --project crea-sim --addon <PORTAL_ADDON_UUID> --json
```

There are no command, environment, SQL, namespace or image selection flags.
Responses are projected to approved metadata; raw proxy errors are not printed.
Apply confirms the returned plan fingerprint and any completed proof's requested
exercise/kernel before printing a result. Status confirms the requested Job name;
these response checks do not turn historical hashes into adoption authority.

## Plan and apply

POST `/v1/admin/projects/<PROJECT>/addons/<ADDON_ID>/retained-generation-read`
with this closed request. Replace identifiers with existing authorized values;
choose one canonical UUID operation key before planning:

```json
{
  "profile": "portal-sim-generation-read",
  "service_id": "<GATEWAY_SERVICE_UUID>",
  "pedido": {
    "version": "crea-sim-generation-read-v1",
    "reservaId": "<EXERCISE_UUID>",
    "nucleoId": "<OWNED_KERNEL_ID>"
  },
  "dry_run": true,
  "idempotency_key": "<OPERATION_UUID>"
}
```

Only the three fixed synthetic-kernel IDs are accepted. The request cannot
choose SQL, URI, namespace, image, environment, command or filesystem path.
Unknown or duplicate JSON keys, aliases and trailing bytes are refused.

The dry run returns `plan_sha256` after native admission checks without creating
runtime objects. Review it. Apply the same identifiers and operation key with
`dry_run:false`, `expect_plan_sha256` equal to that fingerprint, and a concise
`reason`. A changed input, image, custody or authoritative identity refuses.
Read the returned Job name through:

`GET /v1/admin/projects/<PROJECT>/addons/<ADDON_ID>/retained-generation-read/<JOB_NAME>`

GET never collects logs, writes a receipt or repeats work. POST with the same
original input/key collects an already completed Job into an immutable receipt;
it does not rerun it. A timeout, partial creation, expired uncollected Job or
conflict stays held; inspect that operation rather than inventing another key.

## Job and receipt boundary

The fixed command feeds an immutable, Job-owned request file to the reviewed
reader over stdin. No caller bytes are interpolated into the command. The Job
carries only its runtime URI, database namespace and public CA; it does not copy
the gateway's authentication or kernel-control environment. It uses a nonroot,
read-only filesystem, no service-account token, zero retries, a bounded deadline
and database-only egress alongside namespace DNS. Existing managed Job, Secret,
network and signature-admission adapters are reused; no exec RBAC is added.

The collector requires exactly one owned completed Job/Pod, the exact command,
template and signed image digest, zero restarts and exit zero. It reads the full
bounded UTF-8 stdout, rejecting truncation, extra JSON and unknown fields. The
typed result must match the requested exercise/kernel and explicitly declare
`adoptionAuthorized:false`. Invalid typed output retains only its hash and a
closed held reason. Oversized or unreadable output remains uncollected; it never
becomes a successful provenance receipt. Raw output and driver errors are never
returned as receipt data.

Successful receipts are classified `PROVENANCE_ONLY`. Identity values remain
hashed; the proof includes original manifest/seed hashes, historical empty
counters and the observed portal phase. The reader uses a coherent REPEATABLE
READ READ ONLY snapshot and refuses an occupied kernel lock without retrying.
This historical evidence is not a fresh lease, an adoption intention/fence,
durable writer exclusion, owner-execution authority or a SSO promotion gate.

Operational exercise identifiers and retained receipts belong in the private
record, following the [public repository boundary](../PUBLIC_REPO_BOUNDARY.md).
