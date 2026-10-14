# Isolated training and staging custody

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

## Bounded staging namespace bootstrap

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

| Profile | Workload | Fixed operation |
| --- | --- | --- |
| `map-sim-initial` | `crea-sim-map01` | Empty-database metadata guard, canonical Prisma migration, then compiled synthetic seed |
| `map02-sim-initial` | `crea-sim-map02` | Same fixed MAP operation, on kernel 02's own database and runtime role |
| `map03-sim-initial` | `crea-sim-map03` | Same fixed MAP operation, on kernel 03's own database and runtime role |
| `portal-sim-initial` | `crea-sim-gateway` | Bundled `001-portal.sql` through the fixed owner-only initial migrator |

The three MAP profiles share one definition. The profile name alone selects
the kernel's service, database and runtime role from a closed list. Each
kernel's Job name hashes its own project, addon, service and profile, so no
kernel can adopt another kernel's Job, intent or receipt. Kernel 01 keeps the
name `map-sim-initial` because its retained history carries it.

No reset, DROP, arbitrary migration file or Janua staging migration profile is
exposed. A partial MAP migration failure requires governed recovery review; it
cannot be retried as a populated initial database. The portal accepts its exact
existing immutable migration receipt and rejects a different/unknown schema.

The image must match its canonical producer's current immutable pin, actual OCI
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

Inspect the same operation with
`GET /v1/admin/projects/:slug/addons/:id/initial-bootstrap/:job_name`. It returns
only execution counts and suspension state, without credentials or raw logs.
A completed Job is not proof of durable MAP accreditation. The kernel's signed
private control/manifest must separately demonstrate `VACIO`, revision/epoch0,
no owner/exercise and the matching seven-role seed receipt before application
activation. Actual managed CA/TLS, runtime authority, storage/resource placement
and readiness still require live receipts; source/unit tests alone do not
establish them.
