# Service Specification Reference

The `enclii.yaml` file defines how your service is built, deployed, and configured on Enclii.

## Quick Start

Create an `enclii.yaml` in your project root:

```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: my-api
  project: my-project
spec:
  build:
    type: auto
  runtime:
    port: 8080
    replicas: 2
    healthCheck: /health
  domains:
    - name: api.example.com
  env:
    - name: NODE_ENV
      value: production
```

---

## Full Schema

### Root Structure

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `apiVersion` | string | Yes | Must be `enclii.dev/v1` |
| `kind` | string | Yes | Must be `Service` |
| `metadata` | [Metadata](#metadata) | Yes | Service identification |
| `spec` | [Spec](#spec) | Yes | Service configuration |

---

### Metadata

```yaml
metadata:
  name: my-api
  project: acme-corp
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Service name (lowercase, alphanumeric, hyphens) |
| `project` | string | Yes | Project slug this service belongs to |

**Naming Rules:**
- 3-63 characters
- Lowercase letters, numbers, hyphens only
- Must start with a letter
- Must end with a letter or number

---

### Spec

The `spec` section contains all service configuration.

```yaml
spec:
  build: { ... }
  runtime: { ... }
  domains: [ ... ]
  env: [ ... ]
  volumes: [ ... ]
  healthCheck: { ... }
  resources: { ... }
  autoDeploy: { ... }
```

---

## Domains (Custom Domain Auto-Provisioning)

Declares custom domains that are automatically provisioned when the service is deployed.
On each push to main, Enclii reads `enclii.yaml` and:

1. Creates a `CustomDomain` record in the platform database
2. Adds a Cloudflare tunnel route pointing to the service
3. Creates a CNAME DNS record in Cloudflare

**No manual edits to infrastructure repos required.**

```yaml
spec:
  domains:
    - name: api.qubic.quest
      environment: production
      tlsEnabled: true
    - name: studio.qubic.quest
      environment: production
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | Yes | - | Fully qualified domain name (e.g., `api.qubic.quest`) |
| `environment` | string | No | `production` | Target environment for this domain |
| `tlsEnabled` | boolean | No | `true` | Whether to enable TLS via cert-manager |

**Requirements:**
- The domain's zone (e.g., `qubic.quest`) must be managed by Cloudflare under the same account
- The service must be registered via `enclii service create` before the first push
- DNS records are CNAME'd to `tunnel.enclii.dev` (the Cloudflare tunnel endpoint)

**Cleanup:**
When a service is deleted, its tunnel routes and DNS records are automatically removed.

**How It Works:**
On each push to main, the webhook handler fetches `enclii.yaml` from the repo (via the GitHub Contents API), parses the `spec.domains` section, and provisions each domain through three steps: DB record creation, Cloudflare tunnel route addition, and DNS CNAME creation. Errors in provisioning are logged but never block the deployment.

**See Also:** [Cloudflare Integration](../infrastructure/CLOUDFLARE.md#self-service-domain-auto-provisioning) for infrastructure details.

---

## Build Configuration

Defines how your service is built into a container image.

```yaml
spec:
  build:
    type: auto
    dockerfile: ./Dockerfile
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | enum | `auto` | Build method: `auto`, `dockerfile`, `buildpack` |
| `dockerfile` | string | `Dockerfile` | Path to Dockerfile (when type is `dockerfile`) |
| `buildpack` | string | auto-detect | Buildpack to use (when type is `buildpack`) |

### Build Types

#### `auto` (Recommended)

Enclii automatically detects the best build method:

```yaml
spec:
  build:
    type: auto
```

Detection logic:
1. If `Dockerfile` exists → use it
2. Else → use Nixpacks/Buildpacks based on language

#### `dockerfile`

Use a custom Dockerfile:

```yaml
spec:
  build:
    type: dockerfile
    dockerfile: ./docker/Dockerfile.prod
```

#### `buildpack`

Force a specific buildpack:

```yaml
spec:
  build:
    type: buildpack
    buildpack: heroku/nodejs
```

---

## Runtime Configuration

Defines how your service runs in the cluster.

```yaml
spec:
  runtime:
    port: 8080
    replicas: 3
    healthCheck: /health
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `port` | integer | `8080` | Container port to expose |
| `replicas` | integer | `1` | Number of instances |
| `healthCheck` | string | `/health` | Health check endpoint path |

### Port Configuration

The port your application listens on:

```yaml
spec:
  runtime:
    port: 3000  # Node.js default
```

**Environment Variable**: Enclii sets `ENCLII_PORT` to this value. Configure your app to listen on `$ENCLII_PORT` or the specified port.

### Replicas

Number of running instances:

```yaml
spec:
  runtime:
    replicas: 3  # 3 instances for high availability
```

**Scaling Considerations:**
- Preview environments: 1 replica (save costs)
- Staging: 1-2 replicas
- Production: 2+ replicas (for high availability)

---

## Health Check Configuration

Comprehensive health check configuration for Kubernetes probes.

```yaml
spec:
  healthCheck:
    path: /health
    port: 8080
    livenessPath: /live
    readinessPath: /ready
    initialDelaySeconds: 10
    periodSeconds: 10
    timeoutSeconds: 5
    failureThreshold: 3
    disabled: false
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `path` | string | `/health` | Default path for both probes |
| `port` | integer | container port | Port to check |
| `livenessPath` | string | `path` value | Override for liveness probe only |
| `readinessPath` | string | `path` value | Override for readiness probe only |
| `initialDelaySeconds` | integer | 10 (readiness), 30 (liveness) | Wait before first probe |
| `periodSeconds` | integer | `10` | Time between probes |
| `timeoutSeconds` | integer | `5` | Timeout for each probe |
| `failureThreshold` | integer | `3` | Failures before marking unhealthy |
| `disabled` | boolean | `false` | Disable health checks (not recommended) |

### Probe Types

**Liveness Probe**: Determines if the container should be restarted.
- Use for: Deadlock detection, unrecoverable states
- Path: `/live` or `/health`

**Readiness Probe**: Determines if the container can receive traffic.
- Use for: Startup readiness, dependency checks
- Path: `/ready` or `/health`

### Example: Separate Probes

```yaml
spec:
  healthCheck:
    livenessPath: /live        # Simple alive check
    readinessPath: /ready      # Checks dependencies (DB, cache)
    initialDelaySeconds: 30    # Give app time to start
    periodSeconds: 15
    timeoutSeconds: 10
```

### Example: Slow-Starting App

```yaml
spec:
  healthCheck:
    path: /health
    initialDelaySeconds: 60    # Wait 60s before first check
    periodSeconds: 30
    failureThreshold: 5        # Allow 5 failures before restart
```

---

## Resource Configuration

Define CPU and memory limits for your containers.

```yaml
spec:
  resources:
    cpuRequest: "100m"
    cpuLimit: "500m"
    memoryRequest: "128Mi"
    memoryLimit: "512Mi"
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `cpuRequest` | string | `100m` | Minimum CPU guaranteed |
| `cpuLimit` | string | `500m` | Maximum CPU allowed |
| `memoryRequest` | string | `128Mi` | Minimum memory guaranteed |
| `memoryLimit` | string | `512Mi` | Maximum memory allowed |

### CPU Units

- `100m` = 0.1 CPU (100 millicores)
- `500m` = 0.5 CPU
- `1` or `1000m` = 1 full CPU
- `2` = 2 CPUs

### Memory Units

- `128Mi` = 128 mebibytes
- `512Mi` = 512 mebibytes
- `1Gi` = 1 gibibyte
- `2Gi` = 2 gibibytes

### Resource Guidelines

| Workload Type | CPU Request | CPU Limit | Memory Request | Memory Limit |
|---------------|-------------|-----------|----------------|--------------|
| API Server | 100m | 500m | 128Mi | 512Mi |
| Web App | 100m | 250m | 128Mi | 256Mi |
| Worker | 250m | 1000m | 256Mi | 1Gi |
| Database | 500m | 2000m | 512Mi | 2Gi |

---

## Environment Variables

Configure environment variables for your service.

```yaml
spec:
  env:
    - name: NODE_ENV
      value: production
    - name: DATABASE_URL
      value: postgresql://user:pass@db:5432/app
    - name: API_KEY
      value: "secret-key"  # Consider using secrets instead
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Variable name |
| `value` | string | Yes | Variable value |

### Naming Rules

- Uppercase letters, numbers, underscores
- Must start with a letter
- Max 256 characters

### Secrets

For sensitive values, use the Enclii dashboard or CLI to manage secrets:

```bash
# Add a secret via CLI
enclii secrets set DATABASE_URL "postgresql://..." --env production
```

Secrets are:
- Encrypted at rest (AES-256-GCM)
- Masked in API responses
- Audit logged on access

---

## Volumes

Persistent storage for your service.

```yaml
spec:
  volumes:
    - name: data
      mountPath: /app/data
      size: "10Gi"
      storageClassName: standard
      accessMode: ReadWriteOnce
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | Required | Volume name (lowercase, alphanumeric) |
| `mountPath` | string | Required | Path inside container |
| `size` | string | Required | Storage size (e.g., `10Gi`, `100Mi`) |
| `storageClassName` | string | `standard` | Kubernetes storage class |
| `accessMode` | string | `ReadWriteOnce` | Access mode |

### Access Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| `ReadWriteOnce` | Single node read-write | Databases, caches |
| `ReadOnlyMany` | Multi-node read-only | Shared config, static assets |
| `ReadWriteMany` | Multi-node read-write | Shared uploads (NFS) |

### Volume Examples

**Database Storage:**
```yaml
volumes:
  - name: postgres-data
    mountPath: /var/lib/postgresql/data
    size: "50Gi"
    storageClassName: fast-ssd
```

**File Uploads:**
```yaml
volumes:
  - name: uploads
    mountPath: /app/uploads
    size: "100Gi"
    accessMode: ReadWriteMany
```

---

## Auto-Deploy Configuration

Automatically deploy on git push.

```yaml
spec:
  autoDeploy:
    enabled: true
    branch: main
    environment: staging
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | boolean | `false` | Enable auto-deploy |
| `branch` | string | `main` | Branch to watch |
| `environment` | string | `staging` | Target environment |

### Auto-Deploy Flow

1. Push to configured branch
2. GitHub webhook triggers build
3. Build succeeds → Deploy to target environment
4. Build fails → No deployment, notification sent

---

## Complete Examples

### Node.js API

```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: api
  project: acme-corp
spec:
  build:
    type: auto
  runtime:
    port: 3000
    replicas: 3
    healthCheck: /health
  resources:
    cpuRequest: "100m"
    cpuLimit: "500m"
    memoryRequest: "128Mi"
    memoryLimit: "512Mi"
  healthCheck:
    path: /health
    livenessPath: /live
    readinessPath: /ready
    initialDelaySeconds: 10
    periodSeconds: 10
  env:
    - name: NODE_ENV
      value: production
    - name: LOG_LEVEL
      value: info
  autoDeploy:
    enabled: true
    branch: main
    environment: staging
```

### Go Microservice

```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: payment-service
  project: fintech-app
spec:
  build:
    type: dockerfile
    dockerfile: ./Dockerfile
  runtime:
    port: 8080
    replicas: 2
  resources:
    cpuRequest: "250m"
    cpuLimit: "1000m"
    memoryRequest: "256Mi"
    memoryLimit: "1Gi"
  healthCheck:
    path: /healthz
    initialDelaySeconds: 5
    periodSeconds: 10
  env:
    - name: ENVIRONMENT
      value: production
```

### Python Worker

```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: ml-worker
  project: ai-platform
spec:
  build:
    type: buildpack
    buildpack: heroku/python
  runtime:
    port: 8000
    replicas: 5
  resources:
    cpuRequest: "500m"
    cpuLimit: "2000m"
    memoryRequest: "1Gi"
    memoryLimit: "4Gi"
  healthCheck:
    path: /health
    initialDelaySeconds: 60
    timeoutSeconds: 30
  volumes:
    - name: model-cache
      mountPath: /app/models
      size: "50Gi"
```

### Static Website

```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: docs
  project: company-site
spec:
  build:
    type: auto
  runtime:
    port: 80
    replicas: 2
  resources:
    cpuRequest: "50m"
    cpuLimit: "100m"
    memoryRequest: "64Mi"
    memoryLimit: "128Mi"
  healthCheck:
    path: /
    periodSeconds: 30
```

---

## Validation

Validate your configuration before deploying:

```bash
# Dry-run validation
enclii services sync --dry-run

# Full validation with diff
enclii deploy --dry-run
```

### Common Validation Errors

| Error | Cause | Solution |
|-------|-------|----------|
| `invalid name` | Name contains invalid characters | Use lowercase, alphanumeric, hyphens |
| `port out of range` | Port < 1 or > 65535 | Use valid port number |
| `invalid memory format` | Wrong memory unit | Use `Mi` or `Gi` (e.g., `512Mi`) |
| `invalid cpu format` | Wrong CPU unit | Use `m` or whole numbers (e.g., `100m`, `1`) |

---

## See Also

- [`enclii init`](../cli/commands/init.md) - Generate configuration
- [`enclii services sync`](../cli/commands/services-sync.md) - Sync configuration
- [`enclii deploy`](../cli/commands/deploy.md) - Deploy service
