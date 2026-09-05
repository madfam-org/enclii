# ADR-003: `platform_admin` is strictly above `tenant_admin`

> **Status**: Accepted — enforcement landed PR #499 and PR #504 (2026-09-05)
> **Decision id**: `decision.tenant-admin-scope`
> **Date**: 2026-09-05
> **Authors**: Platform / control-plane
> **Supersedes**: None
> **Related**: `apps/switchyard-api/internal/auth/rbac.go` (the role enum this
> ADR splits), `docs/architecture/tenant-export.md`,
> `docs/architecture/managed-db-addon.md` (per-tenant resources the rule covers)

---

## Summary

**Decision: `platform_admin` and `tenant_admin` are two ranks, not one, and
`platform_admin` is strictly above `tenant_admin`. A tenant admin can never
touch another tenant. The rule is enforced at the target of every call, not in
the UI.**

This ADR recorded the ruling. The enforcement has since landed — see
[Enforcement](#enforcement) for what is enforced, where, and what remains.

---

## Context

A single `admin` rank that is used both for "administers this tenant" and for
"administers the platform" is not two permissions that happen to share a name.
It is one permission, and every route guarded by it is cross-tenant by
construction: a rank comparison (`is the caller's role >= admin?`) answers a
question about *seniority* and says nothing about *which tenant* the caller is
senior in. The tenant id in the request path is then used as the target of the
write, unchecked.

That shape matters as soon as a second tenant exists, because provisioning a
tenant necessarily mints an administrator for it. Every customer admin is then
a platform admin in all but name, able to read, modify, suspend and delete
every other customer's resources. A review of the estate's control planes on
2026-08-05 found exactly this conflation and raised it to P0; the underlying
review, the affected code paths and the remediation sequencing are tracked in
`internal-devops` because they concern live systems.

Enclii carries the same shape today: `auth.Role` is `admin > developer >
viewer`, with no rank above `admin` and no tenant comparison in the rank check.
Nothing about Enclii's current single-tenant operation makes that safe — it
makes it *unobserved*. The moment Enclii Depot or Publica onboards a second
tenant, the defect becomes a live cross-tenant write.

## Decision

1. **Two ranks.** `platform_admin` is strictly above `tenant_admin`. There is
   no rank at which the two are the same principal and no rank above
   `platform_admin`.
2. **A tenant admin is scoped to exactly one tenant.** A `tenant_admin` can
   never read, mutate or delete another tenant's resources — not its settings,
   not its projects, services, deployments, addons, secrets, domains, exports
   or billing state, and not the tenant object itself. There is no operation
   for which "admin of tenant A acting on tenant B" is a legitimate answer.
3. **`platform_admin` is the only cross-tenant role.** Cross-tenant reads and
   writes, including acting-as and support access, are reachable only from that
   rank, and they remain auditable.
4. **Enforcement lives at the target, on every call.** Every handler that
   accepts a tenant-scoped identifier compares the caller's tenant against the
   tenant that owns the target resource, and refuses when they differ and the
   caller is not `platform_admin`. Enforcement is per call, not per session,
   not per route table, and not once at the edge.
5. **The UI is not an enforcement point.** Hiding a control, omitting a route
   from a navigation tree, or filtering a list client-side does not implement
   any part of this rule. Those are affordances; the API must refuse the call
   even when it is made directly.

## Enforcement

Landed in PR #499. The shape, for a reader who needs to know what is true
today rather than what was ruled:

- **The rank split is a database column, not a role string.**
  `users.is_platform_admin` (migration `039_platform_admin_rank`) is the only
  thing that grants cross-tenant reach. `admin`, `superadmin` and even a
  literal `platform_admin` presented in a claim all normalize to
  `tenant_admin` (`auth.NormalizeRole`). This is not belt-and-braces: an API
  token's `scopes` list is copied verbatim into the caller's roles, so a rank
  assertable by string would be a rank a tenant admin could mint for itself.
- **Existing `admin` principals were not promoted**, exactly as this ADR
  requires. The column defaults to `false` and is reconciled at each API start
  against an explicit operator allow-list (`ENCLII_PLATFORM_ADMIN_EMAILS`,
  falling back to the existing `ENCLII_ADMIN_EMAILS`). No email domain, no
  pattern, and nothing named in this public repository.
- **The comparison happens at the target.**
  `Handler.enforceUserProjectAccess` resolves the resource's owning tenant and
  compares it with the caller's, on every call. Every `loadXWithAccess` helper
  routes through it, so the resource kind does not matter.
- **List endpoints filter.** `GET /v1/projects` and `GET /v1/deployments`
  return the caller's tenants, not the table; the unfiltered view is reachable
  from the platform rank alone.
- **Refusal is `404`**, following this API's existing convention for a resource
  the caller may not see. A `403` on another tenant's project slug would itself
  confirm that project exists.
- **Rollback is `ENCLII_TENANT_SCOPE_ENFORCE=false`**, default on in code
  (unset = enforce). It restores the pre-ADR-003 bypass in full, including the
  defect. See [the rollout runbook](../runbooks/TENANT_SCOPE_ENFORCEMENT_ROLLOUT.md).

### The rollout is staged, and production starts with the flag off

Merging to `main` deploys immediately, so the enforcing build reaches
production before any operator can run the pre-deploy report against it —
the report is an endpoint in that build. Production therefore ships the flag
explicitly off (`infra/k8s/components/environment/production.env`,
`infra/k8s/production/environment-patch.yaml`), overriding the code default:

1. **Stage 1** — deploy with `ENCLII_TENANT_SCOPE_ENFORCE=false`. Behaviour is
   identical to pre-ADR-003 `main`. The migration runs and the startup
   reconcile still populates `users.is_platform_admin` from the allow-list,
   because neither consults the flag — that is what makes the next stage's
   numbers real.
2. **Stage 2** — the operator runs `GET /v1/admin/tenant-scope/dry-run`,
   resolves every principal it reports losing reach, and sets
   `ENCLII_PLATFORM_ADMIN_EMAILS`.
3. **Stage 3** — a one-line change flips the flag to `true`.

**Stage 3 is the point of the work, and stages 1-2 are not a resting place.**
While the flag reads false, every tenant admin reaches every tenant. The API
logs at ERROR on each boot in that state, and each bypass it grants is logged
at WARN with the project id.

Because the report and the tenant switcher are themselves platform-only
routes, the flag covers that gate too: with it off they gate on the
tenant-admin rank, exactly as they did before this change. A hard rank gate
there would have put the only key to the rollout behind the lock it opens.

### Every tenant-owned route reaches the guard

`apps/switchyard-api/internal/api/tenant_scope_route_coverage_test.go` derives,
from the source on every run, the set of tenant-owned routes that never reach
the guard, and fails when one appears that is not named in its backlog.

**That backlog is empty.** PR #499 left 23 entries in it — a group of
service-addressed verbs (`DELETE /services/:id`,
`POST /services/:id/exec|scale|restart|migrate`, the domain verbs under a
service) and a group of resources addressed by their own id outside any
`/projects/:slug` group (cron jobs, tenant exports, template deployments,
secret-intake status). PR #504 switched all 23 onto the guard at the target and
deleted their entries; `TestTenantScope_BacklogIsEmpty` asserts the map is
empty, and the derivation test remains as the tripwire for the next route
somebody adds.

Three of the 23 did not take the same shape as the rest, and are recorded here
because each is a judgement rather than a mechanical edit:

- **`GET /v1/builds/:commit_sha/status`** is keyed by a git sha, so several
  services in several tenants answer it at once. It **filters** rather than
  refuses: rows the caller cannot reach are dropped from a `200`. The route is
  unauthenticated, so an anonymous caller now reaches no service at all.
- **`GET /v1/secrets/intake/:id`** addresses a Vault path and a namespace in
  the platform's own secret plumbing, parented to no project. There is nothing
  for a tenant comparison to compare, so the correct gate is the rank:
  `/v1/secrets/intake/*` is platform-only.
- **The tenant-export verbs** already refused a caller that is not a project
  admin, but that check opens with a role-string test that waves a tenant
  administrator past it — the same rank-comparison defect, one layer down. The
  guard now runs at the handler, before the service is called at all. The
  service's own string test is a smaller, still-open item: it no longer grants
  cross-tenant reach, but it still lets a tenant admin bypass the project-admin
  requirement *inside its own tenant*.

### `/v1/admin/*` is platform-only

PR #499 moved only the tenant switcher and the dry-run report to
`RequirePlatformAdmin`, and left the rest of the subtree gated on the `admin`
role — i.e. on `tenant_admin` — with the judgement per route explicitly
deferred. PR #504 made it: every `/v1/admin/*` route is platform-only, except
`POST /v1/admin/projects/:slug/reconcile-services`, which is addressed by a
project slug, belongs to the tenant that owns it, and stays on the
tenant-scoped guard. The per-route table is in
[the rollout runbook](../runbooks/TENANT_SCOPE_ENFORCEMENT_ROLLOUT.md).

### What is still not enforced in production

**Production is in stage 1.** Nothing in this section is refusing anything
there yet, and that is the only remaining gap — but it is the one that matters.

The gates PR #504 added are inert while `ENCLII_TENANT_SCOPE_ENFORCE=false`,
deliberately and by a different mechanism than PR #499's. The routes PR #499
touched already called the guard, so for them "the flag off" and "the guard
minus the tenant comparison" are the same thing. These 23 performed no
target-side check at all, so a gate that merely restored the rank bypass would
still be a new refusal for every caller below the admin rank — a developer with
no per-project grant, or the anonymous caller of the commit-status route. The
flag therefore stands those gates down entirely, and stage 1 remains
byte-for-byte pre-ADR-003 `main` on all of them.

**Tenant #2 is now gated on stage 3 alone.** The backlog condition is
discharged; the rollout condition is not.

## Consequences

- **It gates tenant #2.** No second Enclii Depot or Publica tenant is onboarded
  before the enforcement lands, because onboarding one is what turns the
  conflation into a live cross-tenant write. The route backlog condition is
  discharged (PR #504); the remaining condition is the rollout reaching stage
  3 in production.
- **Tests, not review, are the evidence.** The follow-up is complete when a
  `tenant_admin` of tenant A is refused, at the API, on every tenant-scoped
  verb against tenant B — and a test says so for each verb. A route inventory
  is not evidence; the next route added would not be on it.
- **Rank comparison alone is now a defect.** Any handler that authorises a
  tenant-scoped action by comparing role rank without comparing tenants is a
  bug against this ADR, whether or not it is reachable today.
- **Existing `admin` principals map to `tenant_admin`,** not to
  `platform_admin`. The higher rank is granted deliberately and separately;
  a migration that promotes every current `admin` would re-create the defect
  the split exists to remove.
