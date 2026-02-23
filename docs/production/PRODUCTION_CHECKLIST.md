---
title: Production Checklist
description: Complete checklist for deploying Enclii to production
sidebar_position: 3
tags: [production, deployment, checklist, operations]
---

# Enclii Production Deployment Checklist

**Status:** Production Beta v0.1.0
**Last Updated:** February 2026
**Last Audit:** Wave 15 Session 13 (Feb 6, 2026)

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
- [x] 2-node k3s cluster (foundry-core + foundry-builder-01)
- [x] k3s v1.33.6+k3s1 on both nodes
- [x] Builder node tainted (builder=true:NoSchedule)
- [x] Cloudflare Tunnel ingress (2 replicas + PDB)
- [x] 28 tunnel routes configured
- [x] Zero exposed node ports

### Database & Caching
- [x] PostgreSQL 15 in-cluster (data namespace, Longhorn PVC)
- [x] Redis 7 in-cluster (data namespace)
- [x] Redis authentication via K8s Secret
- [x] PostgreSQL daily backup CronJob to R2
- [x] Longhorn backup target configured to R2

### Storage
- [x] Longhorn CSI v1.7.2 (sole default StorageClass)
- [x] local-path demoted from default
- [x] Longhorn backup to Cloudflare R2

### GitOps & CI/CD
- [x] ArgoCD App-of-Apps (16 apps)
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
- [ ] Default-deny for all remaining namespaces

### Image Security
- [x] Kyverno `restrict-image-registries` in **Enforce** mode
- [x] Kyverno `block-latest-ifnotpresent` active
- [x] Kyverno `require-probes` active
- [x] Kyverno `require-resources` active
- [x] Monitoring namespace PolicyException configured
- [ ] Cosign image signature verification (Audit mode)

### Secrets Management
- [x] Redis password in K8s Secret (not in git)
- [x] OIDC credentials in K8s Secret
- [x] ArgoCD webhook secret configured
- [x] GHCR credentials per namespace
- [x] ENCLII_CALLBACK_TOKEN in all 3 repos
- [ ] HashiCorp Vault / ExternalSecrets (future)

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
- [ ] PagerDuty/Opsgenie integration

### Endpoints (12/12 Healthy)
- [x] api.enclii.dev/health → 200
- [x] app.enclii.dev → 200
- [x] enclii.dev → 200
- [x] docs.enclii.dev → 200
- [x] admin.enclii.dev → 200
- [x] status.enclii.dev → 200
- [x] api.dhan.am/health → 200
- [x] app.dhan.am → 200
- [x] admin.dhan.am → 200
- [x] api.janua.dev/health → 200
- [x] app.janua.dev → 200
- [x] admin.janua.dev → 200

---

## Operational Hygiene

### Deployments
- [x] revisionHistoryLimit: 3 on all Deployments (18 total)
- [x] Rolling update strategy on all services
- [x] HPA configured for key services
- [x] PDB on stateful workloads

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

---

## Cost Summary

| Resource | Monthly Cost |
|----------|-------------|
| Hetzner AX41-NVME (foundry-core) | ~$50 |
| Cloudflare R2 (backups) | ~$5 |
| Cloudflare (tunnel, DNS, DDoS) | $0 |
| **Total** | **~$55/month** |

---

## Known Issues / Future Work

- [ ] ArgoCD v2.13 OCI Helm chart bug (2 apps show Unknown — Healthy)
- [ ] Image Updater ConfigMap OutOfSync (cosmetic, dual-ownership)
- [ ] Janua Database Backup workflow failing (separate from platform backups)
- [ ] PostgreSQL HA (Patroni/CloudNativePG) — when SLA > 99.9%
- [ ] Redis Sentinel — manifests staged
- [ ] Multi-node Longhorn — when additional storage nodes added
- [ ] Monitoring access restriction (Cloudflare Access or remove public routes)

---

**Document Version:** 2.0
**Last Audit:** Wave 15 Session 13 (Feb 6, 2026)
**Maintained By:** Platform Team
