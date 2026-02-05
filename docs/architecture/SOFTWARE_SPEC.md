# Enclii — SOFTWARE\_SPEC.md (v0.1)

**Status:** Draft for review
**Owner:** Aldo Ruiz Luna / Platform Team (Enclii)
**Date:** 2025‑08‑26
**Domain:** enclii.dev

---

## 0) Purpose & Scope

Enclii is MADFAM’s internal, open source DevOps platform that abstracts infra complexity and lets teams deploy, scale, and operate containerized services with high safety and low cognitive load. v1 runs on managed Kubernetes and managed databases; v2 targets portability to any cloud and eventually bare metal.

**In scope (v1):** Container services, zero‑downtime deploys, rollbacks, autoscaling (HPA/KEDA), logs/metrics/traces, cron jobs, one‑off jobs, volumes (basic), domains/TLS, preview environments, secrets management, cost showback, SSO/RBAC, CLI+UI.

**Out of scope (v1):** Global edge/CDN, self‑managed production databases, multi‑region active/active, BYO‑hardware data plane, private networking to on‑prem (treat as v2+).

---

## 1) Product Summary

* **One command to prod.** `enclii deploy` promotes a green build with canary and instant rollback.
* **Paved roads.** Golden templates for web/API/worker services with batteries included.
* **Operational guardrails.** SLOs, alerts, autoscaling, backups, and cost budgets by default.
* **Multi‑tenant by design.** Projects and environments isolate teams while sharing a platform.

**Primary personas**

* **App Dev:** ships services fast; wants previews, logs, rollbacks, secrets.
* **Platform/SRE:** cares about SLOs, incident response, policy as code, cost.
* **Eng Manager:** wants dashboards for deploy health, velocity, and spend.

---

## 2) Success Metrics & SLOs

* **Lead time for change:** ↓ ≥40% vs baseline.
* **Change failure rate:** ≤15% (P90), trending down.
* **MTTR:** ↓ ≥30% via rollbacks and runbooks.
* **Adoption:** ≥70% of services onboarded by GA.
* **Platform SLOs:**

  * **Control plane API/UI availability:** 99.95% monthly.
  * **Build subsystem availability:** 99.9% monthly.
  * **Data plane (per service):** 99.9% monthly (single region).
  * **Preview env provisioning:** P95 < 3 min.

---

## 3) Functional Requirements

1. **Projects & Environments**

   * Create projects; environments: `dev`, `stage`, `prod`, and ephemeral `preview‑*`.
   * Per‑env config/limits (CPU/RAM/egress/budget caps).
2. **Services**

   * Deploy containerized services (HTTP/TCP), workers, and jobs.
   * Health checks, startup/readiness probes.
   * Routes (host/path), TLS, custom domains, wildcard preview URLs.
   * Horizontal autoscaling (min/max replicas) and vertical requests/limits.
3. **Builds & Releases**

   * Build from repo via Nixpacks/Buildpacks or Dockerfile; push to registry.
   * Immutable **Release** objects with provenance (git SHA, SBOM, signature).
   * Canary/blue‑green strategies with automated analysis gates.
4. **Secrets & Config**

   * Namespaced secrets; env var injection; sealed at rest/in transit.
   * Rotation workflows and audit trail.
5. **Jobs**

   * Cron jobs (CRON spec); on‑demand one‑off jobs from service images.
   * Retry policies, TTLs for finished jobs, logs in the same stream.
6. **Volumes (basic)**

   * PVCs with selectable classes; non‑HA in v1; snapshot/backup schedule.
7. **Observability**

   * Logs (structured), metrics, distributed traces (OpenTelemetry).
   * SLO definitions per service; alert policies → Pager/Slack.
8. **Cost Showback**

   * Resource usage per project/env/service; daily digest; budget alerts.
9. **Access Control**

   * SSO (OIDC), team roles (Owner, Admin, Developer, ReadOnly).
   * API tokens with least privilege and expirations.
10. **CLI & UI**

    * CLI: `enclii` (alias `conductor`) provides init, up, deploy, logs, ps, secrets, scale, routes, jobs, rollback, cost.
    * UI mirrors CLI and adds dashboards and audit trails.

---

## 4) Non‑Functional Requirements

* **Security:** SBOMs, image signing (cosign), admission policy enforcement, secret scanning in CI.
* **Performance:** P95 deploy (build→running in stage) ≤ 8 min for typical Node/Go services.
* **Reliability:** Self‑healing reconcilers; idempotent operations; rate‑limited retries with jitter.
* **Portability:** Cloud‑agnostic base; no provider‑specific CRDs in control plane data model.
* **Usability:** DX NPS ≥ +20 at GA; task success rate ≥ 90% in usability tests.
* **Compliance‑ready:** Audit logs, change history, RBAC, backup + restore drills.

---

## 5) Architecture Overview

```
[Developers]
   └─> CLI (enclii) / UI (web) ─────────────┐
                                           │
                                    [Control Plane API]
                                           │
               ┌────────────── Reconcilers/Operators ──────────────┐
               │                                                    │
         [Build Subsystem]                                   [Kubernetes]
       (BuildKit/Buildpacks)                             (data plane, per region)
               │                                                    │
        [Container Registry]                           Ingress/TLS/DNS, HPA/KEDA,
               │                                      Deployments/Jobs/CronJobs,
           [SBOM/Signing]                               PVCs, OTel collectors

[Secret Manager] [Observability Stack] [Cost Engine]
  (Vault/1P)       (Prom, Loki, Tempo)    (usage scraper + reports)
```

**Key components (feature names):**

* **Enclii Switchyard:** control plane API + DB.
* **Enclii Conductor (CLI):** developer interface.
* **Enclii Roundhouse:** build/provenance/signing pipeline.
* **Enclii Junctions:** routing/ingress + certs + external‑dns.
* **Enclii Timetable:** cron/one‑off jobs.
* **Enclii Lockbox:** secrets service (Vault or 1Password Connect).
* **Enclii Signal:** logs/metrics/traces/SLOs.
* **Enclii Waybill:** showback and budget alerts.

---

## 6) Data Model (Control Plane)

**Entities**

* **Account** {id, name, ssoProvider, billingProfile}
* **Project** {id, accountId, name, slug, createdAt}
* **Environment** {id, projectId, name(enum), region, budgetLimit, policyRefs, createdAt}
* **Service** {id, envId, name, type(api/worker/job), specYAML, routes\[], alerts\[], createdAt}
* **Release** {id, serviceId, imageRef, buildId, gitSHA, createdAt, sbomURI, signature, status}
* **Runtime** {id, serviceId, releaseId, replicas, status, metricsRef, createdAt}
* **Secret** {id, scope(project/env/service), name, versionRef, rotatedAt}
* **Volume** {id, envId, name, sizeGi, class, backupPolicy}
* **Job** {id, envId, name, schedule|onDemand, imageRef, args, lastRun, nextRun}
* **Route** {id, envId, host, path, serviceId, tlsCertRef}
* **AuditEvent** {id, actor, action, entityRef, timestamp, payload}
* **CostSample** {id, envId, serviceId, cpuSeconds, memGiBHours, storageGiBHours, egressGiB, ts}

**Service Spec (YAML)** — source of truth stored versioned:

```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  project: solarpunk-saas
  environment: prod
  name: api
spec:
  runtime:
    image: ghcr.io/madfam/api:{{ git.sha }}
    cmd: ["node", "server.js"]
    ports: [3000]
    cpu: 500m
    memory: 512Mi
    replicas: { min: 2, max: 5 }
  health:
    readiness: { httpGet: /healthz, timeoutSeconds: 2 }
  routes:
    - host: api.enclii.dev
      path: /
  env:
    - name: NODE_ENV
      value: production
  envFrom:
    - secretRef: api-prod
  volumes:
    - name: uploads
      mountPath: /data
      size: 10Gi
      class: standard
  jobs:
    - name: nightly-report
      schedule: "0 2 * * *"
      args: ["node", "scripts/nightly.js"]
  autoscaling:
    hpa:
      cpuTargetUtilization: 70
```

---

## 7) Interfaces

### 7.1 CLI (canonical: `enclii`; alias: `conductor`)

**Top‑level verbs**

* `enclii init` → scaffold project and Service Spec from templates.
* `enclii up` → build + deploy current branch to a preview env; returns URL.
* `enclii deploy --env prod [--strategy canary --wait]` → promote release.
* `enclii logs <service> [-f] [--env <name>] [--grep <expr>]` → stream logs.
* `enclii ps [--env]` → show services, versions, replicas, health, P95, error rate.
* `enclii secrets set NAME=val --service api --env prod` → write secrets.
* `enclii scale api --min 2 --max 8 --env prod` → adjust HPA bounds.
* `enclii routes add --host api.example.com --service api --env prod`
* `enclii jobs run nightly-report --env prod`
* `enclii rollback api --to <releaseId>`
* `enclii cost --since 30d`
* `enclii auth login|token create --scopes ...`

**Exit codes**: 0 success; 10 validation error; 20 build failed; 30 deploy failed; 40 timeout; 50 auth error.

### 7.2 Control Plane API (REST)

* `POST /v1/projects` → create project
* `POST /v1/projects/{id}/environments` → add env
* `POST /v1/services` → create/update service (accepts Service YAML)
* `POST /v1/services/{id}/deployments` → trigger deploy
* `GET /v1/services/{id}/releases` → list releases
* `POST /v1/secrets/{scope}/` → write secret
* `GET /v1/logs?service=...&env=...` → stream logs (SSE/WebSocket)
* `GET /v1/metrics?service=...` → summary metrics
* `POST /v1/jobs/{id}/run` → run one‑off job
* `POST /v1/routes` → manage host/path bindings
* `GET /v1/cost?project=...&since=...` → usage report

**Auth:** OIDC bearer tokens; PATs (scoped, expiring).
**Rate limits:** 60 req/min per token; burst 120.

### 7.3 Web UI

* **Dashboards:** Project → Environments → Services → Releases/Runtime → Logs/Traces/Alerts.
* **Actions:** Deploy, Rollback, Scale, Rotate secrets, Create Job/Cron, Add Route, View Cost.
* **Audit:** Every action → AuditEvent with diff.

---

## 8) Build & Deploy Flow (Sequences)

**Deploy (main branch, prod)**

1. Git push to `main` → CI calls `enclii build` (Roundhouse) → image + SBOM → sign image.
2. Create `Release` → `POST /deployments` with `strategy=canary 10%/5m` and success metrics (error rate, P95).
3. Reconcilers apply manifests; Service becomes `Healthy` when probes pass and canary checks OK.
4. Auto‑promote 10%→100% if checks pass; on failure, auto‑rollback and page.

**Preview env (PR)**

* `enclii up` creates `preview‑{branch‑hash}` namespace, route `https://{hash}.project.dev.enclii.dev`, deploys release, comments URL on PR.

**Rollback**

* `enclii rollback api --to <releaseId>` swaps ReplicaSets, monitors SLOs for 10 min, and scales down the failed set.

---

## 9) Platform Components (Implementation Choices)

* **Kubernetes:** managed (GKE/EKS/AKS/DigitalOcean) in v1; one cluster/region; namespaces per env.
* **Ingress:** NGINX or Traefik; **cert‑manager** for ACME; **external‑dns** to Cloudflare.
* **Autoscaling:** HPA; **KEDA** for queue/event driven workers.
* **Builder:** BuildKit (rootless) with remote cache; optional Buildpacks; optional Nixpacks.
* **Registry:** GHCR or Harbor; vulnerability scans (Trivy).
* **Secrets:** HashiCorp Vault or 1Password Connect; `envFrom` via CSI/externalsecrets.
* **Observability:** OpenTelemetry → Prometheus (metrics), Loki (logs), Tempo (traces), Grafana (dashboards); Sentry for errors.
* **Policy:** Kyverno or OPA/Gatekeeper; cosign verify admission; SBOM required.
* **Costing:** scrape metrics and cloud billing; attribute by namespace/labels; daily report.

---

## 10) Security Model

* **Identity:** SSO (OIDC) with groups → RBAC roles (Owner/Admin/Dev/ReadOnly).
* **Secrets:** Zero plaintext in CI; short‑lived tokens; scheduled rotation; access logs.
* **Supply chain:** SBOM (CycloneDX), image signing; base image rotation every 30 days or on CVE.
* **Network:** Namespace isolation; NetworkPolicies; egress allow‑list; optional mTLS (v2).
* **Backups:** Control plane DB daily; volumes per policy; quarterly restore tests with signed reports.
* **Audit:** Immutable AuditEvent store; export to SIEM.

---

## 11) Cost & Budgets (Waybill)

* **Meters:** CPU‑seconds, RAM GiB‑hours, Volume GiB‑hours, Egress GiB.
* **Allocation:** namespace labels → Project/Env/Service; `CostSample` every 5 min.
* **Budgets:** per env caps; soft warn at 80%, hard throttle or sleep non‑prod at 100% (policy).
* **Reports:** daily Slack digest, monthly PDF to finance.

---

## 12) User Experience & Copy (EN/ES)

* **Status phrases:** “Clear to deploy / Despliegue autorizado”, “Holding at Junction / En espera en Enlace”.
* **Error phrasing:** precise, actionable: “Route conflict: host already bound in env ‘stage’.”
* **Empty states:** educate: links to templates and `enclii init`.

---

## 13) Operations

* **Runbooks:** rollback, hotfix, secret rotation, route conflict, crashloop, quota breach.
* **On‑call:** lightweight rotation; PagerDuty; severity matrix; comms templates.
* **DR:** restore drills; simulate cluster loss; RTO/RPO by env (prod RTO ≤ 30m, RPO ≤ 15m).
* **Change policy:** trunk‑based; protection rules; canary gates for Tier‑1.

---

## 14) Migration (Railway → Enclii)

1. **Inventory:** Services, env vars, CPU/RAM, routes, volumes, jobs, DB endpoints.
2. **Pilot:** choose Tier‑3 stateless service → port to Service Spec YAML → secrets into Lockbox.
3. **Shadow:** run on both platforms; header‑based mirroring; validate SLOs for 48–72h.
4. **Cutover:** DNS switch; keep Railway hot‑standby 7 days.
5. **Scale‑out:** Tier‑2, then Tier‑1 with explicit rollback windows.
6. **Decommission:** after 30 days stable + DR drill.

---

## 15) Testing & Acceptance Criteria

* **Unit:** control plane schema validation; CLI args; reconcilers idempotency.
* **Integration:** build→release→deploy pipeline; preview env provision; secret injection; route TLS issuance; HPA scale‑out under synthetic load.
* **E2E:**

  * *TC‑01:* `enclii up` creates preview and returns a working URL in <3 min P95.
  * *TC‑02:* canary 10%→100% with automated rollback on 5xx rate > 2% for 2 min.
  * *TC‑03:* secret rotation with zero downtime.
  * *TC‑04:* cron job fires on schedule; logs captured; retry on failure.
* **Security:** SBOM presence, signature verification, policy enforcement; CIS scans pass thresholds.
* **Perf:** P95 log tail < 2s; P95 metrics query < 1s.

**Acceptance for Alpha:** TC‑01..04 pass; SLOs met for 14 days; ≥1 prod service running.
**Acceptance for Beta:** ≥6 services onboarded, MTTR ↓30%, DevEx NPS +20.
**GA Gate:** ≥70% migration; zero Sev‑1s in last 30 days; incident postmortems complete.

---

## 16) Roadmap & Milestones

* **Alpha (4–6 wks):** Control plane, CLI (init/up/deploy/logs), preview envs, TLS/DNS, basic SSO, canary/rollback, OTel, Prom/Loki, single region.
* **Beta (6–10 wks):** KEDA, cost showback, cron jobs, volumes, RBAC, dashboards, budget caps.
* **GA (8–12 wks):** Multi‑region support, policy as code (Kyverno/OPA), SBOM+cosign gates, audit exports.

---

## 17) Risks & Mitigations

* **DB complexity:** keep DBs managed in v1; standardize drivers/backups; DR tests.
* **Scope creep:** enforce non‑goals; roadmap governance via success metrics.
* **Security drift:** automated scans; patch windows; admission policies.
* **People load:** dedicate a small platform squad; buy vs build defaults for non‑core.

---

## 18) Open Questions

1. Preferred cloud for v1 cluster(s) (GKE vs EKS vs DO)?
2. Secrets backend: Vault vs 1Password Connect?
3. Default regions required at GA (US‑East only vs add EU/LATAM)?
4. Error budget policy per tier (what triggers deploy freeze)?
5. Budget cap per month during Alpha/Beta?

---

## Appendix A — GitHub Actions (template)

```yaml
name: enclii-ci
on:
  pull_request:
  push:
    branches: [ main ]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - run: npm ci
      - run: enclii up --preview # build + deploy branch env

  release:
    needs: build
    if: github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: enclii deploy --env prod --strategy canary --wait
```

## Appendix B — Minimal Manifests Emitted by Reconcilers

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: prod-solarpunk-saas
spec:
  replicas: 2
  selector: { matchLabels: { app: api } }
  template:
    metadata: { labels: { app: api } }
    spec:
      containers:
        - name: api
          image: ghcr.io/madfam/api:v2025.08.26-14.02
          ports: [{ containerPort: 3000 }]
          envFrom: [{ secretRef: { name: api-prod } }]
          readinessProbe: { httpGet: { path: /healthz, port: 3000 }, periodSeconds: 5 }
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: { name: api, namespace: prod-solarpunk-saas }
spec:
  scaleTargetRef: { apiVersion: apps/v1, kind: Deployment, name: api }
  minReplicas: 2
  maxReplicas: 5
  metrics:
    - type: Resource
      resource: { name: cpu, target: { type: Utilization, averageUtilization: 70 } }
---
apiVersion: batch/v1
kind: CronJob
metadata: { name: nightly-report, namespace: prod-solarpunk-saas }
spec:
  schedule: "0 2 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: job
              image: ghcr.io/madfam/api:v2025.08.26-14.02
              args: ["node", "scripts/nightly.js"]
          restartPolicy: OnFailure
```

## Appendix C — RBAC Matrix (roles → permissions)

| Permission                          | Owner | Admin | Developer | ReadOnly |
| ----------------------------------- | :---: | :---: | :-------: | :------: |
| Create/Update Project               |   ✓   |   ✓   |     ✗     |     ✗    |
| Create/Update Env                   |   ✓   |   ✓   |     ✗     |     ✗    |
| Deploy/Scale/Rollback               |   ✓   |   ✓   |     ✓     |     ✗    |
| Secrets: Read/Write (service scope) |   ✓   |   ✓   |     ✓     |     ✗    |
| Secrets: Project/Env scope          |   ✓   |   ✓   |     ✗     |     ✗    |
| Manage Routes/Domains               |   ✓   |   ✓   |     ✓     |     ✗    |
| View Logs/Metrics/Traces            |   ✓   |   ✓   |     ✓     |     ✓    |
| Cost Reports                        |   ✓   |   ✓   |     ✓     |     ✓    |
| Manage Budgets                      |   ✓   |   ✓   |     ✗     |     ✗    |
| Manage Roles                        |   ✓   |   ✓   |     ✗     |     ✗    |

## Appendix D — Error Budget Policy (draft)

* Each service has an SLO and a monthly error budget. If a service exhausts 100% of its budget, deploys auto‑freeze until MTTR actions restore margin. Canary deploys remain allowed for fixes.
