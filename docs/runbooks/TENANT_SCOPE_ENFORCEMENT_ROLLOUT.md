# Runbook: rolling out ADR-003 tenant-scope enforcement

> **Boundary checkpoint (2026-09-05, platform on-call):** Public-safe runbook —
> the enforcement's own behaviour, its environment variables and its verify
> steps, with every operator identity, tenant name, hostname and token value
> left as a `<placeholder>`. The allow-list's actual contents, the principals
> the dry-run report names, and the sequencing against live tenants belong to
> `internal-devops` (the 2026-08-05 control-plane review that raised R21).
> Policy: `docs/PUBLIC_REPO_BOUNDARY.md` (repo-boundary contract).

Decision: [`ADR-003 — platform_admin is strictly above tenant_admin`](../architecture/ADR_003_TENANT_ADMIN_SCOPE.md) (ruling R21).

This is a **behaviour-removing** change. Before it, any principal holding the
`admin` role reaches every project on the platform. After it, that principal
reaches its own tenants and nothing else, and only principals on an explicit
operator allow-list keep cross-tenant reach.

It ships in **three stages**, and the code deploy is only the first of them.
Merging to `main` deploys immediately, and the report you need in order to
decide anything is an endpoint in the build being deployed — so production
ships with `ENCLII_TENANT_SCOPE_ENFORCE=false` already set
(`infra/k8s/components/environment/production.env:61`,
`infra/k8s/production/environment-patch.yaml:82`), overriding the code default
of on.

| Stage | State | What changes for callers |
| --- | --- | --- |
| 1 | flag `false`, code deployed | **nothing** — identical to pre-ADR-003 |
| 2 | operator runs the report, sets the allow-list | nothing |
| 3 | one-line PR flips the flag to `true` | the table below |

**Stage 1 is not done.** While the flag reads false every tenant administrator
can reach every tenant, which is the defect this work exists to remove. The API
logs at ERROR on every boot in that state. Treat a cluster parked in stage 1 as
an open item with an owner, not as a completed rollout.

Nothing here can be done by an agent: every step needs production credentials
or a production database. Run them in order.

---

## What changes at stage 3

| | Before | After |
| --- | --- | --- |
| `admin` role string | reaches every project | reaches its own tenants' projects, plus explicit grants |
| `superadmin` role string | reaches every project | same as `admin` — the string no longer elevates |
| API-token `--scopes admin` | mints a platform administrator | mints a tenant administrator |
| Cross-tenant reach | any admin | `users.is_platform_admin` only |
| `GET /v1/projects`, `GET /v1/deployments` | whole platform for an admin | the caller's tenants |
| `/v1/admin/tenants/*` (tenant switcher) | any admin | platform admins only |
| the rest of `/v1/admin/*` | any admin | platform admins only (one exception, table below) |
| `/v1/secrets/intake/*` | any admin | platform admins only |
| the 23 routes in the R21 backlog | no target-side check at all | the tenant comparison at the target |
| `GET /v1/builds/:commit_sha/status` (unauthenticated) | every service that built the sha | only services the caller may reach — **`[]` for an anonymous caller** |
| Refusal | n/a | `404`, matching this API's existing convention for a resource the caller may not see |

### The 23 routes that had no check at all

R21 PR 2 (#504) switched these onto the guard. Until stage 3 they behave exactly as
they do today; at stage 3 a tenant administrator is refused `404` on any of
them that addresses another tenant's resource.

`DELETE /services/:id` · `POST /services/:id/exec` · `POST /services/:id/restart` ·
`POST /services/:id/scale` · `POST /services/:id/migrate` ·
`GET /services/:id/health/detailed` · `GET /services/:id/networking` ·
`GET /services/:id/previews` · `GET /services/:id/builds/:build_id/status` ·
`POST /services/:id/domains` · `PATCH /services/:id/domains/:domain_id` ·
`DELETE /services/:id/domains/:domain_id` ·
`POST /services/:id/domains/:domain_id/verify` · `POST /domains/:domain_id/sync` ·
`DELETE /cron-jobs/:id` · `GET /cron-jobs/:id/runs` · `GET /exports/:export_id` ·
`DELETE /exports/:export_id` · `POST /exports/:export_id/approve` ·
`GET /templates/deployments/:id` ·
`POST /previews/:id/comments/:comment_id/resolve`

Two took a different shape:

- `GET /v1/secrets/intake/:id` is parented to no project — an intake id names a
  Vault path and a namespace — so it is gated on the **rank**, together with
  the other two `/v1/secrets/intake/*` routes.
- `GET /v1/builds/:commit_sha/status` is keyed by a git sha, so it **filters**
  its rows instead of refusing the request. It is unauthenticated, so at stage
  3 an anonymous caller gets `services: []`. **If anything outside Enclii polls
  this endpoint without a credential, it breaks at stage 3.** Check that before
  flipping the flag.

### `/v1/admin/*` — the judgement, route by route

Every route below was gated on the `admin` role, which post-ADR-003 means
`tenant_admin`. The question asked of each was: does this address the estate,
or a tenant's own resource?

| Route family | Decision | Why |
| --- | --- | --- |
| `GET/POST /admin/fleet`, `GET/PUT/POST /admin/fleet/:id/*` | platform-only | Physical hosts. Firmware, partitioning, secure wipe and power are estate operations; no tenant owns a host. |
| `GET/POST/PUT/DELETE /admin/clusters[/:id]` | platform-only | Cluster registration and deregistration. Not a tenant-owned resource. |
| `GET/POST/PUT/DELETE /admin/resources[/:id]` | platform-only | Crossplane compositions and their policy. Infrastructure composition, not tenant workload. |
| `GET/POST/DELETE /admin/vclusters[/:id]`, `/kubeconfig` | platform-only | Provisioning and tearing down virtual clusters, and handing out their kubeconfig. |
| `GET/POST/DELETE /admin/propagation[/:id]` | platform-only | Propagation policy across clusters. |
| `GET /admin/drift[/:id]`, `POST /admin/drift/:id/resolve` | platform-only | Governance over the estate's declared state. |
| `GET /admin/costs`, `/costs/summary`, `POST /admin/costs/allocate` | platform-only | `GET /admin/costs` takes an arbitrary `?tenant_id=` and the summary aggregates every tenant — cross-tenant by construction. Tenant-visible spend is `/v1/usage/costs`, which is unchanged. |
| `/admin/tenants*`, `/admin/tenant-scope/dry-run` | platform-only | Already moved in PR #499; they are the cross-tenant surface. |
| `POST /admin/onboard*`, `GET /admin/onboard[/:owner/:repo]`, `GET /admin/preflight` | platform-only | Onboarding registers a repo into the platform and lists every registration; the listing is estate-wide. |
| `GET /admin/db/schema` | platform-only | The control plane's own schema and migration state. |
| `POST /admin/databases/discover` | platform-only | Cluster-wide discovery and backfill of database rows. |
| `POST /admin/provision/postgres\|secrets\|r2` | platform-only | Writes into the shared Postgres, Vault and R2 as the platform, not as a tenant. |
| `GET /admin/provision/pgbouncer/reconcile` | platform-only | Reads PgBouncer's userlist against Postgres login roles. |
| `GET /admin/status/services`, `POST /admin/status/regenerate` | platform-only | The public status page's configuration. |
| `GET /admin/topology` | platform-only | Topology across every project on the platform. |
| `GET /admin/providers/catalog` | platform-only | Provider capability catalog for the estate. |
| `GET /admin/discovered-orphans` | platform-only | Namespaces in the cluster with no matching service row — enumerates across tenants. |
| **`POST /admin/projects/:slug/reconcile-services`** | **tenant-visible** | Addressed by a project slug and repairs that project's own service rows. A tenant administrator repairing its own project is the intended user; the tenant guard (`RequireProjectAccessBySlug`) keeps it out of anybody else's. |

`/v1/ops/*` and `/v1/providers/*` were looked at and deliberately left on the
`admin` role in this pass: they are platform surfaces by the same reasoning,
but they are not under `/v1/admin/*`, they carry no tenant-owned identifier, and
moving them belongs with a review of the operator contract rather than with
this one. Record it as an open item, not as a cleared route.

---

## Stage 1 — deploy (behaviour unchanged)

Deploy the API as normal, with `ENCLII_TENANT_SCOPE_ENFORCE=false` as committed.
On start it will:

1. run migration `039_platform_admin_rank` (adds `users.is_platform_admin`,
   defaulting to `false` for everyone — no existing `admin` is promoted);
2. reconcile that column against the allow-list, logging
   `granted_this_run` / `revoked_this_run` counts (never addresses);
3. log at ERROR that enforcement is off.

**Neither the migration nor the reconcile consults the flag.** That is
deliberate and it is what makes stage 2 meaningful: `users.is_platform_admin`
is already populated, so the report's answers are the answers that will apply
when the flag flips, not a projection.

Confirm the log line `Reconciled platform-admin rank from operator allow-list`
before continuing.

## Stage 2, step 1 — set the allow-list

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
nothing. The report's `platform_admins_in_database` tells you whether that is the case.

Do not leave the list empty. An empty list means no principal on the platform
can perform a cross-tenant operation, including the tenant switcher.

## Stage 2, step 2 — run the dry-run report

The report is read-only and does not depend on the flag being on, so run it
against production sitting in stage 1. With enforcement off, the reporting and
tenant-switcher routes gate on the `admin` role exactly as they did before this
change, so an existing admin credential reaches them; once the flag flips they
require the platform rank.

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
row is a tenant administrator you intend to scope down. Each change to the
variable needs a pod restart before the reconcile picks it up.

Watch for `projects_lost` on rows whose tenants are empty (`team_count: 0`).
Those principals reach projects today only through the rank, have no tenant to
fall back to, and will be left with their explicit `project_access` grants
alone. Either parent them to a team or grant them the projects explicitly,
before deploying.

Keep the JSON. It is the before-picture for the stage 3 comparison.

## Stage 3 — flip the flag

Open a one-line PR removing `ENCLII_TENANT_SCOPE_ENFORCE=false` from
`infra/k8s/components/environment/production.env` and
`infra/k8s/production/environment-patch.yaml` (or setting it to `"true"`;
removing it is better, since the code default is on and one fewer override is
one fewer thing to explain later). Refresh the golden snapshot with
`scripts/update-golden.sh`. Deploy.

Confirm the ERROR line about enforcement being off is **gone** from the startup
log. Then verify.

## Stage 3, verification — use a tenant-scoped principal

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

# R21 PR 2 — the routes that had no target-side check before. A service in
# tenant B, addressed by its id, from tenant A's administrator.
curl -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $TENANT_A_ADMIN_TOKEN" \
  "$ENCLII_API/v1/services/<tenant-b-service-id>/networking"  # expect 404

curl -o /dev/null -w '%{http_code}\n' -X POST \
  -H "Authorization: Bearer $TENANT_A_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"replicas":1}' \
  "$ENCLII_API/v1/services/<tenant-b-service-id>/scale"       # expect 404

# The rest of the admin subtree is platform-only now, not just the switcher.
curl -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $TENANT_A_ADMIN_TOKEN" \
  "$ENCLII_API/v1/admin/fleet"                                # expect 403

# ...but a tenant administrator keeps the one tenant-visible admin route,
# against its OWN project.
curl -o /dev/null -w '%{http_code}\n' -X POST \
  -H "Authorization: Bearer $TENANT_A_ADMIN_TOKEN" \
  "$ENCLII_API/v1/admin/projects/<tenant-a-project-slug>/reconcile-services"
                                                              # expect 2xx

# The commit-keyed build status, with NO credential. This is the one answer
# that changes for a caller outside Enclii.
curl -fsS "$ENCLII_API/v1/builds/<a-known-sha>/status" | jq '.services | length'
                                                              # expect 0
```

**Before you flip the flag**, confirm nothing outside Enclii polls
`GET /v1/builds/:commit_sha/status` without a credential. It is an
unauthenticated route today and it answers with every service that built the
sha; at stage 3 it answers with only the services the caller can reach, which
for an anonymous caller is none. If something does poll it, give that consumer
a token before stage 3 rather than after.

A `200` on the first call means enforcement is not active: check
`ENCLII_TENANT_SCOPE_ENFORCE` and confirm the principal is not, in fact, on the
allow-list.

## Stage 3, after — re-run the dry-run report

Re-run the stage 2 command. `enforcement_active` is now `true`, and the
`principals[]` rows are the after-picture: compare `projects_reachable_after`
against the JSON you kept. Any difference from the prediction is worth
understanding before you close the change.

---

## Console and CLI — affordances, not gates

ADR-003 §5: neither is an enforcement point, and neither is treated as one.
The API refuses the call regardless of what either of them shows.

**Dispatch (`apps/admin-console`).** It drives `/v1/admin/*`, which is now
platform-only. The console cannot mirror the API's gate, because the rank is a
column in the API's database and deliberately not a role string. What it does
instead: if the verified token *states* `is_platform_admin: false`, the console
denies — there is no reason to hand the portal to a principal every call will
refuse. If the token says nothing about the rank, which is what Janua issues
today, the legacy role list still admits and the console logs that it is doing
so. It does **not** fail closed on a missing claim; that would lock every
operator out of the console on the day this deploys, in service of a check
that is not the enforcement point.

If you want the console narrowed at stage 3, the levers are the existing
`ALLOWED_ADMIN_ROLES` (and the domain list), or teaching Janua to mint the
`is_platform_admin` claim. Neither is required for the API to be correct.

**CLI (`enclii admin ...`).** `enclii admin --help` now says the tree needs the
platform rank, and a `403` from a platform-gated path is explained as a rank
problem naming `ENCLII_PLATFORM_ADMIN_EMAILS`, rather than as "your account
lacks permission" — which post-ADR-003 sends an operator hunting for a role
that no longer grants anything. A `403` on
`/v1/admin/projects/:slug/reconcile-services` is deliberately *not* explained
that way: that route is tenant-gated, so a refusal there means something else.

---

## Rollback

`ENCLII_TENANT_SCOPE_ENFORCE=false` restores the pre-ADR-003 behaviour on the
next pod start — the same lever stage 1 ships, used in the other direction.

**It is a rollback lever, not a mode.** With it off, every tenant administrator
is a platform administrator again — the defect ADR-003 records, in production,
with a second tenant possibly already onboarded. Each bypass it grants is
logged at WARN with the project id, so the window is auditable afterwards, and
the API logs at ERROR on every start while it is off.

A cluster running with enforcement off **after stage 3** is an open P0. Use it
to buy the minutes until the deploy is reverted, and nothing longer. (Before
stage 3, off is the shipped state — see the stage table at the top — but it is
still an open item, not a finished rollout.)

Rolling the schema back as well (`039_platform_admin_rank.down.sql`) drops
`users.is_platform_admin`. Do that only together with reverting the API build:
the enforcing build with the column gone has no platform admins at all.

---

## Related

- [`ADR-003`](../architecture/ADR_003_TENANT_ADMIN_SCOPE.md) — the ruling.
- `apps/switchyard-api/internal/api/access.go` — the guard.
- `apps/switchyard-api/internal/api/access_staged.go` — the gates R21 PR 2
  added, and why they are inert while the flag is off.
- `apps/switchyard-api/internal/api/tenant_scope_route_coverage_test.go` —
  the derivation that finds a tenant-owned route which does not reach the
  guard. Its backlog is now **empty**, and `TestTenantScope_BacklogIsEmpty`
  keeps it that way; the derivation stays as the tripwire for the next route
  somebody adds.
