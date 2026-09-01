# RFC: CLIENT-IN-A-DAY — one manifest that stands up a whole client

> **Status**: Draft (design + validating skeleton only; no execution wired)
> **Date**: 2026-09-01
> **Authors**: Platform / control-plane
> **Scope**: `enclii tenant apply` — a cross-platform tenant manifest and its orchestration contract
> **Related**: `docs/cli/commands/onboard.md` (the single-app precedent this generalizes),
> `packages/sdk-go/pkg/types/ecosystem_app.go` (the existing MADFAM AppSpec contract),
> `docs/runbooks/ENCLII_JUNCTION_ROUTE_RECONCILIATION_2026-05-16.md`
> **Blocked on**: two sibling-platform seams (§6). This RFC ships the schema, the plan,
> and the validator. It deliberately ships **no** execution.

---

## Summary

Standing up a new client today is a multi-hour, multi-repo, multi-credential ritual
performed by hand. For Crea Tu Mundo (CTM) it spanned five systems, three credential
domains, at least one UUID copy-pasted between two scripts, and one raw SQL insert.
Every step is individually documented; none of them are declared together, and nothing
checks that the parts agree.

**Decision proposed: one YAML manifest (`kind: Tenant`) declares a whole client —
apps, database, secrets, domains, the Janua org, the Nauta workspace, and the Kalya
tenant — and `enclii tenant apply` reconciles it in a fixed, idempotent order.**

This document specifies the manifest, the orchestration order, the idempotency
contract, and the failure doctrine. It names the two seams that do not exist yet and
must be built in sibling platforms before execution can land. Appendix A records what
we actually ran by hand for CTM; that trail is the requirements document, and every
schema field below traces back to a line in it.

---

## Context

### What "a client" actually consists of

The CTM onboarding is the only complete instance we have. Reconstructed from the trail
in Appendix A, a client is:

1. **A Janua organization** — the identity root. Its slug is the client's canonical
   name across the ecosystem; its UUID is the tenant id that other platforms key on.
2. **Janua OAuth clients** — one per app, plus one per non-production environment
   whose redirect URI must not be added to the production client.
3. **Product entitlements** — `organizations.product_tiers` JSONB, one key per product.
4. **Enclii runtime** — a project, a namespace, one service per app per environment,
   managed Postgres, K8s secrets, R2 buckets, Cloudflare tunnel routes + DNS + TLS.
5. **A Nauta workspace** — the vCTO engagement home, bound to the Janua org by
   `januaOrganizationId`, with its own hostnames and commercial tier.
6. **A Kalya tenant** — the booking/calendar substrate, whose `Tenant.id` *is* the
   Janua org UUID, plus its hosts, schedules and event types.
7. **An Enclii team (tenant)** — so the client can eventually self-serve, and so the
   master admin can act-as them in the meantime (migration 038, enclii#474).

Seven things, in four repos, behind three different kinds of credential.

### Why the pieces cannot simply be scripted in sequence today

The three provisioners we already have disagree about nearly everything that matters
to an orchestrator:

| | Janua org | Kalya tenant | Nauta workspace |
|---|---|---|---|
| Interface | HTTP `POST /api/v1/admin/organizations` | `npm run provision:tenant` (direct Postgres) | tRPC `workspace.create` |
| Credential | platform-admin **user JWT** | `DATABASE_URL` | cockpit host + `nauta:platform-admin` role |
| Re-run behavior | 400 on duplicate slug | **converges** (find-then-create-or-update) | **throws** (`create`, no upsert) |
| Janua binding | n/a (is the source) | `Tenant.id` == org UUID, **immutable** | `januaOrganizationId`, nullable-unique, updatable |

An orchestrator that assumes any one of these shapes breaks on the other two. The
manifest therefore has to be honest about the asymmetry rather than paper over it, and
the CLI has to own convergence for the steps whose backends do not provide it.

### The outage class this design is built around

Domain declarations are load-bearing, and under-declaring them has already caused an
outage. On 2026-08-27, `janua/enclii.yaml` — a legacy single `Service` document naming
eight identity hostnames — was captured by manifest provisioning, and every identity
hostname was rewritten to a backend that had never existed (fixed in enclii#468:
capture now resolves `metadata.name` against live workloads and refuses rather than
guessing). Earlier, `tunnels-apply --project` reconciled *all* junctions in a project,
so a partial declaration silently deleted the routes it did not mention.

The lesson generalizes past domains: **a manifest that is read as complete desired
state will delete whatever it fails to mention.** A tenant manifest names far more
resources than a service manifest, so it is a far bigger version of the same gun. §5
resolves this by making `tenant apply` strictly additive-and-converging, never
authoritative-deleting, for every resource class.

---

## The manifest

`apiVersion: enclii.dev/v1alpha`, `kind: Tenant`. One document, one client, all
environments.

```yaml
apiVersion: enclii.dev/v1alpha
kind: Tenant
metadata:
  # The canonical client slug. THE joining key across every platform: the Janua
  # org slug, the Nauta workspace slug, the Kalya tenant slug, and the Enclii
  # team slug are all this value. 63 chars max — Nauta's VarChar(63) is the
  # binding floor (Kalya allows 120, Janua 100).
  name: crea
  displayName: Crea Tu Mundo Autismo
  # Free-text; recorded in the plan output and in audit, never parsed.
  description: Cliente vCTO — autismo, CDMX

spec:
  # ---------------------------------------------------------------------------
  # IDENTITY — Janua. Created FIRST; everything else keys on its org UUID.
  # ---------------------------------------------------------------------------
  janua:
    org:
      slug: crea                    # defaults to metadata.name
      name: Crea Tu Mundo Autismo   # defaults to metadata.displayName
      # The owner MUST already exist as a Janua user. Janua resolves the owner
      # before any write specifically so a bad owner cannot orphan a half-made
      # org; we surface the same failure at plan time where we can.
      ownerEmail: owner@example.org
      billingEmail: facturacion@example.org
    # Product entitlements → organizations.product_tiers JSONB.
    # Values: essentials | pro | madfam. Absent product = community/self-hosted,
    # which is NOT the same as unentitled-and-broken.
    tiers:
      enclii: pro
      nauta: pro
      kalya: essentials
    # One OAuth client per app-environment that needs its own redirect URI.
    # A dedicated staging client is the norm, not an exception: adding a staging
    # redirect to the production client is how a staging login bounces into prod.
    oauthClients:
      - logicalKey: crea-map
        audience: crea-map
        redirectURIs: ["https://crea-map.example.mx/api/auth/callback"]
      - logicalKey: crea-map-staging
        audience: crea-map
        redirectURIs: ["https://staging-map.example.mx/api/auth/callback"]

  # ---------------------------------------------------------------------------
  # RUNTIME — Enclii. One project; one service per app per environment.
  # ---------------------------------------------------------------------------
  project: crea                     # Enclii project + team slug
  namespace: crea                   # defaults to project

  apps:
    - name: crea-map
      repo: madfam-org/crea-map
      # The app's own enclii.yaml stays the authority for build/runtime/probes.
      # This manifest references it; it does NOT restate it. Two sources of truth
      # for a port number is a bug generator.
      manifest: enclii.yaml
      environments:
        - name: production
          # Explicit image digest, or omit to track the app manifest's autoDeploy.
          autoDeploy: true
          domains:
            # FLAT LABELS ONLY under a Cloudflare Universal SSL apex: one label
            # below the apex is covered, nested is not. Validated (§4).
            - host: crea-map.example.mx
              tls: true
          envFrom:
            - secret: crea-map-secrets
          env:
            APP_ORIGIN: https://crea-map.example.mx
            JANUA_AUDIENCE: crea-map
            JANUA_CLIENT_ID: crea-map
        - name: staging
          autoDeploy: false
          domains:
            - host: staging-map.example.mx
              tls: true
          env:
            APP_ORIGIN: https://staging-map.example.mx
            JANUA_CLIENT_ID: crea-map-staging
            APP_ENVIRONMENT_LABEL: STAGING

  # ---------------------------------------------------------------------------
  # DATA — managed Postgres. `clones` exist because cloning an existing database
  # on the same instance under the SAME owner adds no DB role, and therefore
  # requires no hand-edit of the static pgbouncer userlist — the 2026-08-24
  # outage class (a botched userlist edit drops users and pooled auth fails
  # cluster-wide while direct still works).
  # ---------------------------------------------------------------------------
  db:
    name: crea_map
    extensions: [pgcrypto]
    rls: true                       # advisory: recorded, asserted, never applied here
    clones:
      - name: crea_map_staging
        from: crea_map

  # Secret KEYS only. Values never appear in a manifest and never appear in plan
  # output. Provisioned out-of-band via `enclii admin provision secrets`; this
  # block declares the CONTRACT so a missing key is a loud plan failure rather
  # than a 3am discovery.
  secrets:
    - name: crea-map-secrets
      keys:
        - DATABASE_URL
        - DIRECT_DATABASE_URL
        - JANUA_CLIENT_SECRET
        - JANUA_INTERNAL_API_KEY
        - R2_ACCESS_KEY_ID
        - R2_SECRET_ACCESS_KEY

  buckets:
    - name: crea-map-uploads
      provider: r2

  # ---------------------------------------------------------------------------
  # SIBLING PLATFORMS — declared here, executed by their own owners (§6).
  # ---------------------------------------------------------------------------
  nauta:
    workspace:
      tier: FRACTIONAL_CTO          # SELF_SERVE | PROJECT | FRACTIONAL_CTO
      locale: es-MX
      currency: MXN
      timezone: America/Mexico_City
      hostnames:
        - host: crea.example.mx
          primary: true

  kalya:
    # A REFERENCE, not an inlined copy. The CTM tenant file is ~32KB of clinical
    # scheduling detail (22 hosts, 31 availability rules, 61 blocks) that belongs
    # under Kalya's own schema and review. Inlining it would fork the schema.
    tenantFile: ../kalya/prisma/provision/ctm-tenant.json
    # Kalya's Tenant.id IS the Janua org UUID and is IMMUTABLE after creation.
    # We therefore never write it into the manifest by hand: `tenant apply`
    # resolves it from step 1 and passes it through. Declaring it here would be a
    # second source of truth for a value that throws on mismatch.
```

### Field-by-field notes that are not obvious from the shape

- **`metadata.name` is the joining key, deliberately.** Four platforms independently
  key on a client slug. Letting them diverge is how you get a Nauta workspace that
  cannot find its Janua org. One field, propagated, validated for the intersection of
  all four platforms' constraints.
- **`apps[].manifest` references rather than restates.** The app's `enclii.yaml`
  already declares build type, ports, probes, resources, SLOs and network policy, and
  it is reviewed in the app's own repo by the people who own it. This manifest declares
  only what is *tenant-scoped*: which environments exist, which domains they answer on,
  and which secrets they draw from.
- **`db.rls` is advisory.** Enclii provisions the database and the role; whether row
  level security is enabled inside it is the app's migration's business. The field
  exists so the intent is recorded and can be asserted, not so `tenant apply` runs DDL
  it does not own.
- **`secrets[].keys` carries no values.** The manifest is committed to a public-boundary
  repo. Values move through `enclii admin provision secrets --secrets-file`, which
  already refuses to echo them.

---

## Orchestration order and the idempotency contract

### The order, and why it is this order

Each step depends on an output of an earlier one. The dependencies are real, not
stylistic:

| # | Step | Depends on | Produces |
|---|---|---|---|
| 1 | Janua **organization** | — | **org UUID** (every later step needs it) |
| 2 | Janua **entitlements** (`product_tiers`) | 1 | tier claims in issued JWTs |
| 3 | Enclii **team/tenant** + project | — | team id (client's self-serve home) |
| 4 | Enclii **namespace** + GHCR creds | 3 | namespace |
| 5 | Managed **Postgres** + role (+ clones) | 4 | `DATABASE_URL` material |
| 6 | **Buckets** (R2) | 3 | bucket + scoped token material |
| 7 | **Secrets** (contract check, then provision) | 4, 5, 6 | K8s Secret per app |
| 8 | Janua **OAuth clients** | 1, and each app's final host | client id/secret |
| 9 | **Services** registered from each app's `enclii.yaml` | 4, 7 | service records |
| 10 | **Domains** — tunnel routes + DNS + TLS | 9 | reachable hosts |
| 11 | **Nauta workspace** | 1 (org UUID), 10 (hostnames) | workspace |
| 12 | **Kalya tenant** | 1 (org UUID → `Tenant.id`) | tenant + hosts + event types |

Two ordering constraints deserve to be called out because getting them wrong is
expensive:

**Domains (10) come after services (9), never before.** enclii#468 exists because
capture provisioned domains from a `metadata.name` that resolved to no live workload.
Route-first ordering guarantees that failure mode on every fresh tenant, since by
definition nothing is running yet.

**OAuth clients (8) come after the hosts are known but before first login.** A redirect
URI must match the app's real public origin exactly. That origin is `APP_ORIGIN`, which
this manifest declares — so the client can be registered from the manifest before the
domain is live, but it cannot be registered from a guess.

### Idempotency contract

**Every step MUST be re-runnable, and `tenant apply` on an unchanged manifest MUST be
a no-op that exits 0.** Concretely, every step is `check-then-act`:

1. **Check** — read the target's current state by its natural key
   (org by `slug`, workspace by `slug`, Kalya tenant by `slug`, database by name,
   secret by name, domain by host, service by name).
2. **Classify** — `absent` → create; `present and matching` → skip; `present and
   differing` → converge if the field is safely updatable, otherwise **report drift and
   do not write**.
3. **Act**, then re-read to confirm.

Each step reports one of `created | unchanged | converged | drift | failed | skipped`,
and the run's exit code is derived from the set, not from the last step.

Where the backend does not provide idempotency, **the CLI owns it**. Two known cases:

- **Janua org-create returns 400 on a duplicate slug.** `tenant apply` must
  `GET` the org by slug first and treat "already exists, same owner" as `unchanged`.
- **Nauta `createWorkspace` is a bare `create` and throws on re-run.** Same treatment:
  look up by slug first. A hostname already claimed by *another* workspace is `drift`,
  never an overwrite — `WorkspaceHostname.hostname` is globally unique, so silently
  reassigning it would steal another client's hostname.

**Immutable fields are checked, never written twice.** Kalya's `Tenant.id` throws on
mismatch by design. `tenant apply` compares and reports; it never attempts the write.

### Idempotency is not the same as convergence

Worth stating because the two existing provisioners differ on it. Kalya's script
converges (it updates a renamed host's colour). Nauta's `create` does not exist a
second time. Where a field is genuinely ambiguous — has the operator changed the
manifest, or has someone changed the live system? — `tenant apply` reports drift and
stops touching that resource. It does not guess, and it does not "restore" a value a
human may have deliberately set.

---

## Failure and rollback doctrine

**Soft failure. `tenant apply` never deletes anything, under any circumstance,
including its own partial work.**

There is no `tenant destroy` in this design and no rollback of a partial apply. The
reasoning is not squeamishness:

1. **Half a tenant is recoverable; a deleted org is not.** Steps 1–12 create
   identity roots and databases. A failed step 7 that rolled back step 1 would delete
   an organization that steps 2 and 3 may already have granted entitlements against,
   and whose UUID may already be embedded in a Kalya `Tenant.id` that throws on
   mismatch forever after.
2. **Deletion is exactly the outage class we already have.** The junction reconciler
   deleted routes it was not told about; manifest capture rewrote hostnames to backends
   that never existed. A tenant-scoped deleter is a strictly larger version of both.
3. **Idempotency makes rollback unnecessary.** Because every step is check-then-act,
   the recovery procedure for a partial apply is to fix the cause and re-run. Completed
   steps report `unchanged`; the failed step retries.

So on any step failure:

- **Stop the dependent chain, continue the independent one.** If step 5 (Postgres)
  fails, steps 6 (buckets) and 8 (OAuth clients) do not depend on it and still run;
  step 7 (secrets, which needs `DATABASE_URL`) is marked `skipped (blocked by: db)`.
- **Report every step's status, failures last and on stderr.** This follows
  `printOnboardResult` in `onboard.go` exactly, and for the reason recorded there:
  onboarding nauta on 2026-08-11 reported success while never creating its R2 bucket,
  and the miss surfaced only when an operator went to mint a token scoped to a bucket
  that did not exist. **A provisioner that half-provisions and exits 0 is worse than
  one that fails, because the operator moves on.**
- **Exit non-zero on any `failed` or `skipped`.** `drift` also exits non-zero: an
  operator must adjudicate it.
- **Never emit a secret value into plan output, logs, or audit.** Secret steps report
  key names and counts.

### What an operator does with a partial apply

Read the failed steps (they name the cause), fix it, re-run the same manifest. That is
the whole runbook. If the manifest itself was wrong, edit it and re-run; the steps that
already matched report `unchanged` and cost one read each.

---

## Cross-platform gaps — the honest blockers

These are the reason this RFC ships a dry-run skeleton and not an implementation. Each
is a change in a repo enclii does not own.

### GAP-1 — Janua: org-create is reachable, but not by a service

**Status: partially closed. Narrower than previously believed.**

An earlier framing of this work asserted Janua had no org-create endpoint and that raw
SQL was the only path. **That is no longer true.**
`POST /api/v1/admin/organizations` exists on `main`
(`apps/api/app/routers/v1/admin.py`, `create_organization_admin`), is mounted, and its
docstring names this exact scenario — "standing up one canonical org that provisions a
client across every product slice". It takes `name`, `slug`
(`^[a-z0-9-]+$`), exactly one of `owner_email` | `owner_id`, and optional
`description` / `billing_email`; it resolves the owner before any write; it adds the
owner as an `owner`-role member.

The residual gap is **authentication shape**, and it is a real blocker:

- The endpoint is gated by `check_admin_permission(current_user)` — a **human platform
  admin's JWT** (`user.is_admin`). A machine caller holding only `INTERNAL_API_KEY`
  cannot reach it.
- The `/api/v1/internal` surface (which *is* internal-key-authed) carries email, users
  and role/tier sync — no organizations.
- Duplicate slug returns **400**, not the idempotent 201/200 that
  `internal_users.provision` established.

**Ask:** `POST /api/v1/internal/organizations`, following `internal_users.py` exactly —
`_auth: bool = Depends(verify_internal_api_key)`; 201 on create and 200 on
already-exists with a `created: bool` discriminator; existing orgs returned untouched
("provisioning, not synchronization"); best-effort audit that never blocks; and **no
delete endpoint, ever**. The handler body can reuse the existing admin logic behind a
swapped dependency. ADR-004 (capability links) already sets the precedent for putting
new internal-key-authed resources on that prefix.

One JSONB caution carried over from `internal_users.py`: `organizations.product_tiers`,
`settings` and `org_metadata` are plain JSONB with no `MutableDict` wrapper. They must
be **rebuilt and reassigned**, never mutated in place, or the write may never flush.

**Until GAP-1 closes**, step 1 runs with an operator's platform-admin token (the shape
`kalya/scripts/provision-janua-client.mjs` already uses) — which means
`tenant apply` cannot run unattended in CI. That is the practical cost.

### GAP-2 — Kalya: tenant provisioning has no callable interface

**Status: open. Confirmed absent, not merely unfound.**

Kalya provisioning is `npm run provision:tenant -- --config <file>`, a `tsx` script
that opens a direct Postgres connection under an RLS bypass. There is **no HTTP tenant
provisioning endpoint**: no `/api/admin/*`, no `/api/v1/tenants`. The `v1/manage/*`
routes consume a Janua token whose `tenant_id` claim must already resolve to an
existing tenant — they operate *within* a tenant and cannot create one.

The script itself is the best of the three: genuinely convergent, keyed on natural keys,
soft-ends resource blocks rather than deleting them, and never touches bookings.
**The logic is not the gap; the interface is.** `enclii tenant apply` would have to hold
Kalya's `DATABASE_URL` to invoke it, which puts a client's clinical database credential
inside the platform CLI's blast radius for no benefit.

**Ask:** an internal-key-authed `POST /api/v1/internal/tenants` in Kalya that accepts
the same JSON the script already validates with `tenantConfigSchema` and calls
`provisionTenant` behind it. The Zod schema and the convergent implementation both
already exist; this is a transport, not a rewrite.

**Until GAP-2 closes**, step 12 emits the exact `npm run provision:tenant` command for
an operator to run, and reports `skipped (manual)`.

### GAP-3 — Nauta: workspace-create exists but is neither idempotent nor machine-callable

**Status: open, two distinct defects.**

`createWorkspace` exists (`packages/services/src/workspace.service.ts`) and is exposed
as the tRPC mutation `workspace.create`. It is well-guarded — deliberately twice, since
on 2026-08-07 an actor with no roles provisioned a workspace through it, after which
`assertPlatformAdmin` was added *in the service* as well as at transport.

Two problems for an orchestrator:

1. **Not idempotent.** It is a bare `create`. Re-running throws
   `CONFLICT/HOSTNAME_TAKEN` or a unique violation on `slug` / `januaOrganizationId`.
2. **tRPC, not REST.** Reachable over HTTP only through the tRPC route handler, and
   gated on the cockpit host plus the `nauta:platform-admin` Janua role. There is no
   `POST /api/workspaces`.

**Ask (in priority order):** (a) make the service converge — look up by slug, return the
existing workspace unchanged, and treat a hostname owned by a *different* workspace as a
hard conflict rather than a reassignment; (b) expose an internal-key-authed REST
entrypoint for it.

**Until GAP-3 closes**, step 11 does its own check-then-act via the tRPC surface using an
operator token, or reports `skipped (manual)` with the exact input JSON.

### The credential problem these gaps add up to

With all three gaps open, a full `tenant apply` needs: an Enclii API token, a Janua
platform-admin **user** JWT, a Kalya `DATABASE_URL`, and a Nauta cockpit session with a
platform-admin role. Four credentials in three trust domains, two of which are
human-interactive. **That is the actual reason CLIENT-IN-A-DAY is not a day yet** — not
the YAML. Closing GAP-1 through GAP-3 collapses it to one Enclii token plus one internal
key per sibling, which is the point of the whole exercise.

---

## Non-goals

- **No `tenant destroy`, no rollback, no reconciliation-with-deletion.** §5.
- **No restating of app manifests.** `apps[].manifest` references the app's own
  `enclii.yaml`; build/runtime/probe/SLO stay there.
- **No secret values in the manifest.** Keys only.
- **No DDL beyond database and role creation.** `db.rls` is advisory; migrations
  belong to apps.
- **No inlining of the Kalya tenant file.** It is referenced. Its schema is Kalya's.
- **No new domain-provisioning path.** Step 10 calls what `onboard` already calls.

---

## What this RFC ships now

1. This document.
2. `enclii tenant apply -f <manifest>` — **dry-run only**. It parses and validates the
   manifest, resolves defaults, and prints the ordered plan with every step's intended
   action and its dependencies. Execution stubs return not-implemented naming this RFC.
3. A validator with unit tests covering the constraints that have already caused
   outages: slug charset and the 63-char cross-platform floor; flat-label domain hosts;
   globally-unique hostnames within a manifest; secret keys declared without values;
   OAuth redirect URIs agreeing with their environment's `APP_ORIGIN`; and every
   cross-reference (`envFrom` → a declared secret, `db.clones[].from` → a declared
   database, `apps[].environments[].domains` → unique across the manifest).

`tenant apply` **cannot** mutate anything today. There is a hidden `--execute` flag,
and it exists precisely so the unimplemented path fails loudly and by name — it returns
a not-implemented error citing this RFC, and calls nothing. It is hidden rather than
absent because a flag that always errors reads as a broken command in `--help`, while a
missing one reads as a capability an operator may assume is on by default.

---

## Consequences

**Positive.** The client's whole shape becomes one reviewable file, in git, with the
same review as code. Re-running is safe, so onboarding stops being a
performance-under-pressure. The plan output is a checklist an operator can follow by
hand while the gaps are open — the RFC pays for itself before a single step executes.

**Negative, stated honestly.** A fifth place that knows about domains is a fifth place
that can be wrong about them; the mitigation is that `tenant apply` reads the app's
`enclii.yaml` rather than duplicating it, and validates agreement where they overlap.
The manifest will also drift from reality the moment anyone changes something by hand,
and with no deletion semantics `tenant apply` will report that drift rather than fix it —
which is correct, but it means the manifest is not a guarantee, only a declared intent
plus a diff.

**Deploy note.** Additive only. No existing command's behavior changes. `tenant` is a
new subcommand tree; `onboard` is untouched and remains the supported path for
single-app onboarding.

---

## Appendix A — the CTM hand-trail (what we actually ran)

The provisioning of Crea Tu Mundo across 2026-08-16 → 2026-08-30, reconstructed from the
repos. Commands are sanitized: no tokens, no secret values, no credential-bearing URLs,
and client-specific hostnames are shown as `example.mx`. This is the requirements
document — every schema field in §3 traces to a line here.

**1. Janua organization.** Created for the org slug `crea`. The endpoint
(`POST /api/v1/admin/organizations`) requires a platform-admin user JWT; where that was
not to hand the row was inserted directly against the Janua database, slug-guarded. Both
paths produce the same thing that matters downstream: **the org UUID**. Every later step
consumes it.

```
# The supported path (needs a platform-admin bearer token):
POST https://<janua-host>/api/v1/admin/organizations
  { "name": "Crea Tu Mundo Autismo", "slug": "crea", "owner_email": "<owner>" }
# The workaround actually used at the time: a slug-guarded INSERT into
# `organizations`, then reading back the generated UUID.
```

**2. Janua OAuth clients.** Registered per app-environment. `crea-map` and
`crea-map-staging` are separate clients precisely so the staging redirect URI never
touches the production client's allow-list.

```
node scripts/provision-janua-client.mjs            # in the kalya repo
# → prints {"organization_id":"<uuid>","org_slug":"crea","client_id":"...", ...}
# The client_secret is displayed once. It was never committed.
```

**3. Enclii onboarding, per app.** The single-app pipeline this RFC generalizes.

```
enclii onboard --repo madfam-org/crea-map \
  --project crea-map \
  --db-name crea_map \
  --db-password "$(openssl rand -base64 32)" \
  --dry-run                                        # inspected first, every time
enclii onboard --repo madfam-org/crea-map --project crea-map --db-name crea_map
```

**4. Secrets.** Values assembled out-of-band, never committed, provisioned by file.

```
enclii admin provision secrets \
  --namespace crea-map --secret-name crea-map-secrets \
  --secrets-file ./crea-map.env --force
# Keys: DATABASE_URL, DIRECT_DATABASE_URL, JANUA_CLIENT_SECRET,
#       JANUA_INTERNAL_API_KEY, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY
```

**5. Domains.** Declared in `crea-map/enclii.yaml` under `spec.domains` and captured by
onboarding — flat labels only, one level below the apex, so Universal SSL covers them.
`APP_ORIGIN` is pinned per environment: without it, a tunnelled pod builds its
`redirect_uri` from its internal origin and login breaks entirely (prod incident,
2026-08-24).

**6. Database clones for the QA twin and staging.** Cloned on the *same* instance under
the *same* owner, deliberately — a new addon means a new DB role, which means appending
to the static pgbouncer userlist, and a botched hand-edit of that userlist is the
2026-08-24 pooled-auth outage class.

```
scripts/ensayo-twin/clone-db.sh                    # in the crea-map repo
```

**7. Enclii team (tenant) + re-parenting.** Migration 038 (enclii#474) inserts the `crea`
team with `ON CONFLICT (slug) DO NOTHING`, re-parents both projects guarded by
`team_id IS NULL`, and backfills the master admin as owner with a `NOT EXISTS` guard —
the idempotency shape this RFC's §4 generalizes.

**8. Nauta workspace.** Created with `slug = crea`, `januaOrganizationId` = the step-1
UUID, `tier = FRACTIONAL_CTO`, plus its primary hostname.

**9. Kalya tenant.** The step-1 org UUID was **hand-copied** into `tenant.id` in
`prisma/provision/ctm-tenant.json`, then:

```
npm run provision:tenant -- --config prisma/provision/ctm-tenant.json --dry-run
npm run provision:tenant -- --config prisma/provision/ctm-tenant.json
```

That hand-copied UUID between step 1 and step 9 — immutable afterwards, and a hard throw
on mismatch — is the single sharpest argument for this RFC. It is a manual transcription
of a primary key between two systems, performed once, unverifiable afterwards, and
unrecoverable if wrong.

---

## Appendix B — step-to-owner map

| Step | Executed by | Interface today | Interface wanted |
|---|---|---|---|
| 1 Janua org | Janua | `POST /api/v1/admin/organizations` (admin JWT) | `POST /api/v1/internal/organizations` (GAP-1) |
| 2 Entitlements | Janua | `POST /api/v1/admin/entitlements/org` (admin JWT) | internal-key variant |
| 3–7, 9, 10 Runtime | Enclii | `POST /v1/admin/onboard` + provisioning APIs | unchanged |
| 8 OAuth clients | Janua | `POST /api/v1/oauth/clients` | unchanged |
| 11 Nauta workspace | Nauta | tRPC `workspace.create` (cockpit + role) | idempotent + REST (GAP-3) |
| 12 Kalya tenant | Kalya | `npm run provision:tenant` (direct DB) | `POST /api/v1/internal/tenants` (GAP-2) |
