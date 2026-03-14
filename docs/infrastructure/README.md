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

### Check Infrastructure Health

```bash
# ArgoCD sync status
kubectl get applications -n argocd

# Longhorn volumes
kubectl get volumes.longhorn.io -n longhorn-system

# Cloudflare tunnel
kubectl get pods -n cloudflare-tunnel

# External secrets sync
kubectl get externalsecrets -n enclii
```

### Access UIs

```bash
# ArgoCD (https://localhost:8080)
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

- **Getting Started**: [Quick Start Guide](/docs/getting-started/QUICKSTART)
- **Architecture**: [Platform Architecture](/docs/architecture/ARCHITECTURE)
- **Production**: [Production Checklist](/docs/production/PRODUCTION_CHECKLIST) | [Deployment Roadmap](/docs/production/PRODUCTION_DEPLOYMENT_ROADMAP)
- **Troubleshooting**: [Networking Issues](/docs/troubleshooting/networking) | [Deployment Issues](/docs/troubleshooting/deployment-issues)
- **Guides**: [Database Operations](/docs/guides/database-operations) | [DNS Setup](/docs/infrastructure/dns-setup-porkbun)
