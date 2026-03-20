---
title: TypeScript SDK
description: Official TypeScript/JavaScript SDK for the Enclii API
sidebar_position: 1
tags: [sdk, typescript, javascript, api]
---

# Enclii TypeScript SDK

The official TypeScript/JavaScript SDK for interacting with the Enclii API.

## Installation

```bash
# npm
npm install @enclii/sdk

# yarn
yarn add @enclii/sdk

# pnpm
pnpm add @enclii/sdk
```

## Quick Start

```typescript
import { EncliiClient } from '@enclii/sdk';

// Initialize the client
const enclii = new EncliiClient({
  apiKey: process.env.ENCLII_API_KEY,
});

// List your projects
const projects = await enclii.projects.list();
console.log(projects);

// Deploy a service
const deployment = await enclii.services.deploy('service-id', {
  environment: 'production',
});
console.log(`Deployment started: ${deployment.id}`);
```

## Requirements

- Node.js 18+ or compatible runtime (Deno, Bun)
- TypeScript 5.0+ (for TypeScript users)

## Features

- **Full API Coverage**: All Enclii API endpoints
- **TypeScript First**: Complete type definitions
- **Modern**: ESM and CommonJS support
- **Lightweight**: Zero runtime dependencies
- **Tree-shakeable**: Only bundle what you use

## Authentication Methods

The SDK supports multiple authentication methods:

### API Key (Recommended for CI/CD)

```typescript
const enclii = new EncliiClient({
  apiKey: 'ek_live_xxx...',
});
```

### Access Token (For user sessions)

```typescript
const enclii = new EncliiClient({
  accessToken: 'eyJhbG...',
});
```

### Custom Token Provider

```typescript
const enclii = new EncliiClient({
  tokenProvider: async () => {
    // Return fresh token
    return getTokenFromStore();
  },
});
```

See [Authentication Guide](./authentication) for more details.

## SDK Modules

| Module | Description |
|--------|-------------|
| [Projects](./projects) | Create and manage projects |
| [Services](./services) | Manage services and configurations |
| [Deployments](./deployments) | Deploy and monitor deployments |
| [Domains](./domains) | Configure custom domains |

## Error Handling

```typescript
import { EncliiClient, EncliiError, RateLimitError } from '@enclii/sdk';

try {
  const project = await enclii.projects.get('invalid-id');
} catch (error) {
  if (error instanceof RateLimitError) {
    // Wait and retry
    await sleep(error.retryAfter * 1000);
    return retry();
  }

  if (error instanceof EncliiError) {
    console.error(`API Error: ${error.code} - ${error.message}`);
    console.error(`Request ID: ${error.requestId}`);
  }

  throw error;
}
```

## Configuration Options

```typescript
const enclii = new EncliiClient({
  // Authentication (one required)
  apiKey: 'ek_live_xxx',
  accessToken: 'eyJhbG...',
  tokenProvider: async () => 'token',

  // Optional settings
  baseUrl: 'https://api.enclii.dev',  // Default
  timeout: 30000,                      // 30 seconds
  retries: 3,                          // Automatic retries

  // Hooks
  onRequest: (config) => {
    console.log(`Request: ${config.method} ${config.url}`);
  },
  onResponse: (response) => {
    console.log(`Response: ${response.status}`);
  },
  onError: (error) => {
    console.error(`Error: ${error.message}`);
  },
});
```

## Environment Variables

The SDK automatically reads these environment variables:

| Variable | Purpose |
|----------|---------|
| `ENCLII_API_KEY` | API key for authentication |
| `ENCLII_API_URL` | Custom API endpoint |
| `ENCLII_DEBUG` | Enable debug logging |

```typescript
// Uses ENCLII_API_KEY automatically
const enclii = new EncliiClient();
```

## Framework Integration

### Next.js

```typescript
// lib/enclii.ts
import { EncliiClient } from '@enclii/sdk';

export const enclii = new EncliiClient({
  apiKey: process.env.ENCLII_API_KEY,
});

// app/api/deploy/route.ts
import { enclii } from '@/lib/enclii';

export async function POST(request: Request) {
  const { serviceId } = await request.json();
  const deployment = await enclii.services.deploy(serviceId);
  return Response.json(deployment);
}
```

### Express

```typescript
import express from 'express';
import { EncliiClient } from '@enclii/sdk';

const app = express();
const enclii = new EncliiClient();

app.get('/projects', async (req, res) => {
  const projects = await enclii.projects.list();
  res.json(projects);
});
```

### GitHub Actions

```yaml
- name: Deploy to Enclii
  env:
    ENCLII_API_KEY: ${{ secrets.ENCLII_API_KEY }}
  run: |
    npx @enclii/sdk deploy --service my-service
```

## Related Documentation

- **API Reference**: [OpenAPI Docs](/api-reference/)
- **Authentication**: [Auth Guide](./authentication)
- **Go SDK**: [Go SDK](https://github.com/madfam-io/enclii/tree/main/packages/sdk-go)
- **CLI**: [CLI Reference](/cli/)
