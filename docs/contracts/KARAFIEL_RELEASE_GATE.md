# Karafiel release gate API

This closed API accepts GitHub Actions OIDC identities for the reviewed Karafiel
release workflows. It does not grant a platform role or issue a general API token.
The pinned issuer is `https://token.actions.githubusercontent.com`, the audience is
`enclii-karafiel-release-v1`, and the repository is `madfam-org/karafiel`.

Signed identity claims must identify the first attempt of an approved workflow on
`main`. The API independently checks the actual in-progress GitHub run, source,
repository and owner IDs, event, workflow path and run attempt. A production
workflow must use the production environment subject. Staging acceptance and
bootstrap use the staging subject. Build and parity use the main-ref subject.
Tokens expire within ten minutes; errors never include tokens or provider bodies.

| Route below `/v1/releases/karafiel` | Capability |
| --- | --- |
| `POST /observe` | Exact staging or production application and pod observations |
| `GET /staging/prerequisites/:service` | Actual closed staging application prerequisites |
| `GET /staging/quarantine/:service` | Registered staging identity and zero-executor quarantine |
| `GET /staging/database/:service/:addon` | Owned dedicated staging database and credential custody |
| `POST /staging/sync` | CAS sync of the fixed staging application at its actual release ref |
| `POST /staging/migration/:service/:addon` | Fixed candidate-image staging migration profile |
| `GET /staging/migration/:service/:addon/:job` | Existing fixed migration observation |

Build may observe prerequisites, quarantine and database custody. Bootstrap adds
staging sync. Acceptance adds fixed migration execution and observation.
Promotion and failed-promotion resume may observe prerequisites and migration;
they cannot sync or mutate production through this API. Parity may only observe.

Requests are bounded and reject unknown or duplicate JSON fields. Read routes
reject query parameters and request bodies. Responses disable caching. Observations
omit node identity, secret values, free-form status messages, private source
parameters and taxpayer data. A hash or a source test is never live acceptance.

Staging sync requires the current application UID/resource version, an exact
revision matching `release/karafiel-staging`, a bounded idempotency key and reason.
The native sync operation checks the same fetched application before its CAS
update. Automatic staging sync must be explicitly disabled, replica overrides
must be absent, and the actual staging AppProject must retain its closed source,
destination and resource permissions. Global project inheritance is held.

The first quarantine sync also requires actual namespace `Prune=false` protection,
completed exact legacy tracking detachment and absence of its former policy exception.
A separate human-only reviewed adoption plan performs that narrow metadata handoff;
the release workflow cannot perform it.
The platform owns the namespace, database custody and fixed migration Job;
application Git must not create namespaces, RBAC or policy exceptions.

Human platform operations remain separate: held service metadata enrollment,
reviewed database preparation and any legacy production ownership enrollment use
operator authentication and a fresh reviewed plan fingerprint. Neither a GitHub
release identity nor a source-only receipt can substitute for that operator proof.
All financial and fiscal execution remains disabled through staging and release.

## Deployment order

1. Review and deploy this API through ordinary platform CI and image promotion.
2. Capture actual application ownership, refs, tracking and namespace protection.
3. Enroll the five production writer bindings against their current coherent
   source before changing that source; retain the immutable binding receipts.
4. Adopt the held release refs and restricted staging AppProject, then verify
   actual root reconciliation and repeated current source readback.
5. Materialize and sync the all-zero staging quarantine. Enroll the distinct held
   staging service, prepare its owned namespace/database and separate credentials,
   and verify actual Janua registrations and secret custody.
6. Build the normal-CI-green candidate, run fixed migration while executors are
   zero, serve API/web/admin, and retain first acceptance. Worker and beat stay zero.
7. Verify a fresh same-candidate observation after at least thirty minutes, then
   perform the normal same-digest production promotion with execution flags off.

Failure at any prerequisite holds advancement. A failed promotion may only resume
that same accepted promotion; a database reverse migration is not a rollback plan.

## Controller compatibility

The project proof verifies the actual closed ACL and tracking configuration.
It does not certify the controller against security advisories. The
3.2 controller line is affected by the upstream
[delete-hook permission bypass](https://github.com/argoproj/argo-cd/security/advisories/GHSA-fmxq-cgp8-87wp).
Current release operations admit reviewed flat staging inputs and never delete
an Application or use delete hooks. A reviewed supported-controller upgrade and
fleet acceptance remain required before claiming complete platform isolation.
