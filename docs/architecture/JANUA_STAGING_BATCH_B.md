# Janua staging fixed migration and runtime grants

Batch B is a source-selected, initial-only operation for the owned Janua staging
database. Both Batch A and Batch B remain unregistered and apply disabled.
Merging this engineering does not provision a database, project credentials,
execute DDL, attach a runtime binding, enable authentication or send mail.

## Authority and custody

The operator, project, staging environment, completed repository registration,
held service and catalog addon are re-read from persisted authority. Immutable
Batch A operation, namespace birth, Cluster/runtime Secret and custody receipts
bind the same operation. A missing or uncertain receipt is held, never rebuilt.

The migrator reads only the exact CNPG-owned owner Secret and verified public
CA.
It does not widen the generic synthetic-only credential adapter. Owner and
runtime DSNs remain server-only and are never returned, logged or projected to
an application. Owner password length is bounded independently of the generated
runtime password format. Both custody identities are rechecked before use.

Batch A deliberately creates no active application database binding. Batch B
binds its authority to the operation/journal/custody identities instead of
inventing a binding. Other bootstrap profiles still require their real binding.

## Fixed execution

The profile admits only the current signed Janua API digest, actual OCI
source/revision, successful exact-source main producer and Tests workflows,
mandatory native API checks, and current staging pin. There is no historical-pin
or equivalent-PR-tree shortcut. The reviewed migration file hashes are fixed;
the actual verified final source SHA is recorded separately from the review
baseline, so a merge SHA is not fabricated from a PR head.

The suspended native Job uses the existing protected intent and activation CAS.
It has one worker, no retries, a 300-second deadline, 600-second TTL,
UID/GID1001,
read-only root, no service account token, dropped capabilities, RuntimeDefault,
bounded temporary storage, the public CA mount and exact owned database egress.
Namespace DNS-only baseline and current database policies must remain verified.
The root API image entrypoint is bypassed; no API lifespan or seed hook runs.

Freshness includes schemas, types, functions, relations and default ACLs, so a
pre-table partial attempt cannot pass as an empty database.

The fixed Python executor verifies every migration file, the single Alembic
head,
owner/database/role/CA identity and an empty catalog. It holds an advisory lock,
and applies the source graph using the internal isolated Alembic contract. Its
helper sets 5-second connect and 120-second command bounds. The real DDL
connection must prove PostgreSQL18, verified TLS and 120-second
statement/5-second
lock settings before DDL. Its receipt is published only after successful
migration commit. The executor verifies that receipt,
the actual head/catalog and synthetic counts, then grants the exact reviewed
contract. A separate runtime login verifies table
and column privileges without grant options or owner authority. The aggregate
receipt binds executor, migration, grant and catalog hashes to terminal native
Pod/image/exit evidence. Failed, partial, uncertain or expired operations do not
rerun under another key. This is not an upgrade, reset or adoption mechanism.

Revision 008 inserts two fixed provider-type catalog rows. They are counted
separately; every identity, client, session, provider credential and mail table
must be empty before native test fixtures are deliberately seeded.

## Supported runtime surface

`janua_staging_runtime_grants.json` contains the complete object inventory and
per-verb source references. The qualified surface is deliberately bounded:

- Existing password and pre-enrolled TOTP sessions, rotation and revocation.
- Owner-fixture preseeded magic-link consumption, including email verification.
- Existing first-party authorization-code/refresh and default-off bridge reads.
- Append-only audit/activity records and current membership/policy/entitlement
  reads needed to resolve the existing identity's claims.

Users have no runtime INSERT/DELETE or role/MFA/profile mutation. Only email
verification, lockout bookkeeping and its mapped update timestamp are writable.
Sessions permit INSERT and only rotation/revocation columns. Magic links permit
only their one-use timestamp update. OAuth clients permit only use telemetry and
its mapped timestamp, never registry policy changes. Audit/activity records have
no UPDATE/DELETE. All other migrated objects have no runtime privileges.

Signup, password reset, MFA enrollment/recovery, mail issuance, organization or
client administration, localization, billing, compliance and integration/content
management are outside this grant contract. No blanket table CRUD, default
future grants, owner runtime, role membership or privilege escalation is used.
Mail receipt guards remain installed and those tables are not granted.

## Required execution evidence

The hosted PostgreSQL18 gate builds the actual root `Dockerfile.api` from the
immutable reviewed Janua source and runs the exact executor with verify-full TLS
in a disposable, internal Docker network. It neither pushes nor signs/pins an
image. Its owner-only fixture seeds synthetic identities/clients/links after the
empty-database receipt. Runtime probes check migrated ORM reads, native source
JSONB/legacy string lookup, current pair-policy re-read, one-use SQL link
claims,
append audit behavior, rotation/revocation writes, and forbidden ownership,
membership/MFA/client-policy/secret-table authority. Fixture cleanup names only
its own randomly generated resources, never shared services.

These are database and packaging proofs. SQL one-use link claims are not an HTTP
consume test. Neither native helpers nor injected ASGI cookies establish actual
browser SameSite delivery, hosted MFA, PWA or application rollout readiness.
Those require the separately reviewed owned-stage execution and navigation
proof.
No local Docker/service setup or hosted synthetic authentication is required by
this source preparation. A skipped native gate is not a pass.
