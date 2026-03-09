# Changelog

All notable changes to the Enclii platform are documented here.

This project follows [Conventional Commits](https://www.conventionalcommits.org/) for changelog generation.

## [Unreleased]

### Added
- Cloudflare tunnel routes for vault.enclii.dev and analytics.enclii.dev (backends pending deploy)
- ArgoCD network-policies app for GitOps-managed NetworkPolicy enforcement
- PostHog Helm values, ArgoCD app, Go/frontend SDKs (pending cluster deploy)
- Vault Helm values, ArgoCD app, ESO ClusterSecretStore (pending cluster deploy)
- Status page 24h timeline history (PostgreSQL-backed, 15-min aggregation windows)
- Cluster operations deployment script (`scripts/cluster-ops-deploy.sh`)

### Fixed
- NetworkPolicy default-deny across all namespaces
- Cosign image verification (Audit mode, Enforce pending verification)

## [0.1.0] - 2026-01-25

### Added
- Switchyard control plane API (Go/Gin) — 107+ REST endpoints
- Switchyard web UI (Next.js) — project/service/deployment management
- Dispatch admin platform — fleet management, topology visualization
- CLI (`enclii` command) — deploy, logs, rollback, onboard
- Go SDK — API client and type definitions
- Build pipeline — GitHub webhooks + Buildpacks/Dockerfile detection
- ArgoCD App-of-Apps GitOps with self-heal
- Longhorn CSI storage (single-replica, multi-node ready)
- Cloudflare Tunnel zero-trust ingress
- Janua SSO integration (OIDC/RS256 JWT)
- Status pages — status.enclii.dev, status.madfam.io
- Self-service repo onboarding API
- Deployment lifecycle event tracking

---

For the full commit history, see: `git log --oneline`
