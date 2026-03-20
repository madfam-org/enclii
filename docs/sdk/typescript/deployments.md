---
title: Deployments
description: Manage deployments with the Enclii TypeScript SDK
sidebar_position: 5
tags: [sdk, typescript, deployments, rollback]
---

# Deployments

Manage deployments using the TypeScript SDK.

## Overview

Deployments represent the lifecycle of releasing your code to an environment. The SDK provides full control over the deployment process.

```typescript
import { EncliiClient } from '@enclii/sdk';

const enclii = new EncliiClient();

// Deployments module
enclii.deployments.list(serviceId);
enclii.deployments.get(deploymentId);
enclii.deployments.create(serviceId, options);
enclii.deployments.promote(deploymentId);
enclii.deployments.abort(deploymentId);
enclii.deployments.rollback(serviceId, releaseId);
```

## Deploy a Service

### Basic Deployment

```typescript
// Deploy using latest code
const deployment = await enclii.services.deploy('svc_xyz789');

console.log(`Deployment started: ${deployment.id}`);
console.log(`Status: ${deployment.status}`);
```

### Wait for Completion

```typescript
const deployment = await enclii.services.deploy('svc_xyz789');

// Wait for deployment to complete
await deployment.wait();

if (deployment.status === 'succeeded') {
  console.log(`Deployed successfully: ${deployment.url}`);
} else {
  console.error(`Deployment failed: ${deployment.error}`);
}
```

### With Progress Updates

```typescript
const deployment = await enclii.services.deploy('svc_xyz789');

// Subscribe to progress
deployment.on('progress', (event) => {
  console.log(`${event.phase}: ${event.message}`);
});

deployment.on('complete', (result) => {
  console.log(`Deployment ${result.status}`);
});

deployment.on('error', (error) => {
  console.error(`Error: ${error.message}`);
});

await deployment.wait();
```

## Deployment Options

### Environment Selection

```typescript
// Deploy to specific environment
const deployment = await enclii.services.deploy('svc_xyz789', {
  environment: 'production',
});

// Deploy to staging first
const stagingDeploy = await enclii.services.deploy('svc_xyz789', {
  environment: 'staging',
});
```

### Deployment Strategies

```typescript
// Rolling update (default)
const deployment = await enclii.services.deploy('svc_xyz789', {
  strategy: 'rolling',
});

// Canary deployment
const deployment = await enclii.services.deploy('svc_xyz789', {
  strategy: 'canary',
  canaryPercent: 10,  // Start with 10% traffic
  canaryTimeout: 300, // Auto-promote after 5 min if healthy
});

// Blue-green deployment
const deployment = await enclii.services.deploy('svc_xyz789', {
  strategy: 'blue-green',
});

// Recreate (stop old, start new)
const deployment = await enclii.services.deploy('svc_xyz789', {
  strategy: 'recreate',
});
```

### Specific Commit

```typescript
// Deploy specific commit
const deployment = await enclii.services.deploy('svc_xyz789', {
  commit: 'abc123',
});

// Deploy specific branch
const deployment = await enclii.services.deploy('svc_xyz789', {
  branch: 'feature/new-api',
});

// Deploy specific tag
const deployment = await enclii.services.deploy('svc_xyz789', {
  tag: 'v1.2.3',
});
```

### With Pre/Post Hooks

```typescript
const deployment = await enclii.services.deploy('svc_xyz789', {
  preDeploy: 'npm run migrate',
  postDeploy: 'npm run seed',
});
```

## List Deployments

```typescript
// List all deployments for a service
const deployments = await enclii.deployments.list('svc_xyz789');

for (const d of deployments) {
  console.log(`${d.id}: ${d.status} (${d.createdAt})`);
}
```

### With Filtering

```typescript
// Filter by status
const failed = await enclii.deployments.list('svc_xyz789', {
  status: 'failed',
});

// Filter by environment
const prodDeployments = await enclii.deployments.list('svc_xyz789', {
  environment: 'production',
});

// Filter by date range
const recentDeployments = await enclii.deployments.list('svc_xyz789', {
  since: '2024-01-01',
  until: '2024-01-31',
});
```

## Get Deployment Details

```typescript
const deployment = await enclii.deployments.get('deploy_abc123');

console.log(`Status: ${deployment.status}`);
console.log(`Environment: ${deployment.environment}`);
console.log(`Strategy: ${deployment.strategy}`);
console.log(`Started: ${deployment.startedAt}`);
console.log(`Finished: ${deployment.finishedAt}`);
console.log(`Duration: ${deployment.duration}s`);
```

### Build Logs

```typescript
const deployment = await enclii.deployments.get('deploy_abc123', {
  include: ['buildLogs'],
});

console.log('Build logs:');
console.log(deployment.buildLogs);
```

### Deployment Events

```typescript
const events = await enclii.deployments.listEvents('deploy_abc123');

for (const event of events) {
  console.log(`[${event.timestamp}] ${event.type}: ${event.message}`);
}
```

## Canary Deployments

### Create Canary

```typescript
const deployment = await enclii.services.deploy('svc_xyz789', {
  strategy: 'canary',
  canaryPercent: 5,  // Start with 5% traffic
});

console.log(`Canary deployment: ${deployment.id}`);
```

### Monitor Canary

```typescript
// Watch canary metrics
const metrics = await enclii.deployments.getCanaryMetrics('deploy_abc123');

console.log(`Traffic: ${metrics.canaryPercent}%`);
console.log(`Error rate: ${metrics.errorRate}%`);
console.log(`Latency P95: ${metrics.latencyP95}ms`);
```

### Promote Canary

```typescript
// Gradually increase traffic
await enclii.deployments.updateCanary('deploy_abc123', {
  percent: 50,  // Increase to 50%
});

// Check metrics, then promote to 100%
await enclii.deployments.promote('deploy_abc123');
```

### Abort Canary

```typescript
// If metrics look bad, abort
if (metrics.errorRate > 5) {
  await enclii.deployments.abort('deploy_abc123');
  console.log('Canary aborted, traffic restored to previous version');
}
```

## Rollback

### To Previous Version

```typescript
// Rollback to the previous release
const rollback = await enclii.services.rollback('svc_xyz789');

console.log(`Rolled back to: ${rollback.targetRelease}`);
await rollback.wait();
```

### To Specific Release

```typescript
// List available releases
const releases = await enclii.services.listReleases('svc_xyz789');

for (const release of releases) {
  console.log(`${release.id}: ${release.commit.substring(0, 7)} (${release.createdAt})`);
}

// Rollback to specific release
const rollback = await enclii.services.rollback('svc_xyz789', {
  releaseId: 'rel_xyz789',
});
```

### Rollback in CI/CD

```typescript
// Auto-rollback on deployment failure
try {
  const deployment = await enclii.services.deploy('svc_xyz789');
  await deployment.wait();

  if (deployment.status === 'failed') {
    throw new Error(`Deployment failed: ${deployment.error}`);
  }
} catch (error) {
  console.error('Deployment failed, rolling back...');
  await enclii.services.rollback('svc_xyz789');
  throw error;
}
```

## Releases

### List Releases

```typescript
const releases = await enclii.services.listReleases('svc_xyz789');

for (const release of releases) {
  console.log(`${release.id}:`);
  console.log(`  Commit: ${release.commit}`);
  console.log(`  Image: ${release.image}`);
  console.log(`  Created: ${release.createdAt}`);
  console.log(`  Status: ${release.status}`);
}
```

### Get Release Details

```typescript
const release = await enclii.releases.get('rel_xyz789');

console.log(`Commit: ${release.commit}`);
console.log(`Author: ${release.commitAuthor}`);
console.log(`Message: ${release.commitMessage}`);
console.log(`Image: ${release.image}`);
console.log(`SBOM: ${release.sbomUrl}`);
```

## Deployment Automation

### GitHub Actions

```typescript
// deploy.ts - Run in GitHub Actions
import { EncliiClient } from '@enclii/sdk';

async function deploy() {
  const enclii = new EncliiClient();

  const deployment = await enclii.services.deploy(process.env.SERVICE_ID!, {
    environment: process.env.ENVIRONMENT || 'staging',
    commit: process.env.GITHUB_SHA,
  });

  deployment.on('progress', (event) => {
    console.log(`::notice::${event.message}`);
  });

  await deployment.wait();

  if (deployment.status !== 'succeeded') {
    console.log(`::error::Deployment failed: ${deployment.error}`);
    process.exit(1);
  }

  console.log(`::notice::Deployed to ${deployment.url}`);
}

deploy();
```

### Scheduled Deployments

```typescript
// Schedule deployment for off-hours
const deployment = await enclii.services.deploy('svc_xyz789', {
  environment: 'production',
  scheduledFor: '2024-01-15T03:00:00Z',  // 3 AM UTC
});

console.log(`Deployment scheduled for: ${deployment.scheduledFor}`);
```

## Types

```typescript
interface Deployment {
  id: string;
  serviceId: string;
  status: 'pending' | 'building' | 'deploying' | 'succeeded' | 'failed' | 'aborted';
  environment: string;
  strategy: 'rolling' | 'canary' | 'blue-green' | 'recreate';
  commit?: string;
  branch?: string;
  releaseId?: string;
  url?: string;
  error?: string;
  startedAt: string;
  finishedAt?: string;
  duration?: number;
}

interface DeployOptions {
  environment?: string;
  strategy?: 'rolling' | 'canary' | 'blue-green' | 'recreate';
  commit?: string;
  branch?: string;
  tag?: string;
  canaryPercent?: number;
  canaryTimeout?: number;
  preDeploy?: string;
  postDeploy?: string;
  scheduledFor?: string;
}

interface Release {
  id: string;
  serviceId: string;
  commit: string;
  commitMessage: string;
  commitAuthor: string;
  image: string;
  sbomUrl?: string;
  status: 'active' | 'superseded' | 'failed';
  createdAt: string;
}
```

## Error Handling

```typescript
import {
  DeploymentError,
  BuildError,
  TimeoutError,
  ConcurrencyError
} from '@enclii/sdk';

try {
  const deployment = await enclii.services.deploy('svc_xyz789');
  await deployment.wait();
} catch (error) {
  if (error instanceof BuildError) {
    console.error('Build failed:', error.buildLogs);
  } else if (error instanceof TimeoutError) {
    console.error('Deployment timed out');
  } else if (error instanceof ConcurrencyError) {
    console.error('Another deployment is in progress');
  } else if (error instanceof DeploymentError) {
    console.error('Deployment failed:', error.message);
  }
}
```

## Related Documentation

- **SDK Overview**: [TypeScript SDK](/sdk/typescript/)
- **Services**: [Service Management](./services)
- **Troubleshooting**: [Deployment Issues](/troubleshooting/deployment-issues)
- **API Reference**: [Deployments API](/api-reference/#tag/deployments)
