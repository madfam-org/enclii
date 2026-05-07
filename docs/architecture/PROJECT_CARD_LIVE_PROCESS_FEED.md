---
title: Project Card Live Process Feed
description: Plan for live, interactive build, deploy, and provisioning visibility in Enclii project cards
sidebar_position: 8
tags: [architecture, ui, deployments, observability]
---

# Project Card Live Process Feed

**Status:** implemented foundation, producer expansion remaining  
**Last updated:** 2026-05-06  
**Scope:** Switchyard API, Switchyard UI, lifecycle events, build/deploy tracking, Enclii ops integration

## Implementation Status

Implemented in the 2026-05-06 working tree:

- read-only process summary API at `GET /v1/project-processes/summary`
- single-project timeline API at `GET /v1/projects/:slug/processes`
- server-sent event stream at `GET /v1/project-processes/stream`
- single-project stream at `GET /v1/projects/:slug/processes/stream`
- lifecycle-event and service-state process normalization
- project-card hook using authenticated fetch-based SSE with batched polling fallback
- process rail in grid cards and compact state indicator in list rows
- per-service process chips beside service status
- interactive sheet drawer from the process rail
- lazy drawer history load from `GET /v1/projects/:slug/processes`
- home dashboard and `/projects` page both receive the same feed
- existing per-service domain rows remain tied to each service environment

Still remaining:

- event-driven backend fanout from lifecycle/deploy callbacks; the current SSE
  endpoint emits an initial snapshot and refreshes summaries on an interval
- `Last-Event-ID` resume/backfill semantics beyond the current snapshot refresh
- wider event producer coverage for domains, addons, jobs, providers, and
  Enclii `ops` remediation results
- OpenAPI/SDK coverage once the read model stabilizes

## Goal

Every project card must show, in real time, what is happening across every
service assigned to that project:

- source push received
- CI queued/running/succeeded/failed
- Roundhouse build started/ready/failed
- image pushed, signed, and SBOM attached
- digest committed
- ArgoCD sync/deploy/rollback progress
- Kubernetes rollout health for the new ReplicaSet
- preview creation/destruction
- domain, secret, addon, database, cron, and one-off job provisioning events
- failed or blocked operations that need operator or agent action

The card must separate **serving health** from **active rollout state**. A
service can be healthy while a newer deployment is failing readiness; the UI
must expose that difference instead of collapsing it into a single green state.

## Current Reality

The existing project page already has enough shape to host the feed, but it is
polling coarse state:

- `app/(protected)/projects/page.tsx` fetches `/v1/projects`, then fetches
  `/v1/projects/:slug/services` per project and maps service state into compact
  cards.
- `ProjectCardCompact` renders service status, health, domains, replica counts,
  digest chips, repo metadata, and deployment freshness.
- `ProjectRowCompact` renders the dense list-mode view and should get a smaller
  process indicator.
- Switchyard API already records lifecycle events and exposes timeline endpoints
  under `/v1/lifecycle/*`.
- Switchyard API already exposes unified per-service build status at
  `/v1/services/:id/builds/:build_id/status`.
- Switchyard API already exposes generic activity via `/v1/activity`.

What is missing is a **project-card optimized process read model** and a
**live transport**. The UI should not add per-service build-status requests for
every visible card.

## Canonical Process Model

Add a normalized process DTO owned by Switchyard API:

```ts
type ProjectProcess = {
  id: string;
  correlation_id: string;
  project_id: string;
  project_slug: string;
  service_id?: string;
  service_name?: string;
  kind:
    | "git_push"
    | "ci"
    | "build"
    | "image"
    | "digest"
    | "gitops_sync"
    | "deploy"
    | "rollback"
    | "rollout"
    | "preview"
    | "domain"
    | "secret"
    | "addon"
    | "database"
    | "job"
    | "operator";
  status:
    | "queued"
    | "running"
    | "waiting"
    | "succeeded"
    | "failed"
    | "blocked"
    | "cancelled"
    | "unknown";
  phase?: string;
  message?: string;
  branch?: string;
  commit_sha?: string;
  environment?: string;
  progress?: number;
  source:
    | "github"
    | "ci_callback"
    | "roundhouse"
    | "argocd"
    | "kubernetes"
    | "switchyard"
    | "enclii_ops"
    | "provider";
  links?: {
    logs?: string;
    github_run?: string;
    deployment?: string;
    lifecycle?: string;
    remediation?: string;
  };
  started_at?: string;
  updated_at: string;
  completed_at?: string;
};
```

Correlation rules:

1. Prefer explicit `correlation_id` when present.
2. Fall back to `service_id + commit_sha + environment`.
3. Fall back to `project_id + operation_id` for provider/provisioning events.

Every event producer should eventually provide an explicit correlation ID so
Selva and other agents can trace process chains without guessing.

## Backend Plan

### Phase 1 — Process Summary API

Add read-only endpoints:

- `GET /v1/project-processes/summary?project_ids=...&limit_per_project=5`
- `GET /v1/projects/:slug/processes?limit=50&active_only=false`

Responsibilities:

- build one normalized response per project, not one request per service
- merge lifecycle events, CI runs, releases, deployments, audit logs, previews,
  junction/domain records, addons, jobs, and Enclii ops activity
- compute `active_count`, `failed_count`, `blocked_count`, `latest`, and
  per-service summaries
- apply tenant/team RBAC exactly like the existing project and service routes
- cap result sizes so the dashboard remains cheap

Initial source priority:

1. `deployment_lifecycle_events` for push/build/image/digest/deploy/preview
2. `ci_runs` for GitHub Actions state and links
3. `releases` for Roundhouse build/image/SBOM/signature state
4. `deployments` for deployment status, health, rollback, and version labels
5. `audit_logs` for operator/user actions
6. domain/junction/addon/job repositories for provisioning processes

Do not call `/v1/services/:id/builds/:build_id/status` from the project page.
That endpoint is still useful for drilldown after a user clicks a process.

### Phase 2 — Live Stream

Add a project-scoped server-sent event stream:

- `GET /v1/project-processes/stream?project_ids=...`
- `GET /v1/projects/:slug/processes/stream`

SSE is the preferred first implementation because project cards need one-way
updates and simple fanout. The browser client uses authenticated `fetch()`
streaming instead of native `EventSource` so the JWT remains in headers rather
than query strings. WebSocket can remain reserved for bidirectional terminals
or log streams.

Current stream behavior:

- emits an initial snapshot from the process summary read model
- refreshes summaries on a bounded interval and only emits changed payloads
- sends heartbeat comments every 20-30 seconds
- disables proxy buffering where needed
- falls back to polling when blocked by browser, auth, or tunnel behavior

Still-required stream behavior:

- supports `Last-Event-ID` resume
- performs event-driven fanout from lifecycle/provisioning producers instead of
  relying only on interval refresh

### Phase 3 — Event Producer Coverage

Emit or normalize process events from:

- GitHub push/webhook ingestion
- CI callback lifecycle events
- Roundhouse build status changes
- image push/SBOM/signature/digest commit steps
- ArgoCD callbacks and deployment sync state
- Kubernetes rollout-state reconciliation
- preview lifecycle
- domain/junction provisioning
- addon/database/secret provisioning
- cron and one-off job execution
- Enclii `ops` and `providers` operation contract results

Provider and cluster remediation actions must flow through Enclii contracts, not
raw `kubectl`, `gh`, Cloudflare, Porkbun, or Hetzner tooling.

## Frontend Plan

### Data Model

Extend the project-card contract:

```ts
type CompactService = {
  // existing fields...
  processSummary?: ServiceProcessSummary;
  activeProcessCount?: number;
  lastProcess?: ProjectProcess;
};

type CompactProject = {
  // existing fields...
  processSummary?: ProjectProcessSummary;
  liveState?: "idle" | "running" | "failed" | "blocked" | "unknown";
};
```

Add a hook:

- `useProjectProcessFeed(projects)`

Responsibilities:

- fetch process summaries for the visible project IDs/slugs
- subscribe to the authenticated process SSE stream
- merge events by `id` and `correlation_id`
- keep a bounded in-memory timeline per project
- degrade to 10-15 second polling if SSE is unavailable
- expose `liveState`, counts, latest process, and grouped service summaries

### Project Card UX

Add three card-level surfaces:

1. **Process rail**
   - horizontal rail below the existing status/header zone
   - shows the last 3-5 active/recent processes
   - animated only for running/waiting states
   - failed/blocked states always visible

2. **Per-service process indicators**
   - small icon/status chip on each service row
   - CI/build/deploy/rollout failures visible even if the service is still
     serving healthy traffic
   - hover/click reveals branch, commit, age, phase, and links
   - must preserve the existing per-service domain row, including the service's
     prod/staging/preview/dev environment badge, instead of collapsing domains
     into a single project-level URL

3. **Interactive mini-feed drawer**
   - opens from the process rail or latest-process chip
   - groups events by service
   - lazy-loads the fuller project process history on open
   - links to lifecycle timeline, build status, deployment logs, GitHub run,
     service logs, or Enclii remediation action

List mode should render a compact equivalent in `ProjectRowCompact`: icon stack,
active count, and highest-severity state.

### Visual State Rules

Severity order:

1. `blocked`
2. `failed`
3. `running`
4. `waiting`
5. `queued`
6. `succeeded`
7. `unknown`
8. `idle`

Display rules:

- failed/blocked process state overrides healthy aggregate visuals
- running state pulses, but only one animation per card
- completed successful events age out of the rail after a short retention window
- accessibility labels must include service name, phase, status, and age
- cards must remain scannable; details belong in the drawer, not the main rail

## Interaction Plan

Process click targets:

| Process kind | Primary target |
|---|---|
| `git_push` | lifecycle timeline filtered to commit |
| `ci` | GitHub Actions run when available |
| `build` | unified build status or build logs |
| `image` / `digest` | release detail or deployment detail |
| `gitops_sync` / `deploy` / `rollout` | deployment detail and logs |
| `rollback` | deployment detail and rollback audit event |
| `preview` | preview URL/detail |
| `domain` | junction/domain management |
| `secret` | secret status, never secret values |
| `addon` / `database` | addon detail/provisioning status |
| `job` | job logs and last run |
| `operator` | Enclii ops operation result |

For failed or blocked processes, expose a remediation CTA that maps to an
Enclii operation contract dry run, for example:

- `enclii ops pods diagnose`
- `enclii ops apps diff`
- `enclii ops secrets external`
- `enclii providers github runs`

Mutation CTAs must remain plan-first and require explicit confirmation.

## Implementation Sequence

### P0 — Spec and Pure Helpers (implemented)

- add DTOs and mapping functions for process kind/status/severity
- add pure frontend helper tests for severity, age-out, and merge behavior
- document current process sources and missing producers

### P1 — Backend Read Model (foundation implemented)

- add project process summary DTOs and handlers
- query lifecycle events and service rollout state in batched repository calls
- extend coverage to CI runs, releases, deployments, and audit logs
- add API tests for active, failed, blocked, and empty-project cases
- register routes under existing protected project APIs

### P2 — Polling UI (implemented)

- add `useProjectProcessFeed` with summary polling first
- extend `CompactProject` and `CompactService`
- add `ProjectProcessRail`, `ServiceProcessIndicator`, and list-mode indicator
- add helper and component tests for visible process states, link targets,
  service grouping, and per-service domain row preservation

### P3 — Live SSE (snapshot stream implemented)

- add stream endpoint with heartbeat and interval refresh
- publish events from lifecycle callback paths and deployment updates
- add reconnect/backfill logic in the hook
- validate behavior behind Cloudflare Tunnel and local development proxy

### P4 — Drilldown and Remediation (drawer implemented)

- add interactive sheet drawer with lazy process history load
- link each process kind to the correct Enclii UI page or external provider URL
- add dry-run remediation CTAs through Enclii `ops`/`providers`
- preserve operation IDs for Selva/agent traceability

### P5 — Full Coverage

- add domain, addon, database, secret, cron, one-off job, and provider inventory
  event producers
- add process history retention and pruning policy
- add OpenAPI/SDK coverage after the read model stabilizes
- add dashboard-level filters for active/failed/blocked process state

## Acceptance Criteria

- project cards show active process counts for every service with running,
  waiting, failed, or blocked work
- no N+1 build-status calls from the projects page
- failed or blocked process state appears on the card within 5 seconds via SSE
  or within 15 seconds via polling fallback
- serving health and rollout state are visually distinct
- list mode exposes the same highest-severity process state
- stream reconnects and backfills missed events
- keyboard and screen-reader flows expose all process details
- failed/blocked remediation links route through Enclii contracts
- tests cover API aggregation, stream authorization/filtering, frontend state
  merging, severity ordering, and rendering

## Risks

- lifecycle, CI, release, deployment, and provider events may not yet share a
  strong correlation ID
- project cards can become noisy if the rail tries to show every detail
- SSE may require proxy buffering changes in production
- existing projects page already performs per-project service requests; adding
  process data must be batched to avoid worse dashboard load
- activity logs are useful for audit but insufficient as the primary process
  source because they do not model multi-stage build/deploy state

## Open Decisions

- whether to store normalized `project_processes` rows or compute from existing
  event tables for the first release
- whether the first stream should be project-scoped only or support all visible
  project IDs in a single request
- retention window for completed successful processes on cards
- exact remediation CTA policy for production projects
- whether Selva receives process events through the same SSE stream or a
  backend agent event bus
