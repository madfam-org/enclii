---
title: Services
description: Manage Enclii services with the TypeScript SDK
sidebar_position: 4
tags: [sdk, typescript, services, deployment]
---

# Services

Manage Enclii services using the TypeScript SDK.

## Overview

Services are the deployable units in Enclii. Each service represents an application or microservice within a project.

```typescript
import { EncliiClient } from '@enclii/sdk';

const enclii = new EncliiClient();

// Services module
enclii.services.list(projectId);
enclii.services.get(serviceId);
enclii.services.create(data);
enclii.services.update(serviceId, data);
enclii.services.delete(serviceId);
enclii.services.deploy(serviceId, options);
```

## List Services

```typescript
// List all services in a project
const services = await enclii.services.list('proj_abc123');

for (const service of services) {
  console.log(`${service.name}: ${service.status}`);
}
```

### With Filtering

```typescript
// Filter by status
const runningServices = await enclii.services.list('proj_abc123', {
  status: 'running',
});

// Filter by environment
const prodServices = await enclii.services.list('proj_abc123', {
  environment: 'production',
});

// Search by name
const apiServices = await enclii.services.list('proj_abc123', {
  search: 'api',
});
```

## Get Service

```typescript
const service = await enclii.services.get('svc_xyz789');

console.log(`Service: ${service.name}`);
console.log(`Status: ${service.status}`);
console.log(`URL: ${service.url}`);
console.log(`Replicas: ${service.replicas.running}/${service.replicas.desired}`);
```

### With Related Data

```typescript
const service = await enclii.services.get('svc_xyz789', {
  include: ['deployments', 'domains', 'metrics'],
});

console.log(`Recent deployments: ${service.deployments.length}`);
console.log(`Custom domains: ${service.domains.map(d => d.name).join(', ')}`);
```

## Create Service

```typescript
const service = await enclii.services.create({
  projectId: 'proj_abc123',
  name: 'api-service',
  type: 'web',  // 'web', 'worker', 'cron'
});
```

### From GitHub Repository

```typescript
const service = await enclii.services.create({
  projectId: 'proj_abc123',
  name: 'my-api',

  // GitHub integration
  github: {
    repository: 'madfam-org/my-app',
    branch: 'main',
    rootPath: 'apps/api',  // For monorepos
  },

  // Build configuration
  build: {
    type: 'dockerfile',  // or 'buildpack'
    dockerfile: './Dockerfile',
    context: '.',
  },
});
```

### With Full Configuration

```typescript
const service = await enclii.services.create({
  projectId: 'proj_abc123',
  name: 'production-api',

  // Runtime configuration
  runtime: {
    port: 3000,
    replicas: 2,
    resources: {
      requests: { cpu: '100m', memory: '128Mi' },
      limits: { cpu: '500m', memory: '512Mi' },
    },
  },

  // Health checks
  healthCheck: {
    path: '/health',
    interval: 30,
    timeout: 5,
    healthyThreshold: 2,
    unhealthyThreshold: 3,
  },

  // Environment variables
  env: {
    NODE_ENV: 'production',
    LOG_LEVEL: 'info',
  },

  // Auto-scaling
  autoscaling: {
    enabled: true,
    minReplicas: 2,
    maxReplicas: 10,
    targetCPU: 70,
  },

  // Custom domains
  domains: ['api.example.com'],
});
```

## Update Service

```typescript
// Update service configuration
const service = await enclii.services.update('svc_xyz789', {
  replicas: 3,
  env: {
    LOG_LEVEL: 'debug',
  },
});
```

### Update Resources

```typescript
await enclii.services.update('svc_xyz789', {
  resources: {
    requests: { cpu: '200m', memory: '256Mi' },
    limits: { cpu: '1000m', memory: '1Gi' },
  },
});
```

### Update Auto-scaling

```typescript
await enclii.services.update('svc_xyz789', {
  autoscaling: {
    enabled: true,
    minReplicas: 2,
    maxReplicas: 20,
    targetCPU: 60,
  },
});
```

## Delete Service

```typescript
// Delete a service
await enclii.services.delete('svc_xyz789', {
  confirm: true,
});
```

## Deploy Service

See [Deployments](./deployments) for detailed deployment documentation.

```typescript
// Quick deploy
const deployment = await enclii.services.deploy('svc_xyz789');

// Deploy with options
const deployment = await enclii.services.deploy('svc_xyz789', {
  environment: 'production',
  strategy: 'canary',
  canaryPercent: 10,
});

// Wait for deployment
await deployment.wait();
console.log(`Deployed: ${deployment.url}`);
```

## Environment Variables

### List Variables

```typescript
const variables = await enclii.services.listVariables('svc_xyz789');

for (const v of variables) {
  console.log(`${v.key}=${v.isSecret ? '***' : v.value}`);
}
```

### Set Variables

```typescript
// Set multiple variables
await enclii.services.setVariables('svc_xyz789', {
  API_KEY: 'secret-value',
  DEBUG: 'false',
}, {
  secrets: ['API_KEY'],  // Mark as secret
});

// Set single variable
await enclii.services.setVariable('svc_xyz789', 'NEW_VAR', 'value');
```

### Delete Variable

```typescript
await enclii.services.deleteVariable('svc_xyz789', 'OLD_VAR');
```

## Service Logs

```typescript
// Get recent logs
const logs = await enclii.services.logs('svc_xyz789', {
  tail: 100,
});

for (const log of logs) {
  console.log(`[${log.timestamp}] ${log.message}`);
}
```

### Stream Logs

```typescript
// Stream logs in real-time
const stream = await enclii.services.streamLogs('svc_xyz789');

stream.on('log', (log) => {
  console.log(`[${log.timestamp}] ${log.message}`);
});

stream.on('error', (error) => {
  console.error('Log stream error:', error);
});

// Stop streaming after 5 minutes
setTimeout(() => stream.stop(), 5 * 60 * 1000);
```

### Filter Logs

```typescript
const logs = await enclii.services.logs('svc_xyz789', {
  tail: 100,
  since: '1h',  // Last hour
  level: 'error',  // Only errors
  search: 'database',  // Search term
});
```

## Service Metrics

```typescript
const metrics = await enclii.services.metrics('svc_xyz789', {
  period: '1h',
  resolution: '5m',
});

console.log(`CPU: ${metrics.cpu.avg}%`);
console.log(`Memory: ${metrics.memory.avg}%`);
console.log(`Requests: ${metrics.requests.total}`);
console.log(`Errors: ${metrics.errors.total}`);
console.log(`Latency P95: ${metrics.latency.p95}ms`);
```

## Service Actions

### Restart Service

```typescript
// Restart all pods
await enclii.services.restart('svc_xyz789');

// Rolling restart
await enclii.services.restart('svc_xyz789', {
  strategy: 'rolling',
});
```

### Scale Service

```typescript
// Manual scaling
await enclii.services.scale('svc_xyz789', {
  replicas: 5,
});

// Scale to zero (suspend)
await enclii.services.scale('svc_xyz789', {
  replicas: 0,
});
```

### Exec into Service

```typescript
// Execute command in running container
const result = await enclii.services.exec('svc_xyz789', {
  command: ['npm', 'run', 'migrate'],
});

console.log(result.stdout);
if (result.exitCode !== 0) {
  console.error(result.stderr);
}
```

## Types

```typescript
interface Service {
  id: string;
  projectId: string;
  name: string;
  type: 'web' | 'worker' | 'cron';
  status: 'pending' | 'building' | 'deploying' | 'running' | 'stopped' | 'failed';
  url?: string;
  replicas: {
    desired: number;
    running: number;
    ready: number;
  };
  runtime: RuntimeConfig;
  build?: BuildConfig;
  github?: GitHubConfig;
  createdAt: string;
  updatedAt: string;
}

interface CreateServiceInput {
  projectId: string;
  name: string;
  type?: 'web' | 'worker' | 'cron';
  github?: GitHubConfig;
  build?: BuildConfig;
  runtime?: RuntimeConfig;
  env?: Record<string, string>;
  domains?: string[];
}

interface RuntimeConfig {
  port?: number;
  replicas?: number;
  resources?: ResourceConfig;
  healthCheck?: HealthCheckConfig;
  autoscaling?: AutoscalingConfig;
}
```

## Error Handling

```typescript
import {
  EncliiError,
  NotFoundError,
  ConflictError,
  DeploymentError
} from '@enclii/sdk';

try {
  await enclii.services.deploy('svc_xyz789');
} catch (error) {
  if (error instanceof DeploymentError) {
    console.log(`Deployment failed: ${error.reason}`);
    console.log(`Build logs: ${error.buildLogs}`);
  } else if (error instanceof ConflictError) {
    console.log('Deployment already in progress');
  }
}
```

## Related Documentation

- **SDK Overview**: [TypeScript SDK](/sdk/typescript/)
- **Deployments**: [Deployment Management](./deployments)
- **Domains**: [Custom Domains](./domains)
- **API Reference**: [Services API](/api-reference/#tag/services)
