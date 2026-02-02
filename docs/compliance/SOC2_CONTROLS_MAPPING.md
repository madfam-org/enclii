# SOC 2 Controls Mapping

Mapping of SOC 2 Trust Services Criteria to Enclii platform implementations.

**Last reviewed:** 2026-02-01
**Scope:** Enclii PaaS (2-node k3s cluster, control plane, build pipeline, web UI)

## CC1 -- Control Environment

| Control | Implementation | Evidence Source | Status |
|---------|---------------|----------------|--------|
| CC1.1 -- Organizational commitment to integrity and ethics | Code of conduct, conventional commit enforcement | `.github/CONTRIBUTING.md`, commit hooks via `make bootstrap` | Active |
| CC1.2 -- Board/management oversight | RBAC roles (superadmin, admin, operator) enforced at API and UI layers | `apps/switchyard-api/internal/middleware/`, `apps/dispatch/middleware.ts` | Active |
| CC1.3 -- Authority and responsibility | Role-based access with Janua SSO; admin domain restrictions | `apps/dispatch/contexts/AuthContext.tsx`, Janua OIDC config | Active |
| CC1.4 -- Competence commitment | PR review requirements, CI gate enforcement | `.github/workflows/`, ArgoCD sync policies | Active |
| CC1.5 -- Accountability | Audit logging on all API mutations | `apps/switchyard-api/internal/audit/middleware.go` | Active |

## CC2 -- Communication and Information

| Control | Implementation | Evidence Source | Status |
|---------|---------------|----------------|--------|
| CC2.1 -- Internal communication of objectives | CLAUDE.md, AI_CONTEXT.md, architecture docs | `CLAUDE.md`, `docs/architecture/` | Active |
| CC2.2 -- Internal communication of policies | Compliance docs, runbooks | `docs/compliance/`, `docs/production/` | Active |
| CC2.3 -- External communication | Status page, public docs site | `status.enclii.dev`, `docs.enclii.dev` | Active |

## CC3 -- Risk Assessment

| Control | Implementation | Evidence Source | Status |
|---------|---------------|----------------|--------|
| CC3.1 -- Risk identification | Vulnerability scanning (Trivy, gosec, govulncheck) | CI pipeline scan results, `scripts/` | Active |
| CC3.2 -- Fraud risk assessment | Admission policies block unauthorized images | `infra/k8s/policies/`, Kyverno policies | Active |
| CC3.3 -- Change-related risks | Canary deployments with automatic rollback at >2% error rate | `apps/switchyard-api/internal/reconciler/` | Active |
| CC3.4 -- Risk tolerance | Budget alerts at 80%, hard throttle at 100% for non-prod | Cost tracking service, Waybill module | Active |

## CC4 -- Monitoring Activities

| Control | Implementation | Evidence Source | Status |
|---------|---------------|----------------|--------|
| CC4.1 -- Ongoing monitoring | Prometheus metrics, Grafana dashboards, Jaeger tracing | `apps/switchyard-api/internal/monitoring/` | Active |
| CC4.2 -- Deficiency communication | Alert routing via Prometheus Alertmanager | Alertmanager config in `infra/k8s/production/` | Active |

## CC5 -- Control Activities

| Control | Implementation | Evidence Source | Status |
|---------|---------------|----------------|--------|
| CC5.1 -- Risk mitigation activities | Kyverno admission policies, NetworkPolicy isolation | `infra/k8s/policies/`, `infra/k8s/production/` | Active |
| CC5.2 -- Technology general controls | k3s RBAC, namespace isolation, pod security standards | k3s config, namespace manifests | Active |
| CC5.3 -- Policy deployment | ArgoCD GitOps with self-heal and auto-sync | `infra/argocd/root-application.yaml` | Active |

## CC6 -- Logical and Physical Access

| Control | Implementation | Evidence Source | Status |
|---------|---------------|----------------|--------|
| CC6.1 -- Logical access security | JWT/RS256 via Janua SSO, JWKS validation | `apps/switchyard-api/internal/auth/`, Janua OIDC | Active |
| CC6.2 -- User authentication | OAuth 2.0/OIDC with GitHub federation | `apps/switchyard-api/internal/auth/` | Active |
| CC6.3 -- Access authorization | RBAC (admin/developer/viewer), API key scoping | `apps/switchyard-api/internal/middleware/` | Active |
| CC6.4 -- Access restriction to protected assets | Namespace-level NetworkPolicy, Cloudflare tunnel (zero exposed ports) | `infra/k8s/production/cloudflared-unified.yaml` | Active |
| CC6.5 -- Access removal | Session management via Redis, SSO logout (RP-Initiated) | Redis session store, Janua logout endpoint | Active |
| CC6.6 -- System boundaries | Cloudflare edge TLS, zero NodePorts, tunnel-only ingress | Cloudflare tunnel config | Active |
| CC6.7 -- Transmission security | TLS everywhere (Cloudflare edge to tunnel), encrypted Redis/PG connections | Cloudflare TLS, k8s service mesh | Active |
| CC6.8 -- Unauthorized access prevention | Kyverno policies, pod security restricted profile | `infra/k8s/policies/dhanam-quota.yaml` | Active |

## CC7 -- System Operations

| Control | Implementation | Evidence Source | Status |
|---------|---------------|----------------|--------|
| CC7.1 -- Infrastructure monitoring | Prometheus/Grafana with SLO alerting (99.95% API, 99.9% builds) | Grafana dashboards, Alertmanager | Active |
| CC7.2 -- Incident detection | Error rate monitoring, automatic rollback at >2% for 2 minutes | Reconciler logic, Prometheus alerts | Active |
| CC7.3 -- Incident response | Rollback procedures, `enclii rollback` command | `packages/cli/internal/cmd/` | Active |
| CC7.4 -- Backup and recovery | Daily PostgreSQL backups to Cloudflare R2 | `apps/switchyard-api/internal/backup/postgres.go` | Active |
| CC7.5 -- Recovery testing | Backup restoration verification | Backup job logs | Planned |

## CC8 -- Change Management

| Control | Implementation | Evidence Source | Status |
|---------|---------------|----------------|--------|
| CC8.1 -- Change authorization | PR review + CI gates (lint, test, security scan) | GitHub PR workflows, `.github/workflows/` | Active |
| CC8.2 -- Change testing | Unit, integration, E2E test suites | `make test`, `make e2e`, Playwright | Active |
| CC8.3 -- Change deployment | ArgoCD GitOps pull-based sync with drift correction | `infra/argocd/` | Active |

## CC9 -- Risk Mitigation (Vendor and Business)

| Control | Implementation | Evidence Source | Status |
|---------|---------------|----------------|--------|
| CC9.1 -- Vendor risk management | Vendor assessments for Hetzner, Cloudflare, GitHub | `docs/compliance/VENDOR_RISK_ASSESSMENT.md` | Active |
| CC9.2 -- Supply chain security | Cosign image signing, SBOM generation (Syft) | `apps/switchyard-api/internal/signing/cosign.go`, `apps/switchyard-api/internal/sbom/syft.go` | Active |
| CC9.3 -- Business continuity | Multi-node ready architecture, Longhorn CSI, daily backups | Longhorn config, backup jobs | Active |

## Additional Trust Services Criteria

### Availability

| Control | Implementation | Status |
|---------|---------------|--------|
| Health checks on all services | `/health` endpoints, liveness/readiness probes | Active |
| Auto-scaling configuration | HPA definitions per service | Active |
| Zero-downtime deploys | RollingUpdate strategy via ArgoCD | Active |

### Confidentiality

| Control | Implementation | Status |
|---------|---------------|--------|
| Secrets management | Lockbox/Vault integration, Kubernetes Secrets | Active |
| Data classification | Four-level classification policy | Active |
| Encryption at rest | PVC encryption via Longhorn, R2 server-side encryption | Active |

### Processing Integrity

| Control | Implementation | Status |
|---------|---------------|--------|
| Input validation | API validation middleware | Active |
| Immutable releases | Git SHA provenance, signed images, SBOMs | Active |
| Idempotent reconciliation | Kubernetes controller reconcile loops | Active |
