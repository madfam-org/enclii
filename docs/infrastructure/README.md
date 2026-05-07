---
title: Infrastructure Overview
description: Production infrastructure components and architecture documentation
sidebar_position: 1
tags: [infrastructure, kubernetes, argocd, longhorn, cloudflare]
---

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

Use Enclii first for infrastructure manipulation. `enclii ops` is the
operator entry point for Kubernetes, ArgoCD, Longhorn, Kyverno,
ExternalSecrets, Vault, and ARC workflows. `enclii providers` is the entry
point for GitHub, Cloudflare, Porkbun, and Hetzner workflows. Direct
`kubectl` or provider CLI/API commands are break-glass only until a missing
adapter is wired.

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

### Break-Glass Equivalents

Use these only when the Enclii adapter is missing or blocked, and record the
gap for Enclii coverage.

```bash
# ArgoCD sync status
kubectl get applications -n argocd

# Longhorn volumes
kubectl get volumes.longhorn.io -n longhorn-system

# Cloudflare tunnel
kubectl get pods -n cloudflare-tunnel

# External secrets sync
kubectl get externalsecrets -n enclii

# ArgoCD UI (https://localhost:8080)
kubectl port-forward svc/argocd-server -n argocd 8080:443

# Longhorn (http://localhost:8081)
kubectl port-forward svc/longhorn-frontend -n longhorn-system 8081:80
```

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
