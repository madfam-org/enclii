---
title: Production Checklist
description: Complete checklist for deploying Enclii to production
sidebar_position: 3
tags: [production, deployment, checklist, operations]
---

# Enclii Production Deployment Checklist

**Status:** Production Release Candidate — **Commercial GA program active**; current program state lives in [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md)  
**GA program:** [COMMERCIAL_GA_MASTER_PLAN.md](./COMMERCIAL_GA_MASTER_PLAN.md) · [GA_READINESS_SCORECARD.md](./GA_READINESS_SCORECARD.md)  
**Last Updated:** July 11, 2026  
**Last Audit:** Gap Remediation Sprint (Mar 16, 2026)

> **Boundary checkpoint (2026-07-11, platform ops):** Public-safe deployment
> checklist — no secrets or private production topology. Live GA status/task
> state lives in the canonical trackers above; private operational detail and
> sink live in `internal-devops`. Policy: `docs/PUBLIC_REPO_BOUNDARY.md`
> (repo-boundary contract).

---

## Pre-Deployment Checklist

### 1. Accounts & Credentials
- [x] **Hetzner Cloud account** created
- [x] **Hetzner API token** generated (Read & Write permissions)
- [x] **Cloudflare account** created
- [x] **Cloudflare API token** generated (Zone:DNS:Edit, Zone:Zone:Read, Account:Tunnel:Edit, Account:R2:Edit)
- [x] **Cloudflare R2** enabled and API keys generated
- [x] **Domain** added to Cloudflare (DNS managed by Cloudflare)
- [x] **GitHub OAuth** configured for repo imports
- [x] **GHCR credentials** (long-lived PAT for imagePullSecrets)

### 2. Local Tools
- [x] `terraform` >= 1.5.0
- [x] `kubectl`
- [x] `hcloud` CLI
- [x] `cloudflared`
- [x] `jq`
- [x] `gh` CLI (GitHub)

### 3. Configuration
- [x] `terraform.tfvars` filled (no `YOUR_*` placeholders)
- [x] Management IP in `management_ips` for SSH access
- [x] Datacenter location selected

---

## Infrastructure Status

### Compute & Kubernetes
- [x] 3-node k3s cluster (foundry-cp [control-plane] + foundry-worker-01 [worker] + foundry-builder-01 [builder])
- [x] k3s v1.33.7+k3s3 on both nodes
- [x] Builder node tainted (builder=true:NoSchedule)
- [x] Cloudflare Tunnel ingress (2 replicas + PDB)
- [x] 28 tunnel routes configured
- [x] Zero exposed node ports

### Database & Caching
- [x] PostgreSQL 15 in-cluster (data namespace, Longhorn PVC)
- [x] Redis 7 in-cluster (data namespace)
- [x] Redis authentication via K8s Secret
- [x] PostgreSQL daily backup CronJob to R2
- [x] PostgreSQL HA CNPG WAL/base backups configured to R2
- [x] Longhorn backup target configured to R2

### Storage
- [x] Longhorn CSI v1.7.2 (sole default StorageClass)
- [x] local-path demoted from default
- [x] Longhorn backup to Cloudflare R2

### GitOps & CI/CD
- [x] ArgoCD App-of-Apps (10 apps in `infra/argocd/apps/`)
- [x] ArgoCD self-heal enabled
- [x] GitHub webhook CI/CD operational
- [x] Auto-deploy pipeline (enclii, dhanam, janua)
- [x] Kustomize digest commit pattern working
- [x] Concurrent digest commit retry loop
- [x] Deployment lifecycle event tracking
- [x] Emergency deploy workflow

### Authentication
- [x] Janua SSO (OIDC/OAuth 2.0 with RS256 JWT)
- [x] Admin user created (admin@madfam.io)
- [x] 2 OAuth clients registered (CLI + Platform)
- [x] RBAC with admin/developer/viewer roles
- [x] Session management via Redis
- [x] SSO Logout (RP-Initiated)

---

## Security Checklist

### Network Security
- [x] Zero exposed node ports (all via Cloudflare Tunnel)
- [x] NetworkPolicies for enclii namespace
- [x] NetworkPolicies for dhanam namespace (default-deny + allow)
- [x] NetworkPolicies for janua namespace (default-deny + allow)
- [x] NetworkPolicies for tezca namespace (default-deny + allow)
- [x] NetworkPolicies for data namespace (default-deny + allow)
- [x] NetworkPolicies for status namespace (default-deny + allow)
- [x] Cloudflare Zero Trust ingress
- [x] Default-deny for all 14 workload namespaces; 6 infra namespaces exempt by design (`enclii.dev/type: infrastructure`)
- [x] Ecosystem NetworkPolicies auto-generated from `enclii.yaml` `network:` section (zero-touch — Session 96)
- [x] Onboarding handler applies NetworkPolicies via K8s API (replaces git-commit path — Session 97)
- [x] PgBouncer egress type added to netpolicy generator (port 6432 — Session 97)

### Image Security
- [x] Kyverno `restrict-image-registries` in **Enforce** mode
- [x] Kyverno `block-latest-ifnotpresent` active
- [x] Kyverno `require-probes` active
- [x] Kyverno `require-resources` active
- [x] Monitoring namespace PolicyException configured
- [x] Cosign image signature verification (Enforce mode in git, opt-in per namespace via `enclii.dev/verify-signatures` label)

### Secrets Management
- [x] Redis password in K8s Secret (not in git)
- [x] OIDC credentials in K8s Secret
- [x] ArgoCD webhook secret configured
- [x] GHCR credentials per namespace
- [x] ENCLII_CALLBACK_TOKEN in all 3 repos
- [x] HashiCorp Vault (git-ready: 19 ExternalSecrets, Helm, ArgoCD app, NetworkPolicies — PR #64 merged). Cluster deploy pending — see `docs/runbooks/CLUSTER_REMEDIATION_OPS.md`

### API Error Handling
- [x] Internal errors (500) return generic message, never leak `err.Error()`
- [x] Recovery middleware strips panic details from responses
- [x] 39 error leakage sites sealed with `middleware.AbortInternal`
- [x] Error handler test coverage: 18 tests in `error_handler_test.go`

### Authentication & Authorization Hardening
- [x] CSRF stateless double-submit cookie (multi-replica safe)
- [x] Global IP-based rate limiter on all public endpoints
- [x] CORS startup validation (fatal if ENCLII_ALLOWED_ORIGINS empty in production, SEC-003)
- [x] Admin email RBAC restricted to configured OIDC issuer only (SEC-007)
- [x] Dispatch admin JWT verification via Janua JWKS (replaces cookie trust)

### Pod Security
- [x] PostgreSQL: seccompProfile, capability restrictions
- [x] Redis: runAsNonRoot, read-only where possible
- [x] All app pods: runAsNonRoot, drop ALL capabilities
- [x] Cloudflared: restricted security context

---

## Backup & Recovery

### PostgreSQL
- [x] Daily CronJob (3 AM UTC) → Cloudflare R2
- [x] pg_dumpall (all databases: enclii, janua, dhanam)
- [x] 30-day retention with automatic cleanup
- [x] Manual backup Job template available
- [x] Restore drill Job configured (data namespace)
- [x] Backup-verify weekly CronJob (Sundays 4 AM UTC)

### K3s Datastore
- [x] Daily CronJob (1:30 AM UTC) → Cloudflare R2
- [x] Archives state.db, TLS certs, cluster token via nsenter
- [x] 7-day retention
- [x] Kyverno PolicyException for hostPID/privileged

### GitHub Repositories
- [x] Daily CronJob (1:00 AM UTC) → Cloudflare R2
- [x] Mirror + bundle all madfam-org repos
- [x] 7-day retention
- [ ] Create `github-backup-credentials` secret on cluster — see [Cluster Ops Runbook §2](../runbooks/CLUSTER_REMEDIATION_OPS.md#section-2--backup-credential-secrets-p1)

### Cloudflare Configuration
- [x] Daily CronJob (1:15 AM UTC) → Cloudflare R2
- [x] DNS records, tunnel configs, zone settings
- [x] 30-day retention
- [ ] Create `cloudflare-api-credentials` secret on cluster — see [Cluster Ops Runbook §2](../runbooks/CLUSTER_REMEDIATION_OPS.md#section-2--backup-credential-secrets-p1)

### ArgoCD Secrets
- [x] Weekly CronJob (Sundays 2:00 AM UTC) → Cloudflare R2
- [x] Secrets, ConfigMaps, repo creds, applications, AppProjects
- [x] 12-week retention
- [x] RBAC: ServiceAccount + Role + RoleBinding for cross-namespace access

### Backup Alerting
- [x] CronJobFailed scope expanded to `data` namespace
- [x] K3sBackupMissing alert (25h threshold, critical)
- [x] GitHubBackupMissing alert (25h threshold, critical)
- [x] CloudflareBackupMissing alert (25h threshold, warning)
- [x] BackupJobFailed alert (all named backup jobs, critical)

### Longhorn Volumes
- [x] Backup target: s3://enclii-backups@auto/
- [x] Credential secret configured
- [x] Daily local snapshots (retain 7)
- [x] Daily R2 backups (retain 7)
- [x] Weekly R2 backups (retain 4)
- [ ] Tested volume restore

---

## Monitoring & Observability

### Metrics
- [x] Prometheus deployed (monitoring namespace)
- [x] Grafana deployed
- [x] AlertManager deployed
- [x] Client SLO recording rules (ConfigMap-based)
- [x] Node-exporter DaemonSet (CPU, memory, disk metrics)
- [x] Redis-exporter (data namespace)
- [x] Postgres-exporter (data namespace)
- [x] Alerting rules for disk, CPU, memory (node-exporter based)
- [x] Platform infrastructure PrometheusRules (Postgres, Redis, API, tunnel, nodes, pods, backups)
- [x] ServiceMonitor for cloudflared tunnel metrics
- [x] ServiceMonitor for waybill cost tracking metrics
- [ ] PagerDuty/Opsgenie integration

### Endpoints (20/20 Healthy)
- [x] api.enclii.dev/health → 200
- [x] app.enclii.dev → 200
- [x] enclii.dev → 200
- [x] docs.enclii.dev → 200
- [x] admin.enclii.dev → 200
- [x] status.enclii.dev → 200
- [x] status.madfam.io → 200
- [x] api.dhan.am/health → 200
- [x] app.dhan.am → 200
- [x] admin.dhan.am → 200
- [x] api.janua.dev/health → 200
- [x] app.janua.dev → 200
- [x] admin.janua.dev → 200
- [x] agents-api.madfam.io → 200
- [x] agents.madfam.io → 307 (login redirect)
- [x] agents-admin.madfam.io → 307 (login redirect)
- [x] agents-ws.madfam.io/health → 200
- [x] agents-gw.madfam.io → 502 (expected: background worker, no HTTP)
- [x] mes.madfam.io → 200
- [x] mes-api.madfam.io/health → 200

---

## Operational Hygiene

### Deployments
- [x] revisionHistoryLimit: 3 on all Deployments (18 total)
- [x] Rolling update strategy on all services
- [x] HPA configured for key services
- [x] PDB on stateful workloads
- [x] docs-site, roundhouse-worker, roundhouse-api bumped to 2 replicas (zero-downtime updates)

### Resource Management
- [x] ResourceQuota on enclii namespace
- [x] ResourceQuota on dhanam namespace (10 CPU limit)
- [x] LimitRange on enclii namespace
- [x] Resource requests/limits on all containers

---

## Services Deployed

| Service | Domain | Port | Status |
|---------|--------|------|--------|
| Switchyard API | api.enclii.dev | 4200 | Running |
| Switchyard UI | app.enclii.dev | 4201 | Running |
| Dispatch (Admin) | admin.enclii.dev | 4203 | Running |
| Status Page | status.enclii.dev | 4204 | Running |
| Docs Site | docs.enclii.dev | 8080 | Running |
| Landing Page | enclii.dev | 80 | Running |
| Dhanam API | api.dhan.am | 4300 | Running |
| Dhanam Admin | admin.dhan.am | 3400 | Running |
| Dhanam Web | app.dhan.am | 4200 | Running |
| Janua API | api.janua.dev | - | Running |
| Janua Dashboard | app.janua.dev | - | Running |
| Janua Admin | admin.janua.dev | - | Running |
| Status (madfam) | status.madfam.io | 4204 | Running |
| Selva Nexus API | agents-api.madfam.io | 4300 | Running |
| Selva Office UI | agents.madfam.io | 4301 | Running |
| Selva Admin | agents-admin.madfam.io | 4302 | Running |
| Selva Colyseus | agents-ws.madfam.io | 4303 | Running |
| Selva Gateway | agents-gw.madfam.io | 4304 | Running (no HTTP) |
| Selva Workers | - | - | Running (background) |
| MES Web | mes.madfam.io | 4501 | Running |
| MES API | mes-api.madfam.io | 4500 | Running |

---

## Cost Summary

See internal-devops for cost breakdown.

---

## Production Hardening (Wave 1+2, Mar 2026)

### Stub Implementations Replaced
- [x] Backup checksum — SHA256 (was placeholder string)
- [x] Backup S3 upload/download — aws CLI exec (was no-op)
- [x] Cloudflare Access policy CRUD — real CF API calls (was fake policy IDs)
- [x] Function invocation proxy — HTTP reverse proxy (was curl hint)
- [x] Status page incident database — Postgres-backed CRUD (was in-memory `[]`)
- [x] Observability uptime — computed from deployment history (was hardcoded 99.9/95/0)
- [x] Billing page — fetches live data from API (was hardcoded "Pro Plan" / fake dates)
- [x] Preview cleanup — CF tunnel route removal on PR close

---

## Known Issues / Future Work

- [ ] ArgoCD multi-source OCI Helm revision resolution bug — persists in v3.2.5 (2 ARC apps show Unknown/Healthy, pods functional)
- [x] ~~Image Updater ConfigMap OutOfSync~~ — Fixed: removed from cluster (unused, CI handles digests)
- [ ] Janua Database Backup workflow failing (separate from platform backups)
- [ ] PostgreSQL HA (Patroni/CloudNativePG) — when SLA > 99.9%
- [ ] Redis Sentinel — manifests staged
- [ ] Multi-node Longhorn — when additional storage nodes added
- [ ] Monitoring access restriction (Cloudflare Access or remove public routes)
- [ ] Longhorn EXT4 filesystem corruption pattern (5 incidents, manual PVC recreation needed) — see [Longhorn Recovery Runbook](../runbooks/LONGHORN_VOLUME_RECOVERY.md)
- [ ] PostHog Helm chart v30.46 broken (using Cloudflare Worker proxy to PostHog Cloud)
- [ ] ESO CRD migration v0.9.11→v0.16.2 deferred — see [Cluster Ops Runbook §7](../runbooks/CLUSTER_REMEDIATION_OPS.md#section-7--eso-crd-migration-plan-p1-deferred)
- [ ] KEDA runtime for serverless functions (operator + HTTP add-on) — ArgoCD app staged

---

**Document Version:** 4.0
**Last Audit:** Full Platform Remediation (Mar 15, 2026)
**Maintained By:** Platform Team
