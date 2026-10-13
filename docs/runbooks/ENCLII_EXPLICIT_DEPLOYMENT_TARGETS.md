# Explicit Deployment targets

A registered service can use a differently named Kubernetes Deployment. For
example, service `fixture` may use Deployment `fixture-web`. Observation labels
and suffixes cannot authorize that target. This adapter requires an explicit
operator plan and immutable binding to the actual object and registered source.

It provides `POST /v1/ops/apps/bind-target` for plan/apply and
`GET /v1/ops/apps/bind-target/:idempotency_id` for receipt inspection. Both routes
require the server-resolved platform operator rank regardless of the tenant
scope rollout flag; apply and receipt reads recheck that rank in SQL. Apply writes only the binding and its audit receipt in one
transaction; it does not change Kubernetes resources or stop writers.

## Plan, review and apply

```json
{
  "dry_run": true,
  "scope": {
    "project": "fixture",
    "service": "11000000-0000-4000-8000-000000000002",
    "environment": "production"
  },
  "args": {"namespace": "fixture", "deployment": "fixture-web"}
}
```

Review the returned identity, native resourceVersions, source and `plan_sha256`.
To apply, use the same scope/args, `dry_run: false`, a nonempty `reason`, a fresh
canonical UUID `idempotency_key`, and `expect_plan_sha256` containing that exact
reviewed hash. The server re-reads authoritative intent, native objects and source
under the shared service/intent locks and refuses any plan change. The closed
request is limited to 8192 bytes and rejects unknown/duplicate keys. There is no
selector, suffix, caller image/ref/source, arbitrary namespace adoption,
credential input, command or SQL.

The server requires complete persisted service/project/environment and completed
onboarding intent: repository, namespace, manifest path, branch and Argo
Application. A namespace claimed by another project's environment/registration
is refused. Missing or conflicting intent remains a diagnosed prerequisite;
labels do not fill it. This source release does not prove a particular live
registration has those prerequisites.

Exact Namespace/Application/Deployment reads require current nondeleting UIDs
and resourceVersions, the registered single-source destination, and
self-referencing Argo tracking. The fixed `argocd/argocd-cm` read must explicitly
configure `annotation` or `annotation+label` tracking; absent or label-only
configuration is refused. No installed default is guessed. ConfigMap UID,
tracking method and installation ID belong to immutable authority identity.
ConfigMap resourceVersion belongs to the reviewed plan and audit, rather than
permanent identity: an unrelated update preserving those authority fields does
not invalidate every later operation.

The Application compared/synced immutable revision must declare the exact target
once through explicitly referenced flat Kustomize resources. Remote/deferred
resources, duplicates and unsupported transforms are refused. Source selectors,
labels and container identity corroborate ownership. Actual images must be
immutable digest references. This adapter does not itself prove registry
signature/admission policy or enroll an image for simulator reset authority.

## Persistence and unknown outcomes

Migration 043 creates empty immutable bindings and immutable audit receipts. It
uses restrictive project/service/environment/registration foreign keys, unique
service/environment and Namespace/Deployment UID targets, payload checks, and
refusing UPDATE/DELETE/TRUNCATE triggers. Nothing is backfilled or rebound.
Binding and receipt insert atomically. Rollback refuses retained history.

A reused idempotency UUID is accepted only for the exact stored request intent,
actor and reviewed hash. A changed request conflicts. If commit is uncertain,
the handler performs one read of that same UUID and never repeats insertion.
Receipt inspection returns only bounded proof metadata; free-text reasons stay
in the private audit record. SQL/read uncertainty is unavailable. `not_recorded`
means only that no receipt was found; it does not authorize retry or prove native
state. Plan and request hashes are fingerprints, not caller authority.

## Bound mutation behavior

Scale/restart use the shared service advisory/row lock before deciding whether a
service is bound. For a bound service they require its exact environment,
operator authority and fresh native/source/Argo ownership proof. They submit one
UPDATE of the verified Deployment with its actual UID/resourceVersion; there is
no second name lookup, conflict retry, suffix inference or UID refresh. Only the
replica field or restart annotation changes. Fresh reviewed versions/source are
recorded in audit. The same object and authority may follow an ordinary source
revision; changing source location, UID, tracking method, installation ID or
declared target identity refuses the operation.

Bound service/project deletion, direct deploy/rollback (including instant
rollback), canary paths, secret rotation, queued reconciliation/results,
post-deploy rollback, delayed notifications, automatic deployment and grouped
metadata writers refuse before their respective effects. Group execution holds
its frozen affected service set in UUID order and child writers consume that
same session, avoiding nested same-service locks. Empty-project execution and empty-group rollback retain their legacy metadata
behavior under a project guard that refuses bound projects. The current
`ListByGroup` repository returns an empty list; no children are invented.

Genuinely unbound services retain their exact legacy namespace/name target and
ordinary best-effort metadata outcomes. Service rows use `FOR NO KEY UPDATE` so
legacy release/deployment FK inserts can proceed while binding remains excluded.
The lock transaction has no shorter independent timer or automatic rollback on
caller cancellation. Queries/native calls still use the caller context, but the
lock survives until the callback completes result classification and explicitly
closes. A failed process/connection can still release a database lock; it is not
distributed fencing of a remote in-flight request.

A transport, audit or commit failure after a native UPDATE can leave its effect
unknown. Inspect actual state; no success is inferred and no automatic retry is
performed. The presence-checked replica pointer admits explicit numeric zero while
rejecting omitted/null, fractional, negative and over-limit values. A valid zero
scalar alone is not a durable reset hold.

## Verification and remaining boundary

Real Gin/SQL and fake HTTP Kubernetes tests cover operator denial under
false/unset rollout flags, exact plan/apply identity, immutable audit atomicity,
changed idempotency intent, receipt uncertainty, one GET-to-PUT mutation,
UID/resourceVersion/config drift, foreign targets, native failures and bound
consumer refusal. They do not prove live target ownership or production stop.

The required existing Unit Tests CI job runs
`scripts/prove-workload-target-postgres.py`. The fixed no-argument helper creates
only a disposable owned loopback PostgreSQL18 fixture with distinct owner/runtime
roles and actual `verify-full` TLS. Its native proof tag has no skip: missing
fixture configuration fails. It applies the fixed migration chain and exercises
immutable/RESTRICT/unique/payload constraints, runtime write denial, rollback
refusal, binding/audit atomicity, idempotency, concurrent row/advisory locks,
caller-cancellation lifetime, and grouped FK inserts while binding waits. No
caller-selected database or SQL is accepted. Cleanup verifies the exact owned
container labels and removes temporary fixture custody. Existing PostgreSQL16
runtime proof remains unchanged.

These locks serialize participating Enclii writers only. Autonomous Argo/HPA,
Pod recreation and external producers are not excluded by this adapter. There
is no simulator reset Job, durable stop/hold, owner custody transfer, provenance
adoption, reset/resume grant or portal reservation closure in this release.
Those require the separately reviewed enrollment/barrier/reset protocol. No
live binding, scale, migration or cutover is established by local or hosted tests.

The [Argo resource tracking specification](https://argo-cd.readthedocs.io/en/release-3.2/user-guide/resource_tracking/)
defines self-referencing tracking and installation identity; neither annotation
nor label alone supplies operator authority or immutable simulator provenance.

**Boundary checkpoint — 2026-10-13; owner: platform maintainer.** This public
runbook documents implementation and synthetic proof limits. Sensitive deployment
identity, custody and operational evidence remain in the private internal-devops /
labspace handoff lane. Policy: [public repository boundary](../PUBLIC_REPO_BOUNDARY.md).
