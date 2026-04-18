# Enclii Status

Next.js status page hosting two deployments:

- `status.enclii.dev` — Enclii platform core
- `status.madfam.io` — MADFAM ecosystem (~50 services across 6 product families)

Internal name: `enclii-status`. Runs on port 4204 per
`CLAUDE.md#port-allocation`.

## Features

- 4-level graded status: Operational / Degraded / Partial / Major outage
- Auto-incident detection from probe history (threshold-configurable)
- 24h timeline with adaptive window sizing
- Atom feed (`/feed.xml`)
- Incident JSON API (`/api/incidents`) with ADMIN_SECRET-gated writes
- Dark/light theme
- **Component grouping by product family** (RFC 0002 S1) — two-level
  collapsible accordion, persisted per-user in localStorage
- **Statuspage-compatible `/api/v2/summary.json`** (RFC 0002 S2) — shim
  over existing probe + incident data, auto-detected by Better Uptime,
  Datadog, Slack `/statuspage` bots, etc.

## API

| Route | Method | Purpose |
|---|---|---|
| `/api/status` | GET / POST | Aggregated health + uptime |
| `/api/incidents` | GET / POST / PATCH / DELETE | Incident CRUD (ADMIN_SECRET for writes) |
| `/api/status/timeline` | GET | 24h per-service timeline |
| `/api/status/uptime` | GET | 24h/7d/30d/90d uptime percentages |
| `/api/status/record` | POST | Probe-result ingest |
| `/api/health` | GET | Liveness + DB readiness |
| `/api/v2/summary.json` | GET | **Statuspage-compatible summary shim** |
| `/feed.xml` | GET | Atom feed of incidents |
| `/trust` | GET | **Trust center** — public SLA / RPO / RTO commitments |
| `/trust/commitments.json` | GET | Machine-readable commitments snapshot |

### `/api/v2/summary.json`

Canonical Atlassian Statuspage schema. Consumers that auto-detect this
shape work against us with zero extra integration:

```jsonc
{
  "page":    { "id": "...", "name": "MADFAM", "url": "https://status.madfam.io", "time_zone": "Etc/UTC", "updated_at": "..." },
  "status":  { "indicator": "none|minor|major|critical|maintenance", "description": "..." },
  "components": [
    // component group
    { "id": "...", "name": "MADFAM Platform", "group": true, "components": ["...", "..."], ... },
    // leaf components
    { "id": "...", "name": "Karafiel API", "status": "operational|degraded_performance|partial_outage|major_outage|under_maintenance", "group_id": "...", "group": false, ... }
  ],
  "incidents": [
    { "id": "...", "name": "...", "status": "investigating|identified|monitoring|resolved", "impact": "none|minor|major|critical", "incident_updates": [...], "components": [...] }
  ],
  "scheduled_maintenances": [
    { "id": "...", "status": "scheduled|in_progress|verifying|completed", "scheduled_for": "...", "scheduled_until": "..." }
  ]
}
```

Status mapping (our → Statuspage):

| Internal | Statuspage component | Statuspage indicator |
|---|---|---|
| `operational` | `operational` | `none` |
| `degraded` | `degraded_performance` | `minor` |
| `maintenance` | `under_maintenance` | `maintenance` |
| `outage` | `major_outage` | `critical` |
| `unknown` | `operational` (optimistic) | `none` |

Incident lifecycle literals pass through unchanged (investigating /
identified / monitoring / resolved). See `lib/statuspage-v2.ts` for the
full mapping and the test snapshot in
`__tests__/lib/statuspage-v2.test.ts`.

## Configuration

Services are defined in a Kubernetes ConfigMap (`k8s/enclii/configmap.yaml`
or `k8s/madfam/configmap.yaml`) consumed via `SERVICES_CONFIG` env var.

Schema:

```jsonc
{
  "name": "Karafiel API",
  "url":  "https://api.karafiel.mx/health",      // probe URL
  "href": "https://api.karafiel.mx",              // optional user-facing link
  "group": "Karafiel",                            // fine-grained product
  "family": "MADFAM Platform",                    // optional — wraps groups
  "description": "Combat accounting API"
}
```

### Product families (RFC 0002 S1)

| Family | Groups |
|---|---|
| MADFAM Platform | Enclii, Janua, Dhanam, Tezca, Karafiel, Fortuna, PhyneCRM, Avala |
| Selva Swarm | Selva Office |
| Rondelio | Rondelio |
| Digital Fabrication | DigiFab, Yantra4D, Pravara MES, Primavera3D, Forj, Forgesight |
| Content & Creative | CEQ, Nuit, Bloom Scroll, Blueprint Harvester, Coforma |
| MADFAM Corporate | MADFAM Site, Platform |

When `family` is set on any service, the UI renders a two-level
accordion (family → group → services). When every service lacks
`family`, the page falls back to the existing single-level group
accordion — no behavioural change for older deployments.

## Development

```bash
pnpm dev        # http://localhost:4204
pnpm test       # Jest unit tests
pnpm build      # production build
```

## References

- RFC 0002 `internal-devops/rfcs/0002-status-page-evolution.md` — roadmap
  for the status page evolution, including S1-S8 backlog.
- Grandparent context: `apps/status/lib/status-config.ts` (colors,
  labels, priority) and `lib/types.ts` (domain model).
