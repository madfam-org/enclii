---
title: npm Registry Implementation
description: Deploy Verdaccio as a private npm registry on Enclii infrastructure
sidebar_position: 21
tags: [infrastructure, npm, verdaccio, registry]
---

# npm.madfam.io Implementation Plan

## Related Documentation

- **DNS Setup**: [DNS Configuration (Porkbun)](/docs/infrastructure/dns-setup-porkbun)
- **Cloudflare**: [Cloudflare Integration](/docs/infrastructure/CLOUDFLARE)
- **Dogfooding**: [Dogfooding Roadmap](/docs/production/dogfooding-roadmap)

## Overview

This document outlines the complete implementation plan for deploying Verdaccio as an Enclii-managed service at `npm.madfam.io`.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         CLOUDFLARE                                  │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────┐ │
│  │ DNS             │  │ Tunnel          │  │ R2 Storage          │ │
│  │ npm.madfam.io   │──│ (Zero LB cost)  │  │ (Package backups)   │ │
│  └────────┬────────┘  └────────┬────────┘  └─────────────────────┘ │
└───────────┼────────────────────┼────────────────────────────────────┘
            │                    │
            ▼                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    HETZNER BARE METAL (k3s)                         │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                    ENCLII WORKLOADS NAMESPACE                │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │   │
│  │  │  VERDACCIO  │  │             │  │   PERSISTENT        │  │   │
│  │  │  Pod 1      │  │             │  │   VOLUME            │  │   │
│  │  │  (Single)   │  │             │  │   (Longhorn)        │  │   │
│  │  └──────┬──────┘  └──────┬──────┘  │   50Gi              │  │   │
│  │         │                │         └─────────────────────┘  │   │
│  │         └────────┬───────┘                                  │   │
│  │                  ▼                                          │   │
│  │         ┌─────────────┐                                     │   │
│  │         │   JANUA     │  (OAuth for npm login)              │   │
│  │         │   SSO       │                                     │   │
│  │         └─────────────┘                                     │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

## Implementation Phases

### Phase 1: DNS & Cloudflare Setup
**Timeline: Day 1**
**Owner: DevOps**

1. **Add DNS record in Porkbun**
   ```
   Type: CNAME
   Name: npm
   Target: <cloudflare-tunnel-id>.cfargotunnel.com
   TTL: Auto
   ```

2. **Configure Cloudflare Tunnel**
   ```yaml
   # cloudflared config
   tunnel: madfam-tunnel
   ingress:
     - hostname: npm.madfam.io
       service: http://verdaccio:4873
     - service: http_status:404
   ```

3. **Cloudflare Settings**
   - SSL/TLS: Full (strict)
   - Always Use HTTPS: On
   - Minimum TLS Version: 1.2
   - Cache: Bypass for authenticated requests

### Phase 2: Kubernetes Manifests
**Timeline: Day 1-2**
**Owner: DevOps**

Files to create in `infra/k8s/base/`:

1. **verdaccio-pvc.yaml** - Persistent storage
2. **verdaccio-config.yaml** - ConfigMap with config.yaml
3. **verdaccio-secret.yaml** - htpasswd and auth tokens
4. **verdaccio-deployment.yaml** - Pod spec
5. **verdaccio-service.yaml** - ClusterIP service
6. **verdaccio-ingress.yaml** - Cloudflare tunnel ingress

### Phase 3: Enclii Service Definition
**Timeline: Day 2**
**Owner: DevOps**

Create `dogfooding/npm-registry.yaml` following Enclii patterns.

### Phase 4: Janua OAuth Integration (Optional Enhancement)
**Timeline: Day 3-4**
**Owner: Backend**

Replace htpasswd with Janua OAuth using `verdaccio-auth-oauth2` plugin.

### Phase 5: CI/CD Integration
**Timeline: Day 4-5**
**Owner: DevOps**

1. Add NPM_MADFAM_TOKEN to GitHub org secrets
2. Update all repo workflows for auto-publish
3. Create publish workflow template

### Phase 6: Migrate Existing Packages
**Timeline: Day 5-7**
**Owner: All teams**

1. Publish existing workspace packages
2. Update `.npmrc` files across repos
3. Test installations

---

## Detailed Implementation

### Kubernetes Manifests

#### verdaccio-pvc.yaml
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: verdaccio-storage
  namespace: enclii-workloads
  labels:
    app: verdaccio
    service: npm-registry
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: longhorn
  resources:
    requests:
      storage: 50Gi
```

#### verdaccio-config.yaml
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: verdaccio-config
  namespace: enclii-workloads
data:
  config.yaml: |
    storage: /verdaccio/storage
    plugins: /verdaccio/plugins

    web:
      title: MADFAM Package Registry
      primary_color: "#6366f1"

    auth:
      htpasswd:
        file: /verdaccio/conf/htpasswd
        max_users: 100
        algorithm: bcrypt

    security:
      api:
        jwt:
          sign:
            expiresIn: 29d

    uplinks:
      npmjs:
        url: https://registry.npmjs.org/
        timeout: 30s
        cache: true

    packages:
      '@madfam/*':
        access: $authenticated
        publish: $authenticated
      '@janua/*':
        access: $authenticated
        publish: $authenticated
      '@dhanam/*':
        access: $authenticated
        publish: $authenticated
      '@cotiza/*':
        access: $authenticated
        publish: $authenticated
      '@fortuna/*':
        access: $authenticated
        publish: $authenticated
      '@avala/*':
        access: $authenticated
        publish: $authenticated
      '@forgesight/*':
        access: $authenticated
        publish: $authenticated
      '@coforma/*':
        access: $authenticated
        publish: $authenticated
      '@forj/*':
        access: $authenticated
        publish: $authenticated
      '@enclii/*':
        access: $authenticated
        publish: $authenticated
      '**':
        access: $all
        publish: $authenticated
        proxy: npmjs

    logs:
      type: stdout
      format: pretty
      level: info
```

#### verdaccio-deployment.yaml
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: verdaccio
  namespace: enclii-workloads
  labels:
    app: verdaccio
    service: npm-registry
spec:
  replicas: 1  # Single replica (RWO PVC)
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 1
  selector:
    matchLabels:
      app: verdaccio
  template:
    metadata:
      labels:
        app: verdaccio
        service: npm-registry
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 10001
        fsGroup: 10001
      containers:
        - name: verdaccio
          image: verdaccio/verdaccio:5
          ports:
            - containerPort: 4873
              name: http
          env:
            - name: VERDACCIO_PORT
              value: "4873"
            - name: VERDACCIO_PUBLIC_URL
              value: "https://npm.madfam.io"
          resources:
            requests:
              cpu: "100m"
              memory: "128Mi"
            limits:
              cpu: "500m"
              memory: "512Mi"
          volumeMounts:
            - name: config
              mountPath: /verdaccio/conf/config.yaml
              subPath: config.yaml
              readOnly: true
            - name: htpasswd
              mountPath: /verdaccio/conf/htpasswd
              subPath: htpasswd
            - name: storage
              mountPath: /verdaccio/storage
          livenessProbe:
            httpGet:
              path: /-/ping
              port: 4873
            initialDelaySeconds: 10
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /-/ping
              port: 4873
            initialDelaySeconds: 5
            periodSeconds: 10
      volumes:
        - name: config
          configMap:
            name: verdaccio-config
        - name: htpasswd
          secret:
            secretName: verdaccio-auth
        - name: storage
          persistentVolumeClaim:
            claimName: verdaccio-storage
```

#### verdaccio-service.yaml
```yaml
apiVersion: v1
kind: Service
metadata:
  name: verdaccio
  namespace: enclii-workloads
  labels:
    app: verdaccio
spec:
  type: ClusterIP
  ports:
    - port: 4873
      targetPort: 4873
      protocol: TCP
      name: http
  selector:
    app: verdaccio
```

### Enclii Service Definition

#### dogfooding/npm-registry.yaml
```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: npm-registry
  project: enclii-platform
  description: MADFAM private npm registry (npm.madfam.io)
  labels:
    tier: infrastructure
    criticality: high

spec:
  # Use official Verdaccio image
  image: verdaccio/verdaccio:5
  
  runtime:
    port: 4873
    replicas: 2
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
      limits:
        cpu: "500m"
        memory: "512Mi"

  env:
    - name: VERDACCIO_PORT
      value: "4873"
    - name: VERDACCIO_PUBLIC_URL
      value: "https://npm.madfam.io"

  volumes:
    - name: storage
      mountPath: /verdaccio/storage
      size: 50Gi
      storageClassName: longhorn
    - name: config
      mountPath: /verdaccio/conf/config.yaml
      subPath: config.yaml
      configMapRef:
        name: verdaccio-config

  domains:
    - domain: npm.madfam.io
      tls: true
      tlsIssuer: cloudflare

  healthCheck: /-/ping
  
  readinessProbe:
    path: /-/ping
    initialDelaySeconds: 5
    periodSeconds: 10
    
  livenessProbe:
    path: /-/ping
    initialDelaySeconds: 10
    periodSeconds: 30

  autoscaling:
    enabled: true
    minReplicas: 1
    maxReplicas: 5
    targetCPUUtilizationPercentage: 70

  slo:
    availability: 99.9
    latencyP95: 100
    errorRate: 0.1

  backup:
    enabled: true
    schedule: "0 2 * * *"  # Daily at 2 AM
    retention: 30
    destination: r2://madfam-backups/npm-registry
```

### GitHub Actions Workflow Template

#### .github/workflows/npm-publish.yml (template for all repos)
```yaml
name: Publish to npm.madfam.io

on:
  push:
    branches: [main]
    paths:
      - 'packages/*/package.json'
      - 'packages/*/src/**'
  workflow_dispatch:

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - uses: pnpm/action-setup@v2
        with:
          version: 9
          
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'pnpm'
          
      - name: Configure npm registry
        run: |
          echo "@madfam:registry=https://npm.madfam.io" >> ~/.npmrc
          echo "@janua:registry=https://npm.madfam.io" >> ~/.npmrc
          echo "@dhanam:registry=https://npm.madfam.io" >> ~/.npmrc
          echo "//npm.madfam.io/:_authToken=${{ secrets.NPM_MADFAM_TOKEN }}" >> ~/.npmrc
          
      - name: Install dependencies
        run: pnpm install --frozen-lockfile
        
      - name: Build packages
        run: pnpm build
        
      - name: Publish changed packages
        run: pnpm publish -r --no-git-checks --access restricted
        env:
          NODE_AUTH_TOKEN: ${{ secrets.NPM_MADFAM_TOKEN }}
```

### .npmrc Template (for all MADFAM repos)
```ini
# MADFAM Private Registry
@madfam:registry=https://npm.madfam.io
@janua:registry=https://npm.madfam.io
@dhanam:registry=https://npm.madfam.io
@cotiza:registry=https://npm.madfam.io
@fortuna:registry=https://npm.madfam.io
@avala:registry=https://npm.madfam.io
@forgesight:registry=https://npm.madfam.io
@coforma:registry=https://npm.madfam.io
@forj:registry=https://npm.madfam.io
@enclii:registry=https://npm.madfam.io

# For CI: auth token is set via NPM_MADFAM_TOKEN env var
# //npm.madfam.io/:_authToken=${NPM_MADFAM_TOKEN}
```

---

## Deployment Checklist

### Pre-deployment
- [ ] Porkbun DNS: Add CNAME for npm.madfam.io
- [ ] Cloudflare: Configure tunnel ingress
- [ ] Cloudflare: SSL settings configured
- [ ] k3s cluster: Verify storage class exists
- [ ] Secrets: Generate initial htpasswd

### Deployment
- [ ] Apply PVC: `kubectl apply -f verdaccio-pvc.yaml`
- [ ] Apply ConfigMap: `kubectl apply -f verdaccio-config.yaml`
- [ ] Apply Secret: `kubectl apply -f verdaccio-secret.yaml`
- [ ] Apply Deployment: `kubectl apply -f verdaccio-deployment.yaml`
- [ ] Apply Service: `kubectl apply -f verdaccio-service.yaml`
- [ ] Verify pods running: `kubectl get pods -l app=verdaccio`
- [ ] Test health endpoint: `curl https://npm.madfam.io/-/ping`

### Post-deployment
- [ ] Create admin user
- [ ] Create CI bot user (for GitHub Actions)
- [ ] Add NPM_MADFAM_TOKEN to GitHub org secrets
- [ ] Update .npmrc in all repos
- [ ] Publish initial packages
- [ ] Test package installation
- [ ] Set up monitoring alerts

---

## Monitoring & Alerts

### Health Checks
- Endpoint: `https://npm.madfam.io/-/ping`
- Expected: HTTP 200
- Check interval: 30s

### Alerts
| Metric | Threshold | Severity |
|--------|-----------|----------|
| Pod restarts | > 3/hour | Warning |
| Response time P95 | > 500ms | Warning |
| Error rate | > 1% | Critical |
| Storage usage | > 80% | Warning |
| Storage usage | > 95% | Critical |

### Grafana Dashboard
- Request rate
- Response time histogram
- Error rate
- Storage usage
- Active users

---

## Backup & Recovery

### Automated Backups
- Schedule: Daily at 2 AM UTC
- Retention: 30 days
- Destination: Cloudflare R2 (`r2://madfam-backups/npm-registry/`)

### Manual Backup
```bash
kubectl exec -n enclii-workloads deploy/verdaccio -- \
  tar czf - /verdaccio/storage | \
  aws s3 cp - s3://madfam-backups/npm-registry/manual-$(date +%Y%m%d).tar.gz
```

### Recovery Procedure
```bash
# 1. Scale down
kubectl scale deploy/verdaccio --replicas=0 -n enclii-workloads

# 2. Restore data
kubectl run restore --rm -it --image=alpine -- sh
# Inside pod: download and extract backup to PVC

# 3. Scale up
kubectl scale deploy/verdaccio --replicas=2 -n enclii-workloads
```

---

## Security Considerations

1. **Authentication**: htpasswd with bcrypt (upgrade to Janua OAuth later)
2. **TLS**: Enforced via Cloudflare (Full strict)
3. **Network Policy**: Only allow ingress from Cloudflare IPs
4. **Rate Limiting**: Cloudflare rate limiting rules
5. **Audit Logging**: All publish/unpublish actions logged
6. **Token Rotation**: CI tokens rotated quarterly

---

## Cost Analysis

| Component | Monthly Cost |
|-----------|--------------|
| Hetzner storage (50Gi) | ~$2.50 |
| Cloudflare R2 backups | ~$0.50 |
| Cloudflare Tunnel | $0 |
| CPU/Memory (shared) | ~$2 |
| **Total** | **~$5/month** |

vs npmjs.com private packages: $7/user/month × 5 users = $35/month

**Savings: $30/month ($360/year)**

---

## Future Enhancements

1. **Janua OAuth Integration** - Replace htpasswd with SSO
2. **Package Signing** - Cosign for supply chain security
3. **Vulnerability Scanning** - Integrate with Snyk/Trivy
4. **Web UI Customization** - MADFAM branding
5. **Metrics Export** - Prometheus metrics for package downloads
