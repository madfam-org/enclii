---
title: npm Registry Implementation
description: Deploy Verdaccio as a private npm registry on Enclii infrastructure
sidebar_position: 21
tags: [infrastructure, npm, verdaccio, registry]
---

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


# npm.madfam.io Implementation Plan

## Related Documentation

- **DNS Setup**: [DNS Configuration (Porkbun)](/infrastructure/dns-setup-porkbun)
- **Cloudflare**: [Cloudflare Integration](/infrastructure/CLOUDFLARE)
- **Onboarding**: [Onboarding Guide](/guides/ONBOARDING_GUIDE)

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

Create `enclii.yaml` service definition following Enclii patterns.

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
        access: $all              # public read for SDK consumers
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
        access: $all              # public read for SDK consumers
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

#### npm-registry enclii.yaml
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

### GitHub Actions Workflow (Enclii)

#### .github/workflows/publish-sdks.yml

Tag-triggered workflow that publishes individual `@enclii/*` packages. Push a tag like `shared-lib-v0.1.0` to trigger.

```yaml
name: Publish SDKs

on:
  push:
    tags:
      - "shared-lib-v*"
      - "ui-components-v*"
      - "config-v*"

jobs:
  publish:
    name: Publish @enclii SDK
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: "20"
          registry-url: "https://npm.madfam.io"
      - uses: pnpm/action-setup@v4
        with:
          version: 9
      - name: Extract package name and version
        id: extract
        run: |
          TAG=${GITHUB_REF#refs/tags/}
          PKG_NAME=${TAG%-v*}
          VERSION=${TAG#*-v}
          echo "pkg_name=$PKG_NAME" >> $GITHUB_OUTPUT
          echo "version=$VERSION" >> $GITHUB_OUTPUT
      - name: Install dependencies
        run: pnpm install
        working-directory: packages
      - name: Build
        run: pnpm run --if-present build
        working-directory: packages/${{ steps.extract.outputs.pkg_name }}
      - name: Publish to npm.madfam.io
        env:
          NODE_AUTH_TOKEN: ${{ secrets.NPM_MADFAM_TOKEN }}
        run: pnpm publish --no-git-checks --access public
        working-directory: packages/${{ steps.extract.outputs.pkg_name }}
      - name: Create GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          name: "@enclii/${{ steps.extract.outputs.pkg_name }} v${{ steps.extract.outputs.version }}"
```

**Publishing a new version:**
```bash
# 1. Bump version in packages/<name>/package.json
# 2. Commit and push
# 3. Tag and push
git tag shared-lib-v0.2.0
git push origin shared-lib-v0.2.0
```

#### Publishable packages (all repos)

| Package | Repo | Tag Pattern | Version | Status |
|---------|------|-------------|---------|--------|
| `@enclii/shared-lib` | enclii | `shared-lib-v*` | 0.1.0 | Published |
| `@enclii/ui-components` | enclii | `ui-components-v*` | 0.1.0 | Published |
| `@enclii/config` | enclii | `config-v*` | 0.1.0 | Published |
| `@janua/ui` | janua | `ui-v*` | 0.1.1 | Published |
| `@janua/react-sdk` | janua | `react-sdk-v*` | 0.1.1 | Published |
| `@janua/typescript-sdk` | janua | `typescript-sdk-v*` | 0.1.1 | Published |
| `@janua/nextjs` | janua | `nextjs-v*` | 0.1.2 | Published |
| `@dhanam/shared` | dhanam | `@dhanam/shared@*` | 0.1.0 | Published |
| `@dhanam/esg` | dhanam | `@dhanam/esg@*` | 0.1.0 | Published |
| `@dhanam/simulations` | dhanam | `@dhanam/simulations@*` | 0.1.0 | Published |
| `@dhanam/billing-sdk` | dhanam | `@dhanam/billing-sdk@*` | 0.2.0 | Published |
| `@tezca/api-client` | tezca | `api-client-v*` | 0.1.0 | Published |
| `@forgesight/client` | forgesight | `client-v*` | 0.1.0 | Published |

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

# Auth token for CI (locally: set NPM_MADFAM_TOKEN env var or run `npm login --registry https://npm.madfam.io`)
//npm.madfam.io/:_authToken=${NPM_MADFAM_TOKEN}
```

---

## Deployment Checklist

### Pre-deployment
- [x] Porkbun DNS: Add CNAME for npm.madfam.io
- [x] Cloudflare: Configure tunnel ingress
- [x] Cloudflare: SSL settings configured
- [x] k3s cluster: Verify storage class exists
- [x] Secrets: Generate initial htpasswd

### Deployment
- [x] Apply PVC: `kubectl apply -f verdaccio-pvc.yaml`
- [x] Apply ConfigMap: `kubectl apply -f verdaccio-config.yaml`
- [x] Apply Secret: `kubectl apply -f verdaccio-secret.yaml`
- [x] Apply Deployment: `kubectl apply -f verdaccio-deployment.yaml`
- [x] Apply Service: `kubectl apply -f verdaccio-service.yaml`
- [x] Verify pods running: `kubectl get pods -l app=verdaccio`
- [x] Test health endpoint: `curl https://npm.madfam.io/-/ping`

### Post-deployment
- [x] Create admin user
- [x] Create CI bot user (for GitHub Actions)
- [x] Add NPM_MADFAM_TOKEN to GitHub org secrets
- [x] Update .npmrc in all repos
- [x] Publish initial @enclii packages (shared-lib, ui-components, config)
- [x] Test package installation
- [ ] Set up monitoring alerts

> **Status (Mar 2026):** Verdaccio running and healthy. ArgoCD-managed as `npm-registry-services` app. PVC reduced from 50Gi to 5Gi (Session 44). After PVC corruption + recreation (Mar 14, 2026), all 13 packages across 5 repos were republished. NPM_MADFAM_TOKEN rotated on all 5 repos (janua, enclii, dhanam, tezca, forgesight). Token expires ~Jun 12, 2026. CI publish workflows operational on enclii, dhanam, tezca, and forgesight.

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
6. **Token Rotation**: CI tokens rotated quarterly (current token expires ~Jun 12, 2026)

---

## Cost Analysis

| Component | Monthly Cost |
|-----------|--------------|
| Hetzner storage (5Gi) | ~$0.25 |
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
