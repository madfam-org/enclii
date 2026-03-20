---
title: Authentication
description: Authentication patterns for the Enclii TypeScript SDK
sidebar_position: 2
tags: [sdk, typescript, authentication, api-key, oauth]
---

# SDK Authentication

The Enclii TypeScript SDK supports multiple authentication methods for different use cases.

## Authentication Methods

| Method | Use Case | Security Level |
|--------|----------|----------------|
| API Key | CI/CD, automation | High |
| Access Token | User sessions | High |
| Token Provider | Custom auth flows | High |

## API Key Authentication

Best for server-side applications and CI/CD pipelines.

### Creating an API Key

```bash
# Via CLI
enclii api-keys create --name "CI/CD Pipeline" --scopes "deploy,read"

# Output:
# API Key: ek_live_abc123xyz...
# Store this securely - it won't be shown again!
```

### Using API Keys

```typescript
import { EncliiClient } from '@enclii/sdk';

// Direct initialization
const enclii = new EncliiClient({
  apiKey: 'ek_live_abc123xyz...',
});

// From environment variable (recommended)
const enclii = new EncliiClient({
  apiKey: process.env.ENCLII_API_KEY,
});

// Auto-detect from environment
const enclii = new EncliiClient();
// Automatically uses ENCLII_API_KEY
```

### API Key Scopes

| Scope | Permissions |
|-------|------------|
| `read` | List and view resources |
| `write` | Create and update resources |
| `deploy` | Trigger deployments |
| `delete` | Delete resources |
| `admin` | Full access including secrets |

```typescript
// Create scoped API key via SDK
const apiKey = await enclii.apiKeys.create({
  name: 'Read-Only Dashboard',
  scopes: ['read'],
  expiresIn: '90d', // Optional expiration
});
```

## Access Token Authentication

For user-facing applications with OAuth/OIDC authentication.

### Token from Janua SSO

```typescript
import { EncliiClient } from '@enclii/sdk';

// After OAuth flow, you have an access token
const accessToken = 'eyJhbGciOiJSUzI1NiIs...';

const enclii = new EncliiClient({
  accessToken,
});

// Make authenticated requests
const user = await enclii.users.me();
console.log(`Logged in as: ${user.email}`);
```

### Token Refresh

```typescript
import { EncliiClient } from '@enclii/sdk';

// With refresh token handling
const enclii = new EncliiClient({
  accessToken: initialToken,
  refreshToken: refreshToken,
  onTokenRefresh: async (newTokens) => {
    // Store new tokens
    await saveTokens(newTokens);
  },
});

// SDK automatically refreshes expired tokens
const projects = await enclii.projects.list();
```

## Custom Token Provider

For advanced authentication flows or token management.

```typescript
import { EncliiClient } from '@enclii/sdk';

// Dynamic token provider
const enclii = new EncliiClient({
  tokenProvider: async () => {
    // Fetch token from your auth service
    const response = await fetch('/api/auth/token');
    const { accessToken } = await response.json();
    return accessToken;
  },
});

// Token provider is called before each request if token is expired
```

### With Caching

```typescript
let cachedToken: { token: string; expiresAt: number } | null = null;

const enclii = new EncliiClient({
  tokenProvider: async () => {
    // Return cached token if still valid
    if (cachedToken && cachedToken.expiresAt > Date.now()) {
      return cachedToken.token;
    }

    // Fetch new token
    const response = await fetch('/api/auth/token');
    const { accessToken, expiresIn } = await response.json();

    // Cache the new token
    cachedToken = {
      token: accessToken,
      expiresAt: Date.now() + (expiresIn * 1000) - 60000, // 1 min buffer
    };

    return accessToken;
  },
});
```

## Authentication in Different Environments

### Browser (SPA)

```typescript
// Don't expose API keys in browser code!
// Use access tokens from your backend

import { EncliiClient } from '@enclii/sdk';

// Get token from your auth flow
const accessToken = await getAccessToken();

const enclii = new EncliiClient({
  accessToken,
});
```

### Node.js Server

```typescript
import { EncliiClient } from '@enclii/sdk';

// API key from environment (secure)
const enclii = new EncliiClient({
  apiKey: process.env.ENCLII_API_KEY,
});

// Or per-request authentication
app.post('/deploy', async (req, res) => {
  const userToken = req.headers.authorization?.split(' ')[1];

  const userEnclii = new EncliiClient({
    accessToken: userToken,
  });

  const deployment = await userEnclii.services.deploy(req.body.serviceId);
  res.json(deployment);
});
```

### Edge Functions (Vercel, Cloudflare)

```typescript
// Works in edge runtime
import { EncliiClient } from '@enclii/sdk';

export default async function handler(request: Request) {
  const enclii = new EncliiClient({
    apiKey: process.env.ENCLII_API_KEY,
  });

  const services = await enclii.services.list();
  return new Response(JSON.stringify(services));
}
```

### CI/CD (GitHub Actions)

```yaml
# .github/workflows/deploy.yml
name: Deploy
on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Deploy to Enclii
        env:
          ENCLII_API_KEY: ${{ secrets.ENCLII_API_KEY }}
        run: |
          npm install @enclii/sdk
          node deploy.js
```

```javascript
// deploy.js
import { EncliiClient } from '@enclii/sdk';

const enclii = new EncliiClient();

const deployment = await enclii.services.deploy('my-service-id', {
  environment: 'production',
});

console.log(`Deployed: ${deployment.url}`);
```

## Security Best Practices

### Never Expose API Keys

```typescript
// ❌ BAD: API key in client-side code
const enclii = new EncliiClient({
  apiKey: 'ek_live_abc123', // Exposed to users!
});

// ✅ GOOD: Use access tokens or server-side proxy
const enclii = new EncliiClient({
  accessToken: userAccessToken,
});
```

### Use Environment Variables

```typescript
// ❌ BAD: Hardcoded credentials
const enclii = new EncliiClient({
  apiKey: 'ek_live_abc123xyz',
});

// ✅ GOOD: Environment variables
const enclii = new EncliiClient({
  apiKey: process.env.ENCLII_API_KEY,
});
```

### Scope API Keys Appropriately

```bash
# ❌ BAD: Full access key for read-only use
enclii api-keys create --name "Dashboard" --scopes "admin"

# ✅ GOOD: Minimal required scopes
enclii api-keys create --name "Dashboard" --scopes "read"
enclii api-keys create --name "Deploy Bot" --scopes "read,deploy"
```

### Rotate Keys Regularly

```typescript
// Rotate API keys periodically
const newKey = await enclii.apiKeys.rotate('key-id');

// Update your secrets
await updateSecret('ENCLII_API_KEY', newKey.value);

// Old key is invalidated immediately
```

## Troubleshooting

### "Unauthorized" Error

```typescript
try {
  await enclii.projects.list();
} catch (error) {
  if (error.code === 'UNAUTHORIZED') {
    // Check: Is your API key valid?
    // Check: Has the token expired?
    // Check: Is the key/token for the correct environment?
  }
}
```

### "Forbidden" Error

```typescript
try {
  await enclii.services.delete('service-id');
} catch (error) {
  if (error.code === 'FORBIDDEN') {
    // Check: Does your API key have 'delete' scope?
    // Check: Do you have permission for this resource?
  }
}
```

### Token Expiration

```typescript
// Handle token expiration gracefully
const enclii = new EncliiClient({
  accessToken,
  onTokenExpired: async () => {
    // Redirect to login or refresh token
    window.location.href = '/login';
  },
});
```

## Related Documentation

- **SDK Overview**: [TypeScript SDK](/sdk/typescript/)
- **CLI Auth**: [CLI Authentication](/guides/cli-auth-setup)
- **Auth Troubleshooting**: [Auth Problems](/troubleshooting/auth-problems)
- **API Reference**: [API Docs](/api-reference/)
