# Fixed initial custody, version 2

The existing platform-admin initial-bootstrap POST retains its five-field closed
request (`profile`, `service_id`, `idempotency_key`, `dry_run`, `reason`). It does
not accept commands, SQL, images, namespaces, credentials or receipt content.
The project-access, completed onboarding, exact active addon binding, owned
workload held at zero replicas, CNPG ownership/public CA, signature enforcement,
producer OCI/source/main CI matrix and cosign verification remain required.
The observed Service selector must match the exact Deployment selector.

`portal-sim-initial` has exactly four secret keys: `ca.crt`,
`CREA_SIM_MIGRATOR_DATABASE_URL`, `CREA_SIM_PORTAL_DATABASE_URL` and
`CREA_SIM_DATABASE_NAMESPACE`. The independent runtime URI must match the
actual held workload. Its raw binding may use only the supported unique
`sslmode=require|verify-full` and an optional exact fixed `sslrootcert`. This
is input compatibility, not TLS proof: the server-generated Job always uses
verify-full and the fixed mounted public CA, and 002 verifies the actual TLS
connections independently. Unknown modes, duplicate/extra query keys or another
CA are refused.
The fixed image runs:

1. Init `portal001`: `node packages/entrenamiento/src/migrar.ts`.
2. Main `initial`: `node packages/entrenamiento/src/preparar-custodia-reset.ts`.

The ordinary migrator owns the dedicated database; the runtime must be a
separate ordinary nonowner without superuser/BYPASSRLS, memberships in either
direction, DDL or TEMP. The 002 source verifies the two actual TLS connections,
PG18, matching native database identity, the retained 001 preparation and exact
catalog before returning its typed receipt. No runtime owner credentials enter
application configuration. The source-only fixture proves those commands and
checks; it is not a canonical gateway-image or hosted admission/readiness proof.

Three MAP profiles prepare the three training kernels: `map-sim-initial`
(kernel 01), `map02-sim-initial` and `map03-sim-initial`. They share one
definition. The profile name alone selects the kernel's service, database and
runtime role from a closed list, and the retained intent must carry exactly that
database and role. Every MAP-specific step below applies to all three alike.
Kernels 02 and 03 share kernel 01's Vault path under their own prefixed keys,
so each kernel's ExternalSecret may read only that kernel's properties from it.

The fixed MAP fresh guard, Prisma migration and compiled seed commands are
unchanged. Its five-key stdout remains `huella`, `codigoSha256`,
`fixtureSha256`, `roles`, `casos`: `roles=7` Member templates and `casos=6`
synthetic cases. The separate manifest has seven lessons; its lesson count is
not the seed stdout case count. The initial
portal/manifest wire works with the current initial MAP source. The separately
governed fresh reset-origin witness is required for reusable reset generations;
legacy initial metadata cannot be adopted as that witness.

## Immutable history and uncertainty

The desired spec explicitly sets the public CA Secret volume's `defaultMode=0644`
and the two fixed `Exists/NoExecute` tolerations for `node.kubernetes.io/not-ready`
and `node.kubernetes.io/unreachable`, each for 300 seconds. These match the
[Kubernetes v1.36.4 core default](https://github.com/kubernetes/kubernetes/blob/v1.36.4/pkg/apis/core/v1/defaults.go)
and [DefaultTolerationSeconds admission source](https://github.com/kubernetes/kubernetes/blob/v1.36.4/plugin/pkg/admission/defaulttolerationseconds/admission.go),
corresponding to the pinned client/API version. They are explicit desired values;
other modes/tolerations or spec changes remain refused by strict matching.
This source compatibility proof does not establish the live server's version,
plugin configuration or admission result.

Admission is checked using server dry-run against actual Job/Pod policy before
any persisted Job. The server creates one suspended Job, verifies its returned
UID/spec/resourceVersion and then creates an immutable namespace-owned
`<job>-intent` ConfigMap. This binds the observed Job UID/creation version,
project/addon/service/binding/onboarding IDs, actual namespace/Deployment/Service/
CNPG UIDs, database/roles/public-CA hash, verified producer image/source/build/CI/
signature and hashes of the exact plan, Job spec, operation key and verified pin
payload. `pin_payload_sha256` is a payload hash, **not a Git pin revision**.
No unavailable Git revision is inferred. Credential values remain transient.

Only after that intent exists may fixed pull/Job credential custody and database
egress be prepared. Authority, namespace, registry custody, retained intent and
same suspended Job UID/spec are checked again. Activation uses its freshly read
resourceVersion in one PUT. The creation version is historical; normal Job
controller status changes do not rewrite it.

Each profile's first init container, `network-ready`, waits at most 120 s for
one TCP handshake with its own database host before any database step. The
Job's egress policy is created moments before activation, and a new Pod IP must
also reach the addon's ingress rules. Until the CNI programs both, connections
are refused at once, so a guard that tries once would hold the Job on a
transient (observed 2026-10-14 on `map-sim-initial`: `fresh-only` exited within
seconds with no server-side log). The probe uses the image's own `node`, does
not authenticate or read data, and prints only fixed messages, never the URL.
It changes the Job spec hash, so it applies to new operations only; it does not
resume or adopt held history.

The Job name is deterministic for project/addon/service/profile and excludes the
request key. Each MAP kernel therefore has its own Job name, and kernel 01's
name is unchanged. Its namespace Create is the atomic arbiter: concurrent
requests with the same or different keys can persist/activate only one Job. The
operation-key SHA remains in the immutable intent; a later incompatible-key
POST is refused.
The history list remains a refusal guard for old keyed Job/intent names, which
cannot be adopted into this new name. No database or extra launch lock is added.

A timed-out create/PUT does not retry, repair, adopt or create a new operation.
The implementation reads the same ID after an unknown create outcome and
returns held. A later POST cannot resume a suspended/failed Job, a missing
intent or an intent whose Job/receipt is missing. Another operation key is
refused while initial history for the same addon/service exists. No third
launch record or reset adapter is introduced.

After a complete Job, the explicit same-key POST may collect once. The server
requires the exact retained Job UID/spec, one controller-owned successful Pod,
fixed init/main source image/digest and zero exit/restarts for every container.
It reads only fixed container stdout with an 8192-byte UTF-8 cap and one closed
JSON object, rejecting duplicate/unknown/missing keys, malformed values and
already-prepared output. Portal001/002 must report fresh `PREPARADO`; MAP keeps
its fixed seed schema. No raw logs are returned. Job, Pod, intent and authority
are re-read before writing the immutable `<job>-receipt` ConfigMap. A conflict
or unknown receipt-write result returns held and performs only a same-ID read.

Both ConfigMaps belong to the namespace UID rather than the TTL Job. Their
closed payload/hash/labels/ownership are validated; they cannot be repaired or
updated. The existing GET is read-only, including after Job TTL deletion. It
never collects logs or creates a ConfigMap. Suspended/failed history is labelled
`INITIAL_ONLY_HELD`; completed execution awaiting explicit retention is
`INITIAL_ONLY_UNRETAINED`. A retained typed receipt is `INITIAL_ONLY`.

These records do not assert current Ready, a native reset origin, exclusion,
release ACK, a live SSO exercise, mail delivery or reusable custody. Actual
Enclii source/admission, human secret-policy readback and hosted UID/CA/ordinary
role checks remain independent gates before any live initial bootstrap.

## Public boundary checkpoint

- Date: 2026-10-14.
- Owner: Enclii initial-custody source maintainers; root review before release.
- Public-safe change: fixed initial profiles, bounded retained receipt contracts
  and source/native proof boundaries.
- Private sink: workload identities, credential/CA custody, operational topology,
  native fixture receipts and hosted admission/readiness evidence remain in
  `internal-devops`; no raw operational values are published here.
- Policy: [local public boundary](../PUBLIC_REPO_BOUNDARY.md) and
  [canonical repository boundary contract](https://github.com/madfam-org/internal-devops/blob/main/docs/repo-boundary-contract.md).
