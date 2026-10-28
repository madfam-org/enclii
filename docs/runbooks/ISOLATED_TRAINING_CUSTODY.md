# Isolated training and staging custody

Last Updated: 2026-10-28

> Boundary checkpoint (2026-10-14, platform preparation): public-safe logical
> target names, namespaces, key names and callbacks only. Actual credentials,
> human/organization identifiers and operational receipts stay in the private
> operator workspace and `internal-devops`. Policy:
> [Public repository boundary](../PUBLIC_REPO_BOUNDARY.md).

Source declarations establish allowed targets, not deployed credentials or a
working training environment. Verify the live target catalog before creating a
client whose secret is returned only once. Use the governed Enclii intake and
OIDC provisioner; never print secrets, copy production material into staging or
write provider/cluster secrets outside those adapters.

## Human training clients

The two embedded registry copies declare confidential authorization-code/PKCE
clients with rotating refresh:

| Logical name/audience | Exact callback | Intake target |
| --- | --- | --- |
| `crea-sim` | `https://sim.creatumundo.mx/auth/callback` | `crea-sim/ui-oidc` |
| `crea-sim-mcp` | `https://sim.creatumundo.mx/auth/mcp/callback` | `crea-sim/mcp-oidc` |

Registration produces the actual `jnc_*` identifier. A logical name is never a
substitute for that identifier. Both declarations initially request only
`openid`, `profile`, `email`, and `offline_access`, which the existing deployed
registration contract accepts. The manual UI pilot keeps MCP/chat disabled.
Reconcile the same MCP client with training-only resource scopes through a
separate reviewed policy release after actual isolated Janua staging and normal
promotion; this declaration grants neither production MAP scopes nor training
write scopes.

An invited human must prove fresh signed active membership in the operator's
exact configured CTM organization. No email match, service account or client
registration creates human membership or a production clinical role. A future
organization-bound Selva producer uses independent machine credentials and its
sole approved inference scope; the human UI/MCP credentials do not travel to
the inference service.

## Closed intake targets

| Targets | Vault path | ExternalSecret |
| --- | --- | --- |
| `crea-sim/ui-oidc`, `crea-sim/mcp-oidc`, `crea-sim/gateway-runtime` | `secret/crea-sim-gateway` | `crea-sim-gateway` |
| `crea-sim/gateway-ca` | `secret/crea-sim-gateway` | `crea-sim-gateway-ca` |
| `crea-sim/capture-runtime` | `secret/crea-sim-captura` | `crea-sim-captura` |
| `crea-sim/capture-ca` | `secret/crea-sim-captura` | `crea-sim-captura-ca` |
| `crea-sim/kernel01-runtime` | `secret/crea-sim-map01` | `crea-sim-map01` |
| `crea-sim/kernel01-ca` | `secret/crea-sim-map01` | `crea-sim-map01-ca` |
| `crea-sim/kernel02-runtime` | `secret/crea-sim-map01` | `crea-sim-map02` |
| `crea-sim/kernel02-ca` | `secret/crea-sim-map01` | `crea-sim-map02-ca` |
| `crea-sim/kernel03-runtime` | `secret/crea-sim-map01` | `crea-sim-map03` |
| `crea-sim/kernel03-ca` | `secret/crea-sim-map01` | `crea-sim-map03-ca` |
| `crea-sim/selva-oidc` | `secret/crea-sim-selva` | `crea-sim-selva` |
| `janua-staging/runtime-crypto`, `janua-staging/database` | `secret/janua-staging` | `janua-staging-secrets` |
| `janua-staging/database-ca` | `secret/janua-staging` | `janua-staging-ca` |
| `janua-staging/redis` | `secret/janua-staging` | `janua-staging-redis` |

The registry specifies each target's exact key allowlist and namespace. CA
targets accept the verified public certificate bundle, never its private key.
The canonical writer policy source includes these five exact Vault data paths
and their metadata paths. No child wildcard or owner-credential destination is
added. Source parity and an available intake catalog do not prove the running
writer token has those permissions. Before minting one-time client secrets,
verify governed live policy capacity. The legacy policy script contains raw
bootstrap commands; routine policy activation must use a supported Enclii
adapter. Record the missing adapter as a blocker rather than executing those
raw commands or granting policy-write authority to the application writer.

Kernels 02 and 03 file into kernel 01's Vault path under prefixed keys
(`map02_*`, `map03_*`), so the writer policy keeps exactly five data paths. A
separate path per kernel would need a Vault policy change, and routine policy
activation has no supported adapter. Sharing the path does not share
credentials. Each kernel has its own database URL, its own context, control,
mail and pseudonym keys, and its own CA property. Each kernel's ExternalSecret
projects only its own properties, and initial bootstrap refuses a kernel 02 or
03 ExternalSecret that reads any other property or the whole path. Isolation
between kernels is per CNPG cluster and per key, not per Vault path. The only
shared property is the public MCP client identifier, filed once through
`crea-sim/kernel01-runtime`. The gateway and capture targets carry one
`simNN_*` key set per kernel. Intake merges into the existing path with a read
and a write, so file the targets that share a path one at a time.

Runtime database targets carry only the managed nonowner URI for the dedicated
synthetic database. Migrator credentials stay in the separately owned CNPG
Secret and never enter an application intake target.

Generate fresh material in the approved private custody flow with the exact
consumer format. The generic intake generator emits unpadded base64url; it is
not a 64-character hex sealing/HMAC key, an RSA PEM pair or a padded Fernet key.
Do not use that default generator for incompatible fields. The context,
control, mail and session keys are distinct by purpose; only the intended two
ends receive each shared key. Registration, invitation and message delivery
are separate operations; this runbook does not send invitations or mail.

### Typed Janua staging initialization

The two fixed profiles below initialize only the existing isolated staging
targets. Their identity, Vault path, namespace, ExternalSecret and complete key
set are validated when the registry loads. The endpoint retains the existing
platform operator checks; no new role or Vault path is added.

| Target | Server-side format |
| --- | --- |
| `janua-staging/runtime-crypto` | One matched 2048-bit RSA pair (PKCS8 private / PKIX public PEM), independent 32-byte JWT and application secrets, and a 32-byte padded URL-safe Fernet key |
| `janua-staging/redis` | One 32-byte password and a matching `redis://` URI for the fixed same-namespace `janua-staging-redis` service, port 6379, database 0 |

Submit the complete declared key group with `--generate`; partial generation or
supplied values for either profile refuse before Vault writes. The database URI
and public CA targets are supplied-only: random bytes cannot establish database
authority or trust.

After the reviewed Switchyard release is deployed, an authorized operator may
initialize each group separately and collect only its metadata receipt:

The updated CLI requires the server's typed/supplied-only `generation` metadata
for all four Janua staging targets. An older server without that metadata is
refused before input or submission; a new CLI alone cannot make its generic
generator safe. Verify the deployed API release and live target policy first.

```bash
enclii secrets intake submit janua-staging/runtime-crypto \
  --generate jwt_private_key,jwt_public_key,jwt_secret_key,secret_key,field_encryption_key \
  --reason "initialize fresh isolated staging crypto"
```

Review the receipt before the next separate operation:

```bash
enclii secrets intake submit janua-staging/redis \
  --generate redis_password,redis_url \
  --reason "initialize fresh isolated staging Redis authentication"
```

Unlike generic generated-key intake, these profiles **do not rotate** existing
custody. Initialization reads the KV v2 version, refuses if any group key exists
(including an empty or partial group), preserves unrelated properties, and uses
an atomic compare-and-set write. Concurrent changes refuse with HTTP 409 and no
retry. File other targets sharing this Vault path serially: legacy generic
merges are still read/write operations without a compare-and-set guarantee.
If the response is lost or the write receipt cannot be verified, inspect the
existing custody state through an authorized operator flow before proceeding;
never mint again to guess the result. A writer without atomic initialization
support refuses with 503 instead of falling back to an overwriting merge.

Values remain inside the existing write-only intake/Vault flow. Responses and
cached status carry only generated key names, version and audit metadata. An
accepted intake or `external_secret_refreshed` does not establish projected
Secret readiness, a running Redis, a database migration, a healthy application,
staging soak, production promotion or SSO activation. This implementation does
not execute any of those operations or introduce an owner/migrator credential.

## Bounded staging namespace bootstrap

The separate [read-only Janua addon plan](JANUA_STAGING_ADDON_PLAN.md) verifies
persisted staging ownership, namespace metadata and catalog allocation without
creating rows/resources or reading credentials. A plan response cannot activate
the namespace or replace the still-missing typed staging provisioning adapter.

`POST /v1/admin/projects/:slug/environments/staging/bootstrap` defaults to
`dry_run: true`. It uses the existing platform-rank and project/acting-tenant
guards, preserving the documented tenant-scope rollback policy. It requires a
persisted staging environment, the exact `<slug>-staging` namespace and a
completed same-project repository onboarding registration. Apply requires a
reason.

The helper prepares only the namespace and centrally owned DNS-only
default-deny. Existing namespaces and policies need the exact project identity;
untagged or foreign objects are refused before writes. Dry runs do not create
or label objects. Fresh creation reuses the UID-checked onboarding rollback if
default-deny fails. It does not retarget production Argo, create workloads,
register routes, grant identities or transfer credentials.

The existing ApplicationSet reads `infra/argocd/projects/*/config.json` from
reviewed main. A separate staging declaration must be reviewed only after
custody, addon binding, resource/storage capacity, image admission, managed
migration and exact network rules are proven. There is no staging declaration
or live activation in this change. A pin timestamp cannot prove a healthy soak.

See [Secret intake](SECRET_INTAKE.md),
[namespace and onboarding](../guides/ONBOARDING_GUIDE.md), and the managed
database isolation contract.

## Fixed initial database Jobs

`POST /v1/admin/projects/:slug/addons/:id/initial-bootstrap` accepts only
`profile`, `service_id`, `dry_run`, `reason` and `idempotency_key`. Unknown fields,
arbitrary commands, SQL, images and namespace selectors are refused. The shared
platform/project guard and durable owned source registration are required. Both
initial execution and status routes, the staging namespace bootstrap and central
intake targets/submit/status also require the unconditional server-resolved
platform operator rank. `ENCLII_TENANT_SCOPE_ENFORCE=false` cannot grant these
capabilities to a tenant administrator; unset retains the existing enforcement
default. Other legacy route policies and the global rollout flag are unchanged.
The active bound addon must be the exact Ready isolated PostgreSQL profile;
the owned application stays at zero replicas throughout initial preparation.
The fixed steps `portal-sim-turno-vigente` and
`portal-sim-cierre-reconstruccion-paso` are the exception: they run against
the live portal database, so the gateway may serve, but its rollout must have
converged on its pinned image.

| Profile | Workload | Fixed operation |
| --- | --- | --- |
| `map-sim-initial` | `crea-sim-map01` | Empty-database metadata guard, canonical Prisma migration, then compiled synthetic seed |
| `map02-sim-initial` | `crea-sim-map02` | Same fixed MAP operation, on kernel 02's own database and runtime role |
| `map03-sim-initial` | `crea-sim-map03` | Same fixed MAP operation, on kernel 03's own database and runtime role |
| `portal-sim-initial` | `crea-sim-gateway` | Bundled `001-portal.sql` through the fixed owner-only initial migrator |
| `portal-sim-turno-vigente` | `crea-sim-gateway` | Fixed step 003 (`003-turno-vigente-por-persona.sql`) on the database `portal-sim-initial` prepared; re-runnable under a new key; answers `APLICADO` or `YA_APLICADO` |
| `portal-sim-cierre-reconstruccion-paso` | `crea-sim-gateway` | Fixed step 004 (`004-cierre-por-reconstruccion.sql`) after an accredited 003; adds the close-by-rebuild catalogue and closes no reservation; re-runnable under a new key; answers `APLICADO` or `YA_APLICADO` |

The three MAP profiles share one definition. The profile name alone selects
the kernel's service, database and runtime role from a closed list. Each
kernel's Job name hashes its own project, addon, service and profile, so no
kernel can adopt another kernel's Job, intent or receipt. Kernel 01 keeps the
name `map-sim-initial` because its retained history carries it.

No reset, DROP, arbitrary migration file or Janua staging migration profile is
exposed. A partial MAP migration failure requires governed recovery review; it
cannot be retried as a populated initial database. The portal accepts its exact
existing immutable migration receipt and rejects a different/unknown schema.

The image must match its canonical producer's current immutable pin (a MAP
kernel may match a recent one; see below), actual OCI
source/revision labels, exact revision tag and successful main caller workflow
and closed source CI. MAP requires all fifteen named CI checks to complete with
`success`; skipped jobs cannot replace a required check. The portal requires
its isolation and real PostgreSQL/Mailpit transport checks to succeed. Keyless verification binds the permitted workflow SAN,
GitHub OIDC issuer, caller repository, revision and ref, preserving transparency
and certificate verification. See the supported
[Cosign verification options](https://github.com/sigstore/cosign/blob/v3.1.3/doc/cosign_verify.md).
MAP's existing producer supplies this signed build/pin contract; it does not
publish an image SBOM/scan attestation. That producer-library improvement is a
separate follow-up and is not claimed by this receipt. No fictitious platform
Release row substitutes for GitOps producer evidence.

A MAP kernel's image may trail crea-map's current pin, because crea-map
re-pins on every deploy. It is still admitted when crea-map pinned that digest
on `main` within its last 30 pin revisions and 14 days. The dry run then shows
`producer.pin_match=historical` and the crea-map commit in
`producer.pin_revision`. Outside that window the preflight answers 409
`owned image is not among the producer's recent main pins`: re-pin crea-sim's
MAP image to crea-map's current pin and roll it out first. The portal profiles
still require the current pin.

The server verifies actual namespace/project and CNPG Cluster/owner/runtime/CA
UID custody and current runtime-role reconciliation. Owner credentials appear
only in a transient Job-owned Secret; ordinary addon credentials remain
runtime-only and the private owner bundle cannot serialize into an API response.
Only `ca.crt` is mounted. Both Job and representative Pod pass actual server
dry-run admission in a signature-enforcing namespace before persistence.

The Job starts suspended, has zero automatic retries, and reaches only its own
exact database cluster on TCP5432 plus the existing DNS rule. Foreign broad
egress or conflicting objects are refused. Before activation the server rechecks
the binding, held workload, private configuration, Cluster UID and credential/CA
identity; changed authority leaves the Job suspended. Errors never trigger a
new operation key or rerun automatically. The Job uses MAP UID/GID1001 or
portal UID/GID1000, a read-only filesystem and a writable temporary volume.
Registry custody is copied only from central `enclii/ghcr-credentials` to the
verified project-addon namespace, selecting only the GHCR entry. Its namespace
owner UID, source UID, project labels and credential bytes must remain exact;
foreign/existing conflicts and rotation refuse activation without adoption.
Dry run only submits server-side dry-run copies; ordinary credentials remain
separate from application/migrator custody.

The bounded generation reader, fence reader and reconstruction close may finish
retaining an intent after a suspended create was interrupted. Before creating
these Jobs, the server retains the complete original pre-UID plan in a separate
immutable namespace-owned preparation; the Job references its UID and hash.
Repeat the exact original apply body and reviewed plan under the same key. The
server requires that exact original preparation, generation one, the exact
fixed Job and fingerprint, no execution evidence,
a complete empty owned-Pod read, and absent intent, receipt, transient credentials
and network policy. It rechecks the UID/resourceVersion and current reviewed
authority, retains the immutable intent, then uses the normal single activation
CAS. A legacy Job without an original preparation stays held; the server never
creates one retroactively. A previously activated, changed or uncertain Job
stays held; initial
migration/seed profiles cannot use this recovery. Reader and close refusals
return a safe `hold_stage` code without raw admission or credential values.

### Retire an unexecuted provenance reader

A legacy reader without its original preparation cannot be adopted. A separate
platform-operator action may retire only a positively unexecuted provenance
reader; it cannot retire migration, seed or reconstruction-close Jobs.

POST `/v1/admin/projects/<slug>/addons/<addon-id>/retained-provenance-read/<read-job>/retire`
with the closed `action=RETIRE_UNEXECUTED_PROVENANCE_READ`, reader `profile`,
`service_id`, original UUID `idempotency_key` and `dry_run=true`. The native Job
must match that exact operation name, owned scope and fixed bounded reader spec,
remain suspended at generation one, and have no execution status, owned Pod,
intent, receipt, transient credentials or database network policy. Unknown or
partial reads refuse. The returned `retirement.plan_sha256` binds its native
UID, resourceVersion, spec and annotation hashes, not a historical identity or
database observation.

Review the plan, then repeat with `dry_run=false`, that exact
`expect_plan_sha256` and a bounded `reason`. The server atomically claims the
existing immutable intent name as `NON_EXECUTION_RETIREMENT_CLAIM`, permanently
denying execution under the old key. It makes one metadata-only Job CAS with
the exact observed UID/resourceVersion, preserving its spec, suspension and
generation, then retains a separate immutable completion only after positive
native readback. `NON_EXECUTION_RETIREMENT`, `conclusive=true` and
`execution_authorized=false` identify completion. The original Job remains.

GET the sibling `.../<read-job>/retirement` route for read-only confirmation.
The original reader status may also report the retirement class, with no native
fence receipt. A lost CAS response or incomplete claim stays held and never
creates an exclusion exception. An explicit same-key collection can retain a
completion from positive native markers/readback; it never retries the CAS.
Claim alone, marker alone, foreign records, UID/spec/generation/execution/Pod
or transient-resource drift remain blockers. The old key stays denied even
after the Job disappears.

Only a conclusively inert retired reader may be ignored by later operation
exclusion. Each new read, seed or close still requires its own independent
current authority; retirement supplies no fence, seed, reset, release, adoption
or reconstruction-close proof. Missing or stale close evidence still refuses.

### Fixed step 003 (`portal-sim-turno-vigente`)

The contract is in
[Fixed initial custody](../architecture/INITIAL_BOOTSTRAP_CUSTODY.md#fixed-step-portal-sim-turno-vigente).
Run it only after the crea-sim gateway image that carries step 003 is merged,
pinned on main and rolled out; the request refuses otherwise. The owner runs it
with an operator login, in three steps, against the portal addon and the
`crea-sim-gateway` service:

1. **Dry run.** POST the profile and `service_id` with no `dry_run` field.
   Expect HTTP 200, `job.dry_run=true`, `job.classification=FIXED_STEP_EXECUTION`
   and `producer.verified_sources` naming both step files. A 409 names its
   refusal stage; nothing was created.
2. **Apply once.** POST again with `dry_run=false`, a new `idempotency_key`
   and a `reason`. Expect HTTP 200 and `job.name` beginning `step-`. Poll the
   GET route with that name until `succeeded` or `failed` is non-zero.
3. **Collect with the same key.** Repeat the step 2 body exactly, within the
   Job's one-hour TTL. Expect `FIXED_STEP_APPLIED` with
   `native_receipt.turno_vigente.estado` `APLICADO`, or `YA_APLICADO` when 003
   was already applied. `FIXED_STEP_HELD` names its `held_reason`; read the
   Job's logs through Enclii before choosing a new key.

A re-run uses a new key, once the earlier Job has finished. It never reuses
or rewrites an earlier key's records.

### Runtime reset-fence observation

The separate `portal-sim-reset-fence-read` operation resolves a retained fence
by one of the three registered kernel IDs. It uses the runtime account only;
its dry-run admits a plan, and apply runs the fixed read-only reader. See
[the request and receipt contract](../architecture/PORTAL_RESET_FENCE_READ.md).
Its historical receipt does not close a reservation or enable fleet reset.

### Fixed step 004 (`portal-sim-cierre-reconstruccion-paso`)

The contract is in
[Fixed initial custody](../architecture/INITIAL_BOOTSTRAP_CUSTODY.md#fixed-step-portal-sim-cierre-reconstruccion-paso).
Run it once per portal database, only after the crea-sim gateway image that
carries step 004 (crea-sim#112) is merged, pinned on main and rolled out, and
only after step 003 has been applied there; the request refuses otherwise.
The sequence is step 003's, with the profile `portal-sim-cierre-reconstruccion-paso`:

1. **Dry run.** Expect `job.classification=FIXED_STEP_EXECUTION` and
   `producer.verified_sources` naming the 004 SQL and
   `preparar-cierre-reconstruccion.ts`.
2. **Apply once** with a new `idempotency_key` and a `reason`; poll the GET
   route until the Job finishes.
3. **Collect with the same key.** Expect `FIXED_STEP_APPLIED` with
   `native_receipt.cierre_reconstruccion_paso.estado` `APLICADO` (or
   `YA_APLICADO`). `FIXED_STEP_HELD` names its `held_reason`.

This prepares the catalogue only. A reservation left `RESTABLECIENDO` stays
retained until the separate witnessed close profile appends its accredited receipt.
See [Witnessed portal close](../architecture/PORTAL_RECONSTRUCTION_CLOSE.md):
collect a new runtime fence observation with its server-owned origin before
changing the kernel; after reconstruction, review/apply/collect the close with
that same observation reference. The server enforces platform-operator rank,
not physical human presence; custody capability checks remain independent.

Inspect the same operation with
`GET /v1/admin/projects/:slug/addons/:id/initial-bootstrap/:job_name`. It returns
only execution counts and suspension state, without credentials or raw logs.
A completed Job is not proof of durable MAP accreditation. The kernel's signed
private control/manifest must separately demonstrate `VACIO`, revision/epoch0,
no owner/exercise and the matching seven-role seed receipt before application
activation. Actual managed CA/TLS, runtime authority, storage/resource placement
and readiness still require live receipts; source/unit tests alone do not
establish them.
