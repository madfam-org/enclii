# ADR-003: `platform_admin` is strictly above `tenant_admin`

> **Status**: Accepted — enforcement landed PR #499 (2026-09-05)
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
[Enforcement](#enforcement) for what is enforced, where, and what is not yet.

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

### Not yet enforced

Two things, and they compound: production is in stage 1 above, so nothing in
this section is enforced there yet.

`apps/switchyard-api/internal/api/tenant_scope_route_coverage_test.go` derives,
from the source on every run, the set of tenant-owned routes that never reach
the guard, and fails when one appears that is not named in its backlog. That
backlog is currently non-empty: a group of service-addressed verbs
(`DELETE /services/:id`, `POST /services/:id/exec|scale|restart|migrate`, the
domain verbs under a service) and a group of resources addressed by their own
id outside any `/projects/:slug` group (cron jobs, tenant exports, template
deployments, secret-intake status). Each is listed with its reason. **Tenant #2
is gated on that backlog being empty, not merely on this ADR's status line** —
the ADR's own test is that a tenant admin is refused on every tenant-scoped
verb, and these verbs are not refused yet.

## Consequences

- **It gates tenant #2.** No second Enclii Depot or Publica tenant is onboarded
  before the enforcement lands, because onboarding one is what turns the
  conflation into a live cross-tenant write.
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
