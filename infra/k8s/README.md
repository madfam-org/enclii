# Enclii Kubernetes Manifests

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


> **Infrastructure as Code** for the Enclii PaaS platform

This directory contains Kustomize-based Kubernetes manifests for deploying Enclii across environments.

---

## Directory Structure

```
infra/k8s/
├── README.md           # This file
├── base/               # Base manifests (shared resources)
│   ├── kustomization.yaml
│   ├── switchyard-api.yaml    # Control plane API
│   ├── landing-page.yaml      # enclii.dev website
│   ├── roundhouse.yaml        # Build worker
│   ├── waybill.yaml           # Cost tracking
│   ├── redis.yaml             # Cache/sessions
│   ├── monitoring.yaml        # Prometheus/Grafana
│   ├── network-policies.yaml  # Pod isolation rules
│   ├── rbac.yaml              # Roles and bindings
│   ├── ingress-nginx.yaml     # Ingress controller (dev only)
│   ├── cert-manager.yaml      # TLS automation
│   ├── secrets.production.yaml # Production secrets (jwt + postgres-credentials)
│   ├── secrets.dev.yaml       # Dev credentials (not in production kustomization)
│   ├── postgres.yaml          # Database (dev only, not in production kustomization)
│   ├── secrets.yaml.TEMPLATE  # Prod template
│   └── verdaccio/             # NPM registry (ArgoCD-managed)
├── platform-infra/     # Infrastructure umbrella (ArgoCD visibility)
│   ├── kustomization.yaml
│   ├── postgres.yaml          # → data namespace postgres (not base/postgres.yaml)
│   ├── redis.yaml             # → symlink to production/data/redis.yaml
│   ├── postgres-backup.yaml   # → symlink to production/backup/
│   ├── backup-verify-cronjob.yaml
│   ├── postgres-restore-drill.yaml
│   └── postgres-exporter.yaml
├── staging/            # Staging overlay
│   ├── kustomization.yaml
│   ├── replicas-patch.yaml
│   └── environment-patch.yaml
└── production/         # Production overlay
    ├── kustomization.yaml
    ├── cloudflared.yaml           # Cloudflare Tunnel
    ├── cloudflared-unified.yaml   # Unified tunnel config
    ├── redis-sentinel.yaml        # HA Redis
    ├── oidc-secrets.yaml          # Janua SSO credentials
    ├── build-secrets.yaml         # GitHub/Registry tokens
    ├── cloudflare-secrets.yaml    # R2/DNS tokens
    ├── replicas-patch.yaml        # Production scaling
    ├── environment-patch.yaml     # Production env vars
    └── security-patch.yaml        # Security hardening
```

---

## Quick Start

### Deploy to Production

```bash
# Prerequisites
export KUBECONFIG=~/.kube/enclii-production

# Preview changes
kubectl kustomize production | kubectl diff -f -

# Apply
kubectl apply -k production

# Verify
kubectl get pods -n enclii-production
```

### Deploy to Staging

```bash
kubectl apply -k staging
kubectl get pods -n enclii-staging
```

### Local Development (Kind/k3d)

```bash
# Create cluster
kind create cluster --name enclii-dev

# Deploy base resources
kubectl apply -k base

# Forward ports for local access
kubectl port-forward svc/switchyard-api 4200:4200
```

---

## Resource Inventory

### Core Services

| Resource | Kind | Port | Purpose |
|----------|------|------|---------|
| switchyard-api | Deployment | 4200 | Control plane API |
| landing-page | Deployment | 4203 | Marketing website |
| roundhouse | Deployment | - | Build worker |
| waybill | Deployment | - | Cost tracking |

### Infrastructure

| Resource | Kind | Purpose |
|----------|------|---------|
| postgres | StatefulSet | Database (data namespace; base/postgres.yaml for dev only) |
| redis | StatefulSet | Session/cache storage |
| redis-sentinel | StatefulSet | Production HA Redis |
| cloudflared | Deployment | Zero-trust ingress tunnel |

### Networking

| Resource | Purpose |
|----------|---------|
| ingress-nginx | HTTP routing (dev only) |
| network-policies | Pod-to-pod isolation |
| cloudflared | Production traffic routing |

### Security

| Resource | Purpose |
|----------|---------|
| rbac.yaml | Service accounts, roles, bindings |
| cert-manager.yaml | TLS certificate automation |
| secrets.yaml | Credential storage |

---

## Kustomize Patterns

### Base + Overlays

The base directory contains environment-agnostic resources. Overlays (staging/production) customize them:

```yaml
# production/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: enclii-production

resources:
  - ../base                    # Import all base resources
  - cloudflared.yaml           # Production-only resources
  - redis-sentinel.yaml

patchesStrategicMerge:         # Customize base resources
  - replicas-patch.yaml
  - environment-patch.yaml
  - security-patch.yaml

configMapGenerator:            # Environment-specific config
  - name: env-config
    literals:
      - ENCLII_ENV=production
      - ENCLII_LOG_LEVEL=warn
```

### Strategic Merge Patches

```yaml
# production/replicas-patch.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: switchyard-api
spec:
  replicas: 3  # Override base replica count
```

### Common Labels

All resources automatically receive:
```yaml
labels:
  app.kubernetes.io/part-of: enclii
  environment: production  # From overlay
```

---

## Secrets Management

### Development (Local)

Uses plaintext secrets for convenience:
```bash
# Dev secrets (not included in production kustomization)
kubectl apply -f base/secrets.dev.yaml
kubectl apply -f base/postgres.yaml
```

### Production

**Never commit production secrets.** Use one of:

1. **External Secrets Operator** (recommended)
   ```yaml
   apiVersion: external-secrets.io/v1beta1
   kind: ExternalSecret
   spec:
     secretStoreRef:
       name: vault-backend
     target:
       name: enclii-secrets
     data:
       - secretKey: db-password
         remoteRef:
           key: enclii/production
           property: database_password
   ```

2. **Sealed Secrets**
   ```bash
   kubeseal --format yaml < secret.yaml > sealed-secret.yaml
   ```

3. **kubectl create secret**
   ```bash
   kubectl create secret generic enclii-secrets \
     --from-literal=db-password='xxx' \
     --from-literal=jwt-secret='xxx' \
     -n enclii-production
   ```

See `secrets.yaml.TEMPLATE` for required keys.

---

## Cloudflare Tunnel Architecture

Production uses Cloudflare Tunnel for zero-trust ingress:

```
Internet → Cloudflare Edge → cloudflared pods → ClusterIP Services
           (TLS, DDoS)       (2 replicas)       (internal routing)
```

### Configuration

```yaml
# production/cloudflared.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cloudflared-config
data:
  config.yaml: |
    tunnel: enclii-tunnel
    credentials-file: /etc/cloudflared/credentials.json
    ingress:
      - hostname: api.enclii.dev
        service: http://switchyard-api:4200
      - hostname: app.enclii.dev
        service: http://switchyard-ui:4201
      - hostname: enclii.dev
        service: http://landing-page:4203
      - service: http_status:404
```

### Tunnel Credentials

```bash
# Create secret from Cloudflare dashboard
kubectl create secret generic cloudflared-credentials \
  --from-file=credentials.json=tunnel-credentials.json \
  -n enclii-production
```

---

## Network Policies

Strict pod isolation is enforced:

```yaml
# base/network-policies.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-all
spec:
  podSelector: {}
  policyTypes:
    - Ingress
    - Egress

---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-switchyard-api
spec:
  podSelector:
    matchLabels:
      app: switchyard-api
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: cloudflared
      ports:
        - port: 4200
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: data
          podSelector:
            matchLabels:
              app: postgres
      ports:
        - port: 5432
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: data
          podSelector:
            matchLabels:
              app: redis
      ports:
        - port: 6379
```

---

## Monitoring

### Prometheus Metrics

Services expose metrics at `/metrics`:

```yaml
# Prometheus scrape annotation
annotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "4290"
  prometheus.io/path: "/metrics"
```

### Grafana Dashboards

Pre-configured dashboards in `base/monitoring.yaml`:
- API latency and error rates
- Build queue depth
- Resource utilization

---

## Common Operations

### Scale a Deployment

```bash
kubectl scale deploy/switchyard-api --replicas=5 -n enclii-production
```

### Rolling Restart

```bash
kubectl rollout restart deploy/switchyard-api -n enclii-production
kubectl rollout status deploy/switchyard-api -n enclii-production
```

### View Logs

```bash
kubectl logs -l app=switchyard-api -n enclii-production -f
```

### Debug a Pod

```bash
kubectl exec -it deploy/switchyard-api -n enclii-production -- sh
```

### Check Resource Usage

```bash
kubectl top pods -n enclii-production
```

---

## Troubleshooting

### Pod Not Starting

```bash
kubectl describe pod <pod-name> -n enclii-production
kubectl logs <pod-name> -n enclii-production --previous
```

### Network Issues

```bash
# Test connectivity
kubectl run debug --rm -it --image=curlimages/curl -- sh
curl http://switchyard-api:4200/health

# Check network policies
kubectl get networkpolicy -n enclii-production
```

### Tunnel Not Routing

```bash
kubectl logs -l app=cloudflared -n enclii-production
kubectl get configmap cloudflared-config -n enclii-production -o yaml
```

---

## File Reference

| File | Purpose | When to Modify |
|------|---------|----------------|
| `base/kustomization.yaml` | Resource list | Adding new services |
| `base/switchyard-api.yaml` | API deployment | Changing ports, resources |
| `base/network-policies.yaml` | Security rules | New service connectivity |
| `production/replicas-patch.yaml` | Scaling | Production capacity |
| `production/environment-patch.yaml` | Env vars | Config changes |
| `production/cloudflared.yaml` | Tunnel routes | New domains |

---

## Related Documentation

- [Production Deployment Roadmap](../../docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md)
- [Production Checklist](../../docs/production/PRODUCTION_CHECKLIST.md)
- [Onboarding Guide](../../docs/guides/ONBOARDING_GUIDE.md)
- [Security Architecture](../../docs/architecture/ARCHITECTURE.md#security-architecture)

---

*Last Updated: February 25, 2026*
