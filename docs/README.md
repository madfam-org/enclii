---
title: Overview
description: Welcome to Enclii - an open source DevOps platform for containerized services
sidebar_position: 1
tags: [overview, documentation, getting-started]
---

# Enclii Documentation

**Welcome to the Enclii documentation!** This directory contains all technical documentation, guides, audit reports, and implementation notes.

## Current Status

**Production:** 95% Ready (Beta) | **Live at:** [app.enclii.dev](https://app.enclii.dev)

| Component | Status | Details |
|-----------|--------|---------|
| API | ✅ Running | api.enclii.dev |
| UI | ✅ Running | app.enclii.dev |
| Admin (Dispatch) | ✅ Running | admin.enclii.dev |
| Auth | ✅ Janua SSO | auth.madfam.io |
| Build Pipeline | ✅ Operational | GitHub webhooks + Buildpacks |
| Docs | ✅ Running | docs.enclii.dev |
| Status Page | ✅ Running | status.enclii.dev, status.madfam.io (24h timeline history) |
| GitOps | ✅ ArgoCD | App-of-Apps (10 apps), auto-sync + self-heal |
| Storage | ✅ Longhorn | CSI storage (single-node; ready for scaling) |
| NetworkPolicies | ✅ ArgoCD App | Default-deny per namespace |
| Vault | ⏳ Staged | Helm + ArgoCD app + tunnel route ready, pending cluster deploy |
| PostHog | ⏳ Staged | Helm + ArgoCD app + tunnel route + SDKs ready, pending cluster deploy |
| GPU Prep | ✅ Ready | Manifests staged, pending nodes |

## Quick Start

**New to Enclii?** Start here:
1. [Quickstart Guide](./getting-started/QUICKSTART.md) - Get up and running in 5 minutes
2. [Development Setup](./getting-started/DEVELOPMENT.md) - Set up your development environment
3. [Production Checklist](./production/PRODUCTION_CHECKLIST.md) - Deployment verification

**Want to understand the architecture?** Read:
- [Architecture Overview](./architecture/ARCHITECTURE.md)
- [API Documentation](/api-reference/)

**Need help?** Check:
- [Troubleshooting](./troubleshooting/) - Solutions to common problems
- [FAQ](./faq/) - Frequently asked questions

**Building integrations?** Use:
- [TypeScript SDK](./sdk/typescript/) - Programmatic access to Enclii
- [CLI Reference](./cli/) - Command-line interface

## Documentation Structure

### 📚 Getting Started
New developer onboarding and initial setup guides.

- [Quickstart Guide](./getting-started/QUICKSTART.md) - Quick introduction and setup
- [Development Setup](./getting-started/DEVELOPMENT.md) - Complete development environment configuration
- [Build Setup](./getting-started/BUILD_SETUP.md) - Build system and tooling guide

### 🏗️ Architecture
System design, architecture decisions, and API references.

- [Architecture Overview](./architecture/ARCHITECTURE.md) - System architecture and design patterns
- [API Documentation](./architecture/API.md) - REST API reference and examples
- [Blue Ocean Roadmap](./architecture/BLUE_OCEAN_ROADMAP.md) - Future architecture plans

### 📖 Guides
User guides for common tasks and migrations.

- [Onboarding Guide](./guides/ONBOARDING_GUIDE.md) - Zero-touch repo onboarding
- [Railway Migration Guide](./guides/RAILWAY_MIGRATION_GUIDE.md) - Migrating from Railway
- [Vercel Migration Guide](./guides/VERCEL_MIGRATION_GUIDE.md) - Migrating from Vercel
- [Heroku Migration Guide](./guides/HEROKU_MIGRATION_GUIDE.md) - Migrating from Heroku
- [Testing Guide](./guides/TESTING_GUIDE.md) - Writing and running tests
- [Database Operations](./guides/database-operations.md) - Database management and migrations
- [CLI Auth Setup](./guides/cli-auth-setup.md) - CLI authentication configuration
- [SSO Deployment](./guides/sso-deployment.md) - SSO configuration and deployment

### 🆘 Troubleshooting & FAQ
Get help with common issues and answers to frequent questions.

- [Troubleshooting Index](./troubleshooting/) - Common problems and solutions
- [API Errors](./troubleshooting/api-errors.md) - API error codes and fixes
- [Build Failures](./troubleshooting/build-failures.md) - Build pipeline troubleshooting
- [Deployment Issues](./troubleshooting/deployment-issues.md) - Deployment troubleshooting
- [Auth Problems](./troubleshooting/auth-problems.md) - Authentication issues
- [FAQ](./faq/) - Frequently asked questions

### 📦 SDKs
Client libraries for programmatic access.

- [TypeScript SDK](./sdk/typescript/) - Full TypeScript/JavaScript SDK
- Go SDK: `packages/sdk-go/` — Go client library

### 🚀 Production
Production deployment, readiness, and operational guides.

- [Production Checklist](./production/PRODUCTION_CHECKLIST.md) - Production readiness assessment
- [Production Deployment Roadmap](./production/PRODUCTION_DEPLOYMENT_ROADMAP.md) - Deployment timeline and milestones
- [Gap Analysis](./production/GAP_ANALYSIS.md) - Feature comparison with Vercel and Railway

### ☸️ Infrastructure
GitOps, storage, compute, and Kubernetes infrastructure. **[Infrastructure Index →](./infrastructure/README.md)**

**Core Infrastructure (Jan 2026):**
- [GitOps with ArgoCD](./infrastructure/GITOPS.md) - App-of-Apps pattern, self-heal, sync operations
- [Storage with Longhorn](./infrastructure/STORAGE.md) - Replicated CSI, StorageClasses, backup/recovery
- [Cloudflare Integration](./infrastructure/CLOUDFLARE.md) - Zero-trust ingress, tunnel route automation
- [External Secrets](./infrastructure/EXTERNAL_SECRETS.md) - Secret sync from external providers

**Configuration Files:**
- ArgoCD Apps: `infra/argocd/` — GitOps App-of-Apps configuration
- Longhorn Values: `infra/helm/longhorn/` — Helm values for storage
- GPU Node Setup: `infra/k8s/base/gpu/` — NVIDIA device plugin and tolerations
- Kaniko Builds: `apps/roundhouse/k8s/kaniko-job-template.yaml` — Secure rootless container builds
- Cloudflare Tunnel: `infra/k8s/production/cloudflared-unified.yaml` — Tunnel manifest
- ARC Runners: `infra/argocd/apps/arc-runners.yaml` — GitHub Actions self-hosted runners

### 🔍 Audits
Browser-based audit reports from January 2026 are available in the `audits/` directory.

> **Note:** Detailed audit reports (security, infrastructure, codebase, UI, dependencies, testing, technical debt) were produced during earlier development phases and have been archived. See `docs/archive/audits-nov-2025/` for the full set of historical audit documents.

### 🛠️ Implementation
Implementation status reports and strategy documents.

- [Build Pipeline Implementation](./implementation/BUILD_PIPELINE_IMPLEMENTATION.md)
- [CLI Implementation Complete](./implementation/CLI_IMPLEMENTATION_COMPLETE.md)
- [MVP Implementation](./implementation/MVP_IMPLEMENTATION.md)
- [Immediate Priorities Implementation](./implementation/IMMEDIATE_PRIORITIES_IMPLEMENTATION.md)
- [Blue Ocean Implementation Status](./implementation/BLUE_OCEAN_IMPLEMENTATION_STATUS.md)
- [Bootstrap Auth Strategy](./implementation/BOOTSTRAP_AUTH_STRATEGY.md)
- [Main Integration Complete](./implementation/MAIN_INTEGRATION_COMPLETE.md)

### 📦 Archive
Historical reports, completed progress tracking documents, and design artifacts are stored in the `archive/` directory. These are excluded from the docs site build but remain available in the repository for reference.

## Navigation by Role

| Role | Start Here |
|------|-----------|
| **Executives / CTOs** | [Production Checklist](./production/PRODUCTION_CHECKLIST.md), [Gap Analysis](./production/GAP_ANALYSIS.md) |
| **Engineering Managers** | [Production Deployment Roadmap](./production/PRODUCTION_DEPLOYMENT_ROADMAP.md), [Testing Guide](./guides/TESTING_GUIDE.md) |
| **DevOps / SRE** | [Infrastructure Index](./infrastructure/README.md), [Runbooks](./runbooks/CLUSTER_REMEDIATION_OPS.md) |
| **Security Engineers** | [Kyverno Policies](./infrastructure/KYVERNO_POLICIES.md), [Secret Rotation Log](./security/SECRET_ROTATION_LOG.md) |
| **Frontend Developers** | [Quickstart](./getting-started/QUICKSTART.md), [TypeScript SDK](./sdk/typescript/) |
| **Backend Developers** | [Architecture](./architecture/ARCHITECTURE.md), [API Docs](./architecture/API.md), [Testing Guide](./guides/TESTING_GUIDE.md) |

## Core Documentation (Root Directory)

The following essential documents are located in the repository root:

- `README.md` (repo root) — Main project README and overview
- `CLAUDE.md` (repo root) — Instructions for Claude Code AI assistant
- `SOFTWARE_SPEC.md` (repo root) — Complete software specification

## Contributing to Documentation

When adding new documentation:

1. **Getting Started:** Add to `getting-started/` for onboarding content
2. **Architecture:** Add to `architecture/` for system design docs
3. **Guides:** Add to `guides/` for how-to guides and tutorials
4. **Production:** Add to `production/` for deployment and operations
5. **Audits:** Add to appropriate `audits/` subdirectory
6. **Implementation:** Add to `implementation/` for status reports
7. **Archive:** Move completed/historical docs to `archive/`

**Remember to update this README.md when adding new documentation!**

## Documentation Standards

- Use clear, descriptive filenames in UPPERCASE with underscores
- Include a summary/overview at the top of each document
- Add navigation links to related documents
- Keep README files in subdirectories for complex sections
- Archive outdated documentation rather than deleting it

---

## Related Documentation

- **API Reference**: [OpenAPI Documentation](./api-reference/)
- **CLI Reference**: [CLI Commands](./cli/)
- **TypeScript SDK**: [SDK Documentation](./sdk/typescript/)
- **Troubleshooting**: [Common Issues](./troubleshooting/)
- **FAQ**: [Frequently Asked Questions](./faq/)

---

**Last Updated:** 2026-03-20
**Documentation Version:** 3.2 (Broken Links Fix)
