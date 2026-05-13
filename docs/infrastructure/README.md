---
title: Infrastructure Overview
description: Production infrastructure components and architecture documentation
sidebar_position: 1
tags: [infrastructure, kubernetes, argocd, longhorn, cloudflare]
---

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


# Infrastructure Documentation

**Last Updated:** January 2026

This section documents Enclii's production infrastructure components deployed in January 2026.

> **Current State:** Running on a single Hetzner dedicated server. Infrastructure (Longhorn, ArgoCD) is prepared for multi-node scaling when additional nodes are added.

## Contents

| Document | Description |
|----------|-------------|
| [GitOps with ArgoCD](./GITOPS.md) | GitOps deployment management using App-of-Apps pattern |
| [Storage with Longhorn](./STORAGE.md) | Block storage (single-node; prepared for multi-node scaling) |
| [Cloudflare Integration](./CLOUDFLARE.md) | Zero-trust ingress, tunnel route automation, DNS |
| [External Secrets](./EXTERNAL_SECRETS.md) | Secret synchronization from external providers |
| [ArgoCD Known Issues](./ARGOCD_KNOWN_ISSUES.md) | Known bugs, workarounds, and upstream fix proposals |

## Quick Reference

### Control Plane Rule

Use Enclii for infrastructure manipulation. `enclii ops` is the operator entry
point for Kubernetes, ArgoCD, Longhorn, Kyverno, ExternalSecrets, Vault, and
ARC workflows. `enclii providers` is the entry point for GitHub, Cloudflare,
Porkbun, and Hetzner workflows.

Direct `kubectl`, `helm`, SSH, provider CLI/API commands, `docker exec`, and
direct container access are not routine operating procedures. They are allowed
only for platform bootstrap or documented break-glass emergencies when Enclii
is unavailable or lacks an implemented adapter.

### Check Infrastructure Health

```bash
# ArgoCD application status
enclii ops apps status --namespace argocd

# Longhorn volumes
enclii ops storage longhorn --namespace longhorn-system

# Cloudflare tunnel
enclii providers cloudflare tunnels

# External secrets sync
enclii ops secrets external --namespace enclii
```

### Break-Glass Scope

Break-glass access is intentionally not a routine command reference. If Enclii
cannot perform a required recovery action, record the actor, reason, target
service/environment, commands executed, result, and the adapter gap that must
be implemented in Enclii. Prefer adding or using the Enclii adapter before
touching production containers directly.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                      Cloudflare Edge                             │
│              (TLS, DDoS, WAF, Global Load Balancing)             │
└─────────────────────────────────────────────────────────────────┘
                              │
                     Cloudflare Tunnel
                              │
┌─────────────────────────────────────────────────────────────────┐
│                   Kubernetes Cluster (k3s)                       │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   ArgoCD    │  │  Longhorn   │  │  External   │              │
│  │   GitOps    │  │   Storage   │  │   Secrets   │              │
│  └─────────────┘  └─────────────┘  └─────────────┘              │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                    Enclii Services                          ││
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌──────────┐ ││
│  │  │    API    │  │    UI     │  │   Docs    │  │  Janua   │ ││
│  │  │  :4200    │  │  :4201    │  │  :4203    │  │  (SSO)   │ ││
│  │  └───────────┘  └───────────┘  └───────────┘  └──────────┘ ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
                              │
                    ┌─────────┴─────────┐
                    ▼                   ▼
           ┌─────────────┐      ┌─────────────┐
           │ PostgreSQL  │      │    Redis    │
           │ (In-cluster)│      │ (In-cluster)│
           └─────────────┘      └─────────────┘
```

## Related Documentation

- **Getting Started**: [Quick Start Guide](/getting-started/QUICKSTART)
- **Architecture**: [Platform Architecture](/architecture/)
- **Production**: [Production Checklist](/production/PRODUCTION_CHECKLIST) | [Deployment Roadmap](/production/PRODUCTION_DEPLOYMENT_ROADMAP)
- **Troubleshooting**: [Networking Issues](/troubleshooting/networking) | [Deployment Issues](/troubleshooting/deployment-issues)
- **Guides**: [Database Operations](/guides/database-operations) | [DNS Setup](/infrastructure/dns-setup-porkbun)
