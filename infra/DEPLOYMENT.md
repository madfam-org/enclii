# Enclii Deployment Guide

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


This document provides comprehensive instructions for deploying Enclii across different environments.

## Prerequisites

- **Kubernetes cluster** (v1.29+)
- **kubectl** configured with cluster access
- **Kustomize** (v5.0+) 
- **Helm** (v3.14+)
- **Docker** or compatible container runtime
- **Git** for source code management

## Architecture Overview

Enclii uses a microservices architecture deployed on Kubernetes with the following components:

### Core Services
- **Switchyard API**: Control plane REST API (Go)
- **Switchyard UI**: Web dashboard (Next.js)
- **Reconcilers**: Kubernetes controllers (Go)

### Infrastructure Services
- **PostgreSQL**: Primary database
- **Redis**: Caching layer
- **Jaeger**: Distributed tracing
- **Prometheus**: Metrics collection

### Security & Networking
- **RBAC**: Role-based access control
- **NetworkPolicies**: Network segmentation
- **TLS**: End-to-end encryption
- **JWT**: Authentication & authorization

## Environment Structure

```
infra/k8s/
├── base/                 # Base configurations
│   ├── switchyard-api.yaml
│   ├── postgres.yaml
│   ├── redis.yaml
│   ├── rbac.yaml
│   ├── secrets.yaml
│   ├── monitoring.yaml
│   └── network-policies.yaml
├── staging/              # Staging overlays
│   ├── kustomization.yaml
│   ├── replicas-patch.yaml
│   └── environment-patch.yaml
└── production/           # Production overlays
    ├── kustomization.yaml
    ├── replicas-patch.yaml
    ├── environment-patch.yaml
    └── security-patch.yaml
```

## Deployment Instructions

### 1. Local Development

Deploy to local kind cluster:

```bash
# Create kind cluster
make kind-up

# Deploy base infrastructure
kubectl apply -k infra/k8s/base

# Wait for services to be ready
kubectl wait --for=condition=ready pod -l app=switchyard-api --timeout=300s
```

### 2. Staging Environment

```bash
# Deploy to staging namespace
kubectl apply -k infra/k8s/staging

# Verify deployment
kubectl get pods -n enclii-staging
kubectl logs -n enclii-staging deployment/switchyard-api
```

### 3. Production Environment

⚠️ **Production deployment requires additional security measures:**

```bash
# Ensure production secrets are configured
kubectl create secret generic postgres-credentials \
  --from-literal=database-url="postgres://user:pass@prod-db:5432/enclii" \
  -n enclii-production

kubectl create secret generic jwt-secrets \
  --from-file=private-key=rsa-private.pem \
  --from-file=public-key=rsa-public.pem \
  -n enclii-production

# Deploy to production
kubectl apply -k infra/k8s/production

# Monitor deployment
kubectl rollout status deployment/switchyard-api -n enclii-production
```

## Configuration Management

### Environment Variables

| Variable | Development | Staging | Production |
|----------|------------|---------|------------|
| `ENCLII_LOG_LEVEL` | `debug` | `info` | `warn` |
| `ENCLII_RATE_LIMIT_REQUESTS_PER_MINUTE` | `1000` | `5000` | `10000` |
| `ENCLII_DB_POOL_SIZE` | `10` | `20` | `50` |
| `ENCLII_CACHE_TTL_SECONDS` | `1800` | `3600` | `7200` |

### Resource Allocation

| Environment | CPU Request | Memory Request | CPU Limit | Memory Limit |
|-------------|-------------|----------------|-----------|--------------|
| Development | `100m` | `128Mi` | `500m` | `512Mi` |
| Staging | `200m` | `256Mi` | `1000m` | `1Gi` |
| Production | `500m` | `512Mi` | `2000m` | `2Gi` |

### Replica Configuration

| Environment | Switchyard API | Redis | Postgres |
|-------------|----------------|-------|----------|
| Development | `2` | `1` | `1` |
| Staging | `3` | `2` | `1` |
| Production | `5` | `3` | `3` (HA) |

## Security Configuration

### RBAC Permissions

The Switchyard API service account requires:

- **Namespaces**: `create`, `get`, `list`, `watch`
- **Deployments**: `create`, `get`, `list`, `watch`, `update`, `patch`, `delete`
- **Services**: `create`, `get`, `list`, `watch`, `update`, `patch`, `delete`
- **Pods**: `get`, `list`, `watch` (logs access)
- **ConfigMaps/Secrets**: `create`, `get`, `list`, `watch`, `update`, `patch`
- **Ingresses**: `create`, `get`, `list`, `watch`, `update`, `patch`, `delete`

### Network Policies

- **Switchyard API**: Can access Postgres, Redis, Jaeger, and Kubernetes API
- **Database**: Only accessible by Switchyard API
- **Cache**: Only accessible by Switchyard API
- **Monitoring**: Prometheus can scrape metrics from all services

### Security Context

All containers run with:
- `runAsNonRoot: true`
- `runAsUser: 65532` (nobody)
- `readOnlyRootFilesystem: true`
- `allowPrivilegeEscalation: false`
- `capabilities.drop: [ALL]`

## Production Ingress: Cloudflare Tunnel

Production traffic routes through Cloudflare Tunnel for zero-trust security and zero-downtime deployments.

### Architecture

```
Internet → Cloudflare Edge → Cloudflare Tunnel → ClusterIP Services
           (TLS, DDoS)      (cloudflared pods)   (internal networking)
```

### Benefits

- **Zero-downtime deployments**: RollingUpdate strategy (no hostPort conflicts)
- **Zero exposed ports**: All traffic routes through tunnel
- **Zero-trust security**: DDoS protection at Cloudflare edge
- **Automatic TLS**: Certificates managed by Cloudflare
- **High availability**: 2 cloudflared replicas with PodDisruptionBudget

### Configuration

**Tunnel manifest**: `infra/k8s/production/cloudflared-unified.yaml`

```bash
# Deploy cloudflared
kubectl apply -f infra/k8s/production/cloudflared-unified.yaml

# Verify tunnel status
kubectl get pods -n cloudflare-tunnel
kubectl logs -n cloudflare-tunnel -l app=cloudflared

# Check service connectivity
curl https://api.enclii.dev/health
```

### Service Routing

**Port Mapping Hierarchy** (Critical for Cloudflare Tunnel Configuration):
1. **Container Port**: What the application listens on internally (e.g., 4200, 4201, 4204)
2. **K8s Service Port**: What the service exposes to the cluster (typically port 80)
3. **Cloudflare Tunnel Route**: Should point to K8s Service port (80), NOT container port

| Public Domain | Internal Service (K8s Service:Port) | Container Port |
|---------------|-------------------------------------|----------------|
| api.enclii.dev | switchyard-api.enclii.svc.cluster.local:80 | 4200 |
| app.enclii.dev | switchyard-ui.enclii.svc.cluster.local:80 | 4201 |
| enclii.dev | landing-page.enclii.svc.cluster.local:80 | 4204 |
| docs.enclii.dev | docs-site.enclii.svc.cluster.local:80 | 4203 |

> **Important**: The Cloudflare tunnel routes traffic to K8s Services, not directly to containers. Always use the K8s Service port (80) in tunnel configuration, not the container port.

### NetworkPolicy Requirements

Each namespace must allow traffic from cloudflared:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-cloudflared-ingress
  namespace: enclii
spec:
  podSelector: {}
  policyTypes:
  - Ingress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: cloudflare-tunnel
```

### Migration Script

Use the migration script for transitioning from hostPort:

```bash
./scripts/migrate-to-cloudflare-tunnel.sh setup     # Create tunnel
./scripts/migrate-to-cloudflare-tunnel.sh deploy    # Deploy cloudflared
./scripts/migrate-to-cloudflare-tunnel.sh remove-hostport  # Remove hostPort
./scripts/migrate-to-cloudflare-tunnel.sh verify    # Verify connectivity
./scripts/migrate-to-cloudflare-tunnel.sh rollback  # Rollback if needed
```

## Health Checks & Monitoring

### Health Endpoints

- **Readiness**: `/health/ready` - Service dependencies are healthy
- **Liveness**: `/health/live` - Service is responding
- **Metrics**: `/metrics` - Prometheus metrics

### Monitoring Stack

- **Metrics**: Prometheus scrapes `/metrics` endpoint
- **Tracing**: Jaeger collects OpenTelemetry traces
- **Logs**: Structured JSON logs with correlation IDs

### Alerts & SLOs

Key metrics to monitor:

- **Request Rate**: HTTP requests per second
- **Error Rate**: HTTP 5xx errors percentage < 1%
- **Response Time**: P95 latency < 500ms
- **Database Connections**: Pool utilization < 80%
- **Cache Hit Rate**: > 90%
- **Pod Restarts**: < 5 per hour

## Backup & Disaster Recovery

### Database Backup

```bash
# PostgreSQL backup
kubectl exec -n enclii-production deployment/postgres -- \
  pg_dump -U postgres enclii > backup-$(date +%Y%m%d).sql

# Restore from backup
kubectl exec -i -n enclii-production deployment/postgres -- \
  psql -U postgres enclii < backup-20240101.sql
```

### Persistent Volumes

Configure persistent storage for:
- PostgreSQL data (`/var/lib/postgresql/data`)
- Redis persistence (optional)
- Application logs

## Troubleshooting

### Common Issues

**Pod Stuck in Pending**
```bash
kubectl describe pod <pod-name> -n <namespace>
# Check resource constraints and node capacity
```

**Database Connection Errors**
```bash
kubectl logs -n <namespace> deployment/switchyard-api
# Verify secrets and network policies
```

**High Memory Usage**
```bash
kubectl top pods -n <namespace>
# Review resource limits and heap settings
```

### Debug Commands

```bash
# Check service endpoints
kubectl get endpoints -n <namespace>

# Verify network connectivity
kubectl exec -n <namespace> deployment/switchyard-api -- \
  nc -zv postgres 5432

# Review recent events
kubectl get events --sort-by=.metadata.creationTimestamp -n <namespace>

# Analyze resource usage
kubectl describe node <node-name>
```

## Rolling Updates & Rollbacks

### Deploy New Version

```bash
# Update image tag in kustomization
cd infra/k8s/production
kustomize edit set image switchyard-api=ghcr.io/madfam/switchyard-api:v1.2.0

# Apply changes
kubectl apply -k .

# Monitor rollout
kubectl rollout status deployment/switchyard-api -n enclii-production
```

### Rollback Deployment

```bash
# Rollback to previous version
kubectl rollout undo deployment/switchyard-api -n enclii-production

# Rollback to specific revision
kubectl rollout undo deployment/switchyard-api --to-revision=2 -n enclii-production
```

## Performance Optimization

### Database Optimization

- Connection pooling: 50 connections in production
- Query optimization with EXPLAIN ANALYZE
- Proper indexing on frequently queried columns
- Regular VACUUM and ANALYZE operations

### Caching Strategy

- Redis for session data and API responses
- Cache invalidation using tags
- TTL configuration per environment

### Resource Tuning

- JVM heap size: 50% of container memory limit
- Go garbage collector: GOGC=100
- Database shared_buffers: 25% of available memory

## Compliance & Auditing

### Security Scanning

```bash
# Container vulnerability scanning
trivy image ghcr.io/madfam/switchyard-api:latest

# Kubernetes configuration scanning
kubesec scan infra/k8s/production/kustomization.yaml
```

### Audit Logging

Enable audit logs for:
- API access patterns
- Resource modifications
- Authentication events
- Error conditions

### Backup Verification

- Weekly backup restoration tests
- Cross-region backup replication
- Recovery time objective (RTO): < 4 hours
- Recovery point objective (RPO): < 1 hour

---

## Support

For deployment issues:
- Check logs: `kubectl logs -f deployment/switchyard-api -n <namespace>`
- Review metrics: Access Grafana dashboards
- Contact: [devops@enclii.dev](mailto:devops@enclii.dev)