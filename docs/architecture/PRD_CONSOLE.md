# PRD: Console — Live Operations Surface for Enclii

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


> **Version**: 0.1.0 (draft)
> **Status**: Draft — capturing positioning for future scoping
> **Author**: MADFAM Engineering
> **Created**: 2026-04-24
> **Last Updated**: 2026-04-24
> **Domain**: `console.enclii.dev` (currently unclaimed / orphan DNS)

---

## Executive Summary

**Console** is a browser-based live operations surface for services running on Enclii. It is *not* a second version of Switchyard UI or Dispatch — it's a third UI with a distinct audience, security posture, and interaction model.

Today Enclii has:

| Surface | Audience | Mode | Actions |
|---|---|---|---|
| `app.enclii.dev` (Switchyard UI) | Developers | Declarative | Edit configs, view deployments, manage teams |
| `admin.enclii.dev` (Dispatch) | Platform operators | Declarative | Fleet topology, clusters, governance |

Missing: the **live imperative operations** surface that every mature PaaS provides — Heroku's Console tab, Fly's `flyctl ssh console`, Railway's exec, Vercel's (thin) CLI-only equivalent.

Console fills that gap at `console.enclii.dev`.

### Key Value Proposition

| Without Console | With Console |
|---|---|
| `kubectl exec` via CLI only — cluster-admin credential required | Browser shell scoped to the service + audit-logged |
| One-off tasks (migrate, seed) via `enclii run` — terminal only | One-click task runner with parameterised commands |
| Live log tailing scattered across Grafana queries | Per-service tail with structured search + replay |
| Release ops (rollback, promote, pause autoscale) via CLI | One-click per action with audit trail |
| Database console requires CLI + local tooling | `enclii db psql` via web, read-only default |

---

## Problem Statement

### Current pain points

1. **Incident response pattern is CLI-centric**. When a service misbehaves at 2am, the fastest path to "get in and poke" is `kubectl exec` — which requires full cluster credentials and leaves no audit trail beyond shell history.

2. **One-off task runners are fragmented**. Each product on Enclii (Karafiel, Dhanam, Avala, Tezca, etc.) has its own ad-hoc `admin/` page or bespoke scripts for "run migration", "seed this", "rotate that key". No uniform pattern.

3. **Elevated-privilege ops have no audit trail**. Production `kubectl exec` → interactive shell → `DROP TABLE x` is indistinguishable from a sanctioned migration. Audit story is "hope the logs caught it".

4. **Release operations are CLI-gated**. Rolling back a bad deploy is `enclii rollback <service>`. Fine when calm. Hard when stressed at 2am with only your phone.

5. **Database introspection is local-tooling-dependent**. To `psql` a prod DB, the operator needs: (a) VPN or tunnel, (b) `psql` installed, (c) a credential fetched from a secrets store. High friction → people avoid it → problems linger.

### Market context

| PaaS | Browser shell | One-off task runner | Live log tail | DB console | Release ops UI |
|---|---|---|---|---|---|
| Heroku | ✅ (Console tab) | ✅ (Heroku Run) | ✅ | ✅ (Heroku Data) | ✅ |
| Fly.io | ❌ (CLI only: `flyctl ssh console`) | Partial (CLI `flyctl machine exec`) | ✅ (web) | ❌ | ✅ |
| Railway | ✅ (recent) | ✅ | ✅ | ✅ | ✅ |
| Render | ✅ | ✅ | ✅ | ✅ | ✅ |
| Vercel | ❌ (no runtime, functions only) | n/a | ✅ | ❌ (integrations only) | ✅ |
| **Enclii today** | ❌ | ❌ | Partial (Grafana) | ❌ | Partial (CLI) |

**The competitive gap is real.** Heroku's Console was one of the headline features that justified its price against raw EC2. A first-class console is table stakes for the PaaS category.

---

## Goals & Non-Goals

### Goals

1. **G1**: Interactive shell from browser into a spawned pod attached to a service — `kubectl exec -it` equivalent, scoped by Enclii project role.
2. **G2**: Parameterised task runner — "run the migration" / "run this arbitrary command with this env" — via a registered catalogue per service.
3. **G3**: Per-service live log tail with structured search (level, field, time range) — beyond what generic Grafana queries provide.
4. **G4**: Release operations panel — rollback to digest, promote canary, pause autoscaling, force restart — one click per action, audit-logged.
5. **G5**: Database console — `enclii db psql <service>` through the browser, read-only by default, elevated-write mode requires re-auth.
6. **G6**: Every imperative action writes to an append-only audit log visible in Dispatch's governance module.

### Non-Goals

- ❌ **Not a replacement for the CLI**. The CLI remains the authoritative interface. Console is *additive* for moments when the browser is faster.
- ❌ **Not a replacement for product-specific admin UIs**. Karafiel's `/admin` understands its domain; Console is the generic fallback when a product doesn't have one.
- ❌ **Not Grafana**. Console's "live log tail" is a few screens of real-time output; deep historical analysis stays in Loki/Grafana.
- ❌ **Not a general K8s dashboard**. Dispatch already covers fleet-level topology; Console stays at the service level.
- ❌ **Not an IDE / web editor**. We're not shipping a browser-based VS Code. Exec + run-task is the scope.

---

## Architecture

### High-level design

```
┌──────────────────────────────────────────────────────────────────┐
│                    Browser (console.enclii.dev)                   │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │ xterm.js terminal  │  Task runner  │  Log tail  │  DB REPL │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────────┬────────────────────────────────────┬──────────────┘
               │ HTTPS + WebSocket (short-lived)    │
               ▼                                     ▼
┌──────────────────────────────────┐  ┌──────────────────────────┐
│  Cloudflare Tunnel + Access       │  │  Zero Trust policy per  │
│  (OIDC step-up for dangerous ops) │  │  project role           │
└──────────────┬───────────────────┘  └──────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────────────────────────────┐
│            switchyard-api (new /v1/console/* handlers)            │
│  • POST /v1/console/sessions        — open shell session          │
│  • GET  /v1/console/sessions/:id/ws — WebSocket shell stream      │
│  • POST /v1/console/tasks/:name     — run registered task         │
│  • GET  /v1/console/logs/:service   — SSE log tail                │
│  • POST /v1/console/db/:service/query — parameterised SQL         │
│  • POST /v1/console/releases/:service/rollback                    │
│                                                                    │
│  Every handler writes to the audit log BEFORE dispatching.         │
└──────────────┬───────────────────────────────────────────────────┘
               │ client-go (in-cluster service account)
               ▼
┌──────────────────────────────────────────────────────────────────┐
│                        k3s cluster (enclii-workloads)              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐               │
│  │ karafiel-api│  │   janua-api │  │ dhanam-api  │ ...           │
│  └─────────────┘  └─────────────┘  └─────────────┘               │
│                                                                    │
│  Console spawns ephemeral `kubectl debug` pods attached to the    │
│  target's pod spec, not `exec` into running containers (safer:    │
│  no shared PID namespace, no risk of pkill'ing prod workers).     │
└──────────────────────────────────────────────────────────────────┘
```

### Security posture (different from app.enclii.dev)

| Aspect | Switchyard UI | Console |
|---|---|---|
| Session length | Long-lived OIDC cookie (hours) | Short-lived session (≤30 min), re-auth for shell |
| Step-up MFA | On first login only | On every dangerous action (shell open, task run, DB write) |
| Audit granularity | Config changes logged | Every keystroke in shell, every command, every query |
| Role gating | Project member = can view | Tiered: viewer / operator / admin per service |
| Network | Direct HTTPS | Cloudflare Zero Trust Access policy in front |

### Tech stack

- **Frontend**: Next.js 14 (App Router), React 18, Tailwind, xterm.js for the shell
- **Backend**: New handlers in `apps/switchyard-api` (Go) — `/v1/console/*`
- **Shell protocol**: WebSocket ↔ `kubectl attach` (via client-go `rest.Request().VersionedParams(&v1.PodAttachOptions{...}, scheme.ParameterCodec).URL()`)
- **Task runner**: parameterised job templates, dispatched as K8s `Job` objects with TTL
- **Log tail**: SSE proxy to Loki query API, per-service labelled streams
- **DB console**: routes SQL through `pgconn` with a per-session read-only role; write mode requires elevated token
- **Audit log**: append to existing `audit_events` table in switchyard-api, exposed via Dispatch's governance module

---

## User stories

### US1 — Restart a stuck worker at 2am

*"As an on-call engineer, when Prometheus pages that karafiel-worker queue depth is growing, I want to open console.enclii.dev on my phone, find the worker service, click 'Restart', and be done — without needing a laptop, VPN, or kubectl."*

### US2 — Run a missed migration

*"As a developer, when I deploy a new release that expects a schema migration and the app is crashlooping, I want to open console, select the service, click 'Run task → migrate', see the output stream in real-time, and know when it's safe to retry the deploy."*

### US3 — Diagnose a SQL slow query

*"As a developer, when p95 latency spikes on the tezca-api, I want to open console → tezca-api → DB → paste an EXPLAIN ANALYZE query, get the result, without installing `psql` or fetching a credential from 1Password."*

### US4 — Emergency rollback

*"As a senior engineer, when a deploy goes bad, I want to open console → service → Releases → see the last 5 digests → click 'Rollback to previous' with a confirmation dialog → and have it recorded in the audit log with my identity."*

### US5 — Shell into a pod to diagnose

*"As a platform operator, when a pod is wedged in a weird way that logs don't explain, I want to open a shell in a debug pod attached to that pod's spec, poke around with `ps`, `netstat`, `curl localhost:8080/metrics`, and close the session with a full transcript captured in the audit log."*

---

## Ecosystem synergy

Every MADFAM product currently on Enclii has its own ad-hoc admin surface:

- Karafiel: `/admin` (Next.js, its own auth wiring)
- Avala: `apps/admin/` (standalone Next.js app, different auth)
- Dhanam: `admin.dhan.am` (separate app, SSO via Janua)
- Tezca: `apps/admin` (yet another implementation)

These exist because each product needed *some* way to run one-off ops and most of the admin UIs could not reuse a common surface. **Console could become the uniform fallback** — "I don't have product-specific admin, but I can still `enclii console <service>` to run a task / shell in / query the DB."

That's significant leverage:
- 10 products × (run_task + shell + db_console + rollback) = 40 ad-hoc features replaced by one shared implementation
- Product teams stop reimplementing the same thing
- Ops teams get a uniform pattern for every service they manage

### Progressive rollout strategy

1. **Phase 1**: Console ships for Enclii itself. Dogfood on `switchyard-api`, `switchyard-ui`, `roundhouse`. Team validates the UX.
2. **Phase 2**: Opt-in for other MADFAM products. Add `console: enabled: true` to `.enclii.yml`. Sane defaults per-service via role inference.
3. **Phase 3**: Default-on for new projects. Existing products migrate gradually.
4. **Phase 4** (optional): External exposure as a paid feature tier for non-MADFAM Enclii users.

---

## Metrics of success

| Metric | Baseline (no Console) | Target (with Console) |
|---|---|---|
| Median time to restart a service in incident response | ~5 min (laptop, VPN, kubectl) | ~30 sec (phone, browser, click) |
| % of prod ops with full audit log | Estimated ~40% (kubectl exec leaves no structured trail) | >95% |
| Product teams reimplementing admin surfaces | 4+ separate implementations | 1 shared + product-specific overrides |
| On-call engineer satisfaction (internal NPS) | TBD baseline | +20 pts after 3 months |
| Ecosystem product adoption of Console | 0 | 5/10 in 6 months |

---

## Phased scope estimate

| Phase | Scope | Effort |
|---|---|---|
| **MVP** | Shell + audit log + basic role gating | 2-3 weeks focused |
| **Phase 2** | Task runner + live log tail | 3-4 weeks |
| **Phase 3** | DB console (read-only) + release ops panel | 3-4 weeks |
| **Phase 4** | DB write mode with elevated auth + step-up MFA | 2 weeks |
| **Phase 5** | Multi-product rollout (docs, onboarding, opt-in wiring) | 2-3 weeks |

**Total**: 12-16 weeks for full scope. MVP alone delivers 60% of the daily value.

---

## Open questions

1. **Cloudflare Zero Trust vs. in-app auth step-up?** Zero Trust is cleaner architecturally but adds a dependency. In-app step-up via Janua is more portable. Decision deferred to design phase.

2. **Shell session recording — mandatory or opt-in?** Recording every shell keystroke is powerful for audit but may deter legitimate exploratory use. Proposal: mandatory on production, opt-in on staging, off on dev.

3. **Task runner: declarative or dynamic?** Option A: every runnable task declared in `.enclii.yml` (`tasks: { migrate: "poetry run manage.py migrate" }`). Option B: arbitrary commands with role-based allowlist. Proposal: start with A, add B as elevated-privilege extension.

4. **DB console: per-service or shared?** Current architecture has shared Postgres in `data` namespace (Karafiel, Dhanam, Tezca share) plus in-namespace DBs (Pravara-MES, Avala). Console needs to understand both patterns. Proposal: service-scoped views that resolve the right DB via service metadata.

5. **Naming**. "Console" is generic. Heroku uses Console. Railway uses Console. If we want to match Enclii's rail theming, alternatives: **Signal-box** (the rail-ops center), **Yardmaster** (the operator in charge of moves), **Switchtower**. Proposal: ship as "Console" for discoverability, reserve right to rename.

---

## Alternatives considered

### Alt 1 — Extend Dispatch to cover console

**Why not**: Dispatch is a superuser tool with fleet-level scope. Mixing developer-level per-service ops into Dispatch would blur the mental model and force every developer to have superuser-like access.

### Alt 2 — Extend Switchyard UI to include console features

**Why not**: Switchyard UI is declarative (view/edit config). Console is imperative (run commands). Security posture is different (long session vs. step-up MFA), audit needs are different, and the two UIs would fight over nav conventions. Separation is honest.

### Alt 3 — Don't build it; keep CLI-only

**Why not**: Leaves the competitive gap open. Every comparable PaaS has a console. It's what operators reach for when the 2am page fires and they're on their phone. The CLI is great for calm work; console is for urgent work.

### Alt 4 — Delete console.enclii.dev and stop carrying the idea forward

**Why not** (though this was seriously considered): The DNS record has been live as orphan for some time, which suggests the intent existed but execution stalled. Claiming the surface with a real product is better than deleting and rebuilding credibility later.

---

## Decision needed

Not from this PRD — this is positioning. The actual decision is whether to schedule MVP scoping in Q2/Q3 2026, or park the idea and delete the DNS.

If scheduled: next step is a 1-day design session to pick the tech for shell protocol + Cloudflare Zero Trust integration, then an MVP ticket breakdown.

If parked: delete the DNS record; this PRD sits in `docs/architecture/` for future reference.

---

## References

- `apps/switchyard-ui/` — developer dashboard (comparative reference)
- `apps/admin-console/` — superuser admin (comparative reference)
- `docs/architecture/SOFTWARE_SPEC.md` — overall Enclii architecture
- Heroku Dev Center: <https://devcenter.heroku.com/articles/heroku-cli-commands#heroku-run> (closest comparable)
- Railway console docs: <https://docs.railway.app/overview/ssh> (modern comparable)
