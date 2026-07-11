---
title: Enclii Documentation
description: Deploy, scale, and operate containerized services on infrastructure you own
sidebar_position: 1
tags: [overview, documentation, getting-started]
---

# Enclii

**Deploy, scale, and operate containerized services — on infrastructure you own.**

Open-source DevOps platform with production-grade Kubernetes, zero vendor lock-in, and Vercel/Railway/Heroku-style ergonomics.

> **[→ Deploy your first service in 5 minutes](./quickstart.md)**

---

## Pick your path

### 🚀 [Just try it →](./quickstart.md)

Install the CLI, sign in with GitHub, and deploy a live service in 5 minutes. No cluster, no YAML.

### 🔁 [Migrate from Vercel, Railway, or Heroku →](./guides/migrating.md)

10-minute migration guides with feature-parity tables, env var import scripts, and DNS cutover steps.

### ☸️ [Run your own cluster →](./guides/SELF_HOSTING.md)

Self-host Enclii end-to-end. Bare-metal or cloud, Kubernetes-native, GitOps-driven.

---

## Quick links

- [Template catalog](./templates/templates.md) — starter repos for Next.js, FastAPI, Go, Rails, and more
- [CLI reference](./cli/README.md) — every `enclii` command
- [Service spec](./reference/service-spec.md) — `service.yaml` field-by-field
- [Troubleshooting](./troubleshooting/) — build failures, deploy timeouts, auth issues
- [FAQ](./faq/) — billing, security, migration FAQs

---

## Platform status

**Program:** commercial launch (GA) program active · **Live at:** [app.enclii.dev](https://app.enclii.dev)

Current program state, readiness, and dates live in [COMMERCIAL_GA_TRACKER.md](./production/COMMERCIAL_GA_TRACKER.md) and the [readiness scorecard](./production/GA_READINESS_SCORECARD.md); the plan is the [GA master plan](./production/COMMERCIAL_GA_MASTER_PLAN.md). External “95% ready” copy is retired at Gate 5.

| Component | Status |
|-----------|--------|
| API (`api.enclii.dev`) | ✅ Running |
| Dashboard (`app.enclii.dev`) | ✅ Running |
| Admin (`admin.enclii.dev`) | ✅ Running |
| Auth (Janua SSO, `auth.madfam.io`) | ✅ Running |
| Build pipeline | ✅ GitHub webhooks + Paketo buildpacks |
| Docs (`docs.enclii.dev`) | ✅ Running |
| Status (`status.enclii.dev`) | ✅ Running |
| GitOps (ArgoCD, App-of-Apps) | ✅ Running |
| Storage (Longhorn CSI) | ✅ Running — bet C staging-proven |
| Previews / domains / billing enforce | ✅ Staging-proven (2026-05-23) |
| Observability (OTel tracing) | ✅ Running |
| Vault | 🟡 P1 — unseal + ESO sync (O-10) |
| Self-serve signup | 🟡 Wave 2 — disabled in prod until O-17 |
| SLA / support / legal | 🟡 Draft — Wave 4 publish |
| Managed databases | ⏳ Post-GA (bet D) |

---

## Documentation structure

This documentation is organized for two audiences: **users** who deploy services to Enclii, and **operators** who run the Enclii platform itself.

### For users

- **[Getting Started](./getting-started/QUICKSTART.md)** — local-dev loop, build tooling (for platform contributors)
- **[Guides](./guides/ONBOARDING_GUIDE.md)** — onboarding, testing, database operations, SSO
- **[CLI Reference](./cli/)** — every command with examples
- **[SDKs](./sdk/typescript/)** — TypeScript and Go client libraries
- **[Troubleshooting](./troubleshooting/)** and **[FAQ](./faq/)**

### For operators

- **[Architecture](./architecture/ARCHITECTURE.md)** — system design and decisions
- **[Infrastructure](./infrastructure/README.md)** — GitOps, Longhorn, Cloudflare, External Secrets
- **[Production](./production/PRODUCTION_CHECKLIST.md)** — deployment roadmap, gap analysis, anti-fragility
- **[GA program](./production/COMMERCIAL_GA_MASTER_PLAN.md)** — master remediation plan, tracker, scorecard, ops queue
- **[Runbooks](./runbooks/)** — incident response, cluster remediation, database recovery
- **[Security](./security/)** and **[Compliance](./compliance/)** — secret rotation, SOC2 mapping

### Implementation history

- **[Implementation notes](./implementation/)** — MVP, build pipeline, CLI completion reports
- **[Audits](./audits/)** and **[archived reports](./archive/)** — historical audit trail

---

## Navigation by role

| Role | Start here |
|------|-----------|
| **New user deploying a service** | [5-minute quickstart](./quickstart.md) |
| **Migrating from Vercel/Railway/Heroku** | [Migration index](./guides/migrating.md) |
| **Platform contributor (local dev)** | [Local quickstart](./getting-started/QUICKSTART.md) |
| **DevOps / SRE** | [Infrastructure](./infrastructure/README.md), [Runbooks](./runbooks/) |
| **Security engineer** | [Kyverno policies](./infrastructure/KYVERNO_POLICIES.md), [Secret rotation log](./security/SECRET_ROTATION_LOG.md) |
| **Frontend developer** | [Quickstart](./quickstart.md), [TypeScript SDK](./sdk/typescript/) |
| **Backend developer** | [Architecture](./architecture/ARCHITECTURE.md), [API docs](./architecture/API.md), [Testing guide](./guides/TESTING_GUIDE.md) |
| **Executive / CTO** | [GA scorecard](./production/GA_READINESS_SCORECARD.md), [GA master plan](./production/COMMERCIAL_GA_MASTER_PLAN.md), [Gap analysis](./production/GAP_ANALYSIS.md) |

---

## Getting help

- **CLI help:** `enclii --help` or `enclii <command> --help`
- **Docs:** you are here ([docs.enclii.dev](https://docs.enclii.dev))
- **Issues:** [github.com/madfam-org/enclii/issues](https://github.com/madfam-org/enclii/issues)
- **Status page:** [status.enclii.dev](https://status.enclii.dev)

---

## Contributing to the docs

When adding documentation:

1. **User-facing tutorials** → `guides/`
2. **Reference material** → `reference/` or `cli/commands/`
3. **Operator runbooks** → `runbooks/`
4. **Architecture and ADRs** → `architecture/`
5. **Historical reports** → `archive/`

Update `apps/docs-site/sidebars.ts` when adding a new top-level page.

---

**Last updated:** 2026-07-11 · **Documentation version:** 4.1 (GA program)
