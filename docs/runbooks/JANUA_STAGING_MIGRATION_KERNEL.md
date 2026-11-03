# Fixed initial Janua staging migration kernel

Source preparation only. No HTTP route, CLI execution capability, Job, secret,
DB grant or staging activation is created by this change. The native identity
and database predecessors remain separately held. Do not add this profile to
the generic synthetic/native043 Job dispatcher.

`internal/januastagemigrate` implements a single initial migration from an empty,
dedicated `janua_staging` database. It provides no reset, stamp, arbitrary SQL,
command, image, namespace, role, seed, automatic retry or populated-DB upgrade.
Migration049 claims a unique retained database-operation slot before native
writes. A second key cannot repeat the migration or adopt native leftovers.

## Source and execution proof

The server must resolve the exact held API image, verify required main CI and
its keyless certificate's exact producer, workflow, main ref, allowed event and
build SHA, and read the complete source descriptor at that SHA. Manifest commit
and image build commit are separate identities. Their equality is not assumed.
The fixed helper reuses Enclii's existing certificate-SHA verification arguments;
no new attestation or BOM pipeline is required. Fulcio documents its signed
[workflow/source digest extensions](https://github.com/sigstore/fulcio/blob/main/docs/oid-info.md).
A source hint or mutable tag alone is insufficient.

The internal API source observer now reads the freshly qualified inert native
authority, retained plan and exact manifest files at its recorded revision. It
derives the API digest from that held overlay. The existing Janua API producer
does not set OCI source/revision labels; the observer discovers a candidate SHA
only from bounded, successfully verified cosign output and independently verifies
the same immutable digest again with the exact certificate SHA/event/repository/
main-ref/issuer/workflow identity filters. Candidate JSON alone is not authority.
The pinned cosign version's [certificate output implementation](https://github.com/sigstore/cosign/blob/v3.1.3/cmd/cosign/cli/verify/common.go)
exposes these fields; absent or ambiguous metadata refuses. No new image build,
OCI-label exception or SLSA pipeline is required.

The observer reuses the native content-addressed registry reader, corroborates
the exact revision tag, and requires the completed exact-SHA main API build and
normal CI/test workflows. Jobs must belong to the observed run and attempt;
mandatory API/PostgreSQL checks cannot be skipped. The existing disabled CI API
and dependent E2E jobs may remain skipped. The approved Playwright job is advisory
internally, so workflow success is not represented as a real portal/login proof.
The full source descriptor comes from the exact Git commit/tree and bounded,
Git-hash-verified blobs, refusing truncation, symlinks and submodules. Source
reads execute no producer code. CI/image evidence, native authority and dedicated
registry custody are re-read before the observation receives its current timestamp.

Dedicated registry custody remains a mandatory internal reader dependency with
no implementation or shared-secret fallback in this increment. Missing custody
fails before registry or source HTTP. No endpoint or kernel constructor wires
this observer, and no observed JSON or successful unit test supplies custody.

The descriptor hashes all application/migration Python files and Alembic config,
rejects ambiguous/disconnected/cyclic/dynamic revision declarations, and requires
one complete linear head. The actual image filesystem must match exactly before
application imports or database access. Source-only files cannot substitute for
an actual image, signature, Pod or CNI observation. The fixed command forces
reviewed source compilation rather than trusting stale bytecode.

The command uses Alembic's supplied-connection pattern with a fixed temporary
migration environment. It executes the verified revision files without API
startup, `create_all`, provider services, JWT keys or application credentials.
All current migrations fit this profile; a future unsupported graph fails closed.

Before DDL it verifies TLS hostname/CA, the exact database/login/session owner,
no privileged owner attributes/memberships, a nonowner runtime role, and an empty
schema/catalog. A database transaction and advisory lock cover the actual
revision upgrades. It refuses populated or partly initialized databases.

After upgrades it verifies the exact head, zero user/org/client/session/API-key
rows, effective runtime audit/provenance restrictions and table ownership. It
retains a fixed private provenance row and a metadata/ACL catalog hash. Only a
bounded schema receipt is printed. Errors and application output never expose
owner credentials or raw source/provider output.

## Native sequence and custody

All native execution requires fresh server observers for held source/CI/
signature, exact database UID/readiness, current platform administrator and
catalog, exact migration networking, and dedicated registry-pull custody. A
valid plan hash or JSON observation is never one of those authorities. The
registry reference is fixed to `janua-staging-registry` in the owned addon
namespace, with retained UID/version; ambient production/default-SA credentials
must not be inherited.

Strict dry admission creates no resource and reads no owner credential. Start
then durably claims the operation, creates the fixed suspended Job, records its
UID, reads separate owner custody, creates an immutable Job-owned Secret, records
its UID/data fingerprint, and CAS-unsuspends after fresh revalidation/readback.
No owner value is in Job environment/arguments, plan JSON, annotations or logs.
The nonroot Job has a read-only root filesystem, dropped capabilities, no SA
token/envFrom/PVC, one container, no retries/TTL, and a bounded deadline.

Terminal schema qualification requires the same Job/Pod UIDs, actual image ID,
zero restarts, fixed Pod execution fields, timestamps and an exact bounded
receipt. Success means `SCHEMA_APPLIED_RUNTIME_UNQUALIFIED`, never API readiness,
login proof, denied-path isolation, custody intake or a healthy soak. Historical
reads and credential removal can use the immutable verified receipt when current
CI/source activation has moved; current operator authority still applies.

Cleanup deletes only the retained owner Secret with UID/version preconditions
and no active or terminating Pod, preserving Job and SQL history. A known
pre-start failure can instead CAS-mark the exact still-suspended Job aborted,
prove no runtime/Pods, record `ABORTED_BEFORE_START`, and remove its exact retained
owner credential. The abort CAS and unsuspend CAS cannot both win. Abort history
prevents a later start. Unknown create/commit/unsuspend results remain held;
there is no speculative adoption or recovery under another operation ID.

## Remaining activation gates

The source/CI/certificate/descriptor observer is source-complete but still needs
dedicated registry custody. That custody adapter and concrete migration-network
observations must exist and be wired before exposing any execution endpoint.
The database kernel's namespace/network/capacity/cost/custody gates still apply.
A safe recovery adapter for unretained unknown writes or a revoked original actor
is not provided; retain and inspect the same operation rather than inventing a
raw fallback or automatic cleanup claim.

After owner-approved execution, independently verify actual isolated schema,
runtime credential intake, API/Redis startup, public issuer/JWKS separation,
a separately reviewed synthetic principal/client bootstrap, real authentication/refresh,
denied production access, schedule adoption and a
continuous healthy 30-minute soak. Production promotion remains blocked until
those native/runtime receipts exist. There is no executable owner command for
this kernel in the current CLI; existing capability/intake inspections remain
read-only. Never substitute legacy addon bootstrap or production credential reuse.

## Validation boundary

Required unit tests exercise strict source-byte verification with Python stdlib,
native transport fakes, missing gates, malicious admission/receipts, immutable
owner fingerprints, abort-versus-unsuspend resourceVersion conflict, pending/
terminating Pod refusal and terminal-only credential cleanup. Actual 049 DDL and
journal methods run in the existing required native PG18/TLS test lane.

An additional owned loopback PG14/verify-full proof ran all 22 actual Janua source
revisions to head 021 and refused a populated rerun. It used synthetic DNS routing
and a source directory; it is not a container, real Janua login, Kubernetes,
registry signature or CNI proof. No production database or identity was read.

Source-observer tests run the actual held-manifest, OCI byte/digest, CI workflow,
Git tree/blob and descriptor readers through synthetic transports and simulated
crypto-subprocess results. They include a label-free image and distinct manifest/
build commits, rejected certificate hints, skipped or foreign mandatory jobs,
pending latest runs, truncated trees, wrong blob bytes, custody/authority changes
and redirected credential-bearing requests. They are not live registry,
certificate or native staging receipts.
