# ADR-003: `platform_admin` is strictly above `tenant_admin`

> **Status**: Accepted — owner ruling, 2026-09-05
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

This ADR records the ruling. It does **not** implement it — see
[Consequences](#consequences).

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

## Consequences

- **The enforcement implementation is a follow-up wave item, not this PR.**
  This ADR is the record of the ruling. Splitting `auth.Role`, adding the
  target-side tenant comparison to every tenant-scoped handler, and the tests
  that prove a `tenant_admin` cannot reach another tenant, all land in their own
  changes. Nothing in this PR changes runtime behaviour.
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
