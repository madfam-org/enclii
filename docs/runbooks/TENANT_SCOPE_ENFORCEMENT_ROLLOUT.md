# Runbook: rolling out ADR-003 tenant-scope enforcement

> **Boundary checkpoint (2026-09-05, platform on-call):** Public-safe runbook —
> the enforcement's own behaviour, its environment variables and its verify
> steps, with every operator identity, tenant name, hostname and token value
> left as a `<placeholder>`. The allow-list's actual contents, the principals
> the dry-run report names, and the sequencing against live tenants belong to
> `internal-devops` (the 2026-08-05 control-plane review that raised R21).
> Policy: `docs/PUBLIC_REPO_BOUNDARY.md` (repo-boundary contract).

Decision: [`ADR-003 — platform_admin is strictly above tenant_admin`](../architecture/ADR_003_TENANT_ADMIN_SCOPE.md) (ruling R21).

This is a **behaviour-removing** deploy. Before it, any principal holding the
`admin` role reaches every project on the platform. After it, that principal
reaches its own tenants and nothing else, and only principals on an explicit
operator allow-list keep cross-tenant reach.

Nothing here can be done by an agent: every step needs production credentials
or a production database. Run them in order.

---

## What changes at deploy

| | Before | After |
| --- | --- | --- |
| `admin` role string | reaches every project | reaches its own tenants' projects, plus explicit grants |
| `superadmin` role string | reaches every project | same as `admin` — the string no longer elevates |
| API-token `--scopes admin` | mints a platform administrator | mints a tenant administrator |
| Cross-tenant reach | any admin | `users.is_platform_admin` only |
| `GET /v1/projects`, `GET /v1/deployments` | whole platform for an admin | the caller's tenants |
| `/v1/admin/tenants/*` (tenant switcher) | any admin | platform admins only |
| Refusal | n/a | `404`, matching this API's existing convention for a resource the caller may not see |

---

## 1. Before the deploy — set the allow-list

The platform rank is granted from an explicit list of email addresses and from
nothing else. No email domain, no pattern, no role string.

```bash
# On the switchyard-api deployment, alongside the existing ENCLII_ADMIN_EMAILS.
ENCLII_PLATFORM_ADMIN_EMAILS=<operator-1>,<operator-2>
```

If `ENCLII_PLATFORM_ADMIN_EMAILS` is unset, the API falls back to
`ENCLII_ADMIN_EMAILS`, which is where the estate's operators are named today.
That fallback exists so this deploy does not lock anyone out; set the narrow
variable explicitly and treat the fallback as temporary.

Each address must belong to a user that has **logged in at least once** — the
reconcile matches on `users.email`, and an address with no user row grants
nothing. Step 2 tells you whether that is the case.

Do not leave the list empty. An empty list means no principal on the platform
can perform a cross-tenant operation, including the tenant switcher.

## 2. Before the deploy — run the dry-run report

The report is read-only and does not depend on the enforcing build, so run it
against production as it stands.

```bash
curl -fsS -H "Authorization: Bearer $ENCLII_TOKEN" \
  "$ENCLII_API/v1/admin/tenant-scope/dry-run" | jq
```

Read three fields first:

- `platform_admin_allow_list_size` — must be non-zero.
- `platform_admins_in_database` — must equal
  the allow-list size. A shortfall means an allow-listed address has no user
  row, or holds no admin role; that operator will be refused cross-tenant calls
  after deploy.
- `principals_losing_reach` — the count that matters.

Then read `principals[]`. For every row with `projects_lost > 0`, answer one
question: **is this a tenant administrator that should never have had
cross-tenant access, or an operator missing from the allow-list?** Add the
operators to `ENCLII_PLATFORM_ADMIN_EMAILS` and re-run until every remaining
row is a tenant administrator you intend to scope down.

Watch for `projects_lost` on rows whose tenants are empty (`team_count: 0`).
Those principals reach projects today only through the rank, have no tenant to
fall back to, and will be left with their explicit `project_access` grants
alone. Either parent them to a team or grant them the projects explicitly,
before deploying.

Keep the JSON. It is the before-picture for step 5.

## 3. Deploy

Deploy the API as normal. On start it will:

1. run migration `039_platform_admin_rank` (adds `users.is_platform_admin`,
   defaulting to `false` for everyone — no existing `admin` is promoted);
2. reconcile the column against the allow-list, logging
   `granted_this_run` / `revoked_this_run` counts (never addresses);
3. log at ERROR if `ENCLII_TENANT_SCOPE_ENFORCE` is off.

Check the startup log for `Reconciled platform-admin rank from operator
allow-list` and confirm the counts match what step 2 predicted.

## 4. After the deploy — verify with a tenant-scoped principal

Verification needs a principal that is a tenant administrator of exactly one
tenant, and a resource in a different tenant. Do not verify with an operator
account; a platform admin passes every check and proves nothing.

```bash
# As tenant A's administrator, against tenant B's project slug.
curl -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $TENANT_A_ADMIN_TOKEN" \
  "$ENCLII_API/v1/projects/<tenant-b-project-slug>"          # expect 404

# The same principal, against its own tenant. Expect 200 even with no
# per-project grant — this is the check that catches an over-tight rollout.
curl -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $TENANT_A_ADMIN_TOKEN" \
  "$ENCLII_API/v1/projects/<tenant-a-project-slug>"          # expect 200

# The tenant switcher must refuse a tenant administrator outright.
curl -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $TENANT_A_ADMIN_TOKEN" \
  "$ENCLII_API/v1/admin/tenants"                             # expect 403

# And a platform admin must still reach both.
curl -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $PLATFORM_ADMIN_TOKEN" \
  "$ENCLII_API/v1/projects/<tenant-b-project-slug>"          # expect 200
```

A `200` on the first call means enforcement is not active: check
`ENCLII_TENANT_SCOPE_ENFORCE` and confirm the principal is not, in fact, on the
allow-list.

## 5. After the deploy — re-run the dry-run report

Re-run step 2's command. `enforcement_active` is now `true`, and the
`principals[]` rows are the after-picture: compare `projects_reachable_after`
against the JSON you kept. Any difference from the prediction is worth
understanding before you close the change.

---

## Rollback

`ENCLII_TENANT_SCOPE_ENFORCE=false` restores the pre-ADR-003 behaviour on the
next pod start.

**It is a rollback lever, not a mode.** With it off, every tenant administrator
is a platform administrator again — the defect ADR-003 records, in production,
with a second tenant possibly already onboarded. Each bypass it grants is
logged at WARN with the project id, so the window is auditable afterwards, and
the API logs at ERROR on every start while it is off.

A cluster running with enforcement off is an open P0. Use it to buy the minutes
until the deploy is reverted, and nothing longer.

Rolling the schema back as well (`039_platform_admin_rank.down.sql`) drops
`users.is_platform_admin`. Do that only together with reverting the API build:
the enforcing build with the column gone has no platform admins at all.

---

## Related

- [`ADR-003`](../architecture/ADR_003_TENANT_ADMIN_SCOPE.md) — the ruling.
- `apps/switchyard-api/internal/api/access.go` — the guard.
- `apps/switchyard-api/internal/api/tenant_scope_route_coverage_test.go` —
  the routes that do **not** reach the guard yet, each with a reason. Until
  that backlog is empty, the endpoints it names are still reachable
  cross-tenant by a tenant administrator holding the right role.
