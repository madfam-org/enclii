---
title: Authentication
description: Authentication patterns for the Enclii TypeScript SDK
sidebar_position: 2
tags: [sdk, typescript, authentication, api-token, oauth]
---

# SDK Authentication

The Enclii TypeScript SDK (`@madfam/enclii-sdk`) authenticates every request with a bearer token. There is one option, `token`, and it sends `Authorization: Bearer <token>`. The SDK has no separate "API key" concept, no `apiKey`/`accessToken`/`tokenProvider` options, and no `apiKeys` resource. It also does not read credentials from environment variables on its own: pass the token explicitly.

## What the `token` option accepts

| Value | Behaviour |
|-------|-----------|
| A string | Static token, sent on every request |
| An async function `() => Promise<string \| null \| undefined>` | Called before **every** request; return `null`/`undefined` to send no header. The SDK does not cache the value, so the function owns any caching. |
| An `AuthStrategy` object (`{ getToken(): Promise<string \| null \| undefined> }`) | Used as is |
| `null` or omitted | Anonymous: no `Authorization` header (health endpoints only) |

`baseUrl` is required and must include the `/v1` prefix; the client does not append it.

## Which tokens the API accepts

The Enclii API accepts two kinds of bearer token, and the SDK sends both the same way:

| Token | Where it comes from | Typical use |
|-------|---------------------|-------------|
| Personal API token (`enclii_…`) | [`enclii tokens create`](../../cli/commands/tokens.md) | CI/CD, scripts, server-side automation |
| OIDC access token (a JWT) | Janua SSO, for example the token `enclii login` stores | User-facing apps acting as the signed-in user |

## Personal API tokens (CI/CD and automation)

Create a token with the CLI. The plaintext value is printed once, to stderr, and cannot be retrieved later.

```bash
enclii tokens create --name "ci-deploy" --expires-in 30d
```

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | | Human-readable token name (required) |
| `--expires-in` | `90d` | Lifetime: Go duration syntax extended with `d` for days (`24h`, `30d`, `90d`) |
| `--scopes` | full account access | Comma-separated scope list |
| `--json` | `false` | Emit machine-readable JSON to stdout |

Manage tokens with `enclii tokens list`, `enclii tokens get <id>`, and `enclii tokens revoke <id>`. There is no `enclii api-keys` command, and no rotate operation: to rotate, create a new token, switch your secret to it, then revoke the old one.

About scopes: the API currently acts on one scope value, `admin`, which gives the token the admin role. Any other scope string is stored with the token but not enforced, so a token without `admin` has the developer role on the account that created it. Limit exposure with short `--expires-in` values and prompt revocation rather than relying on scope strings.

Use the token with the SDK:

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});

const projects = await enclii.projects.list();
```

`ENCLII_API_TOKEN` in this example is your own variable name (it matches what the CLI reads); the SDK itself only sees the value you pass.

## OIDC access tokens (user sessions)

When your application already holds a Janua access token for the signed-in user, pass it as `token`. The SDK does not refresh tokens; supply a function if the token can change during the client's lifetime:

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  // Called before every request; return a current access token.
  token: async () => getAccessTokenFromYourSession(),
});
```

### Caching inside the provider

```typescript
let cached: { token: string; expiresAt: number } | null = null;

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: async () => {
    if (cached && cached.expiresAt > Date.now()) return cached.token;

    const res = await fetch('/api/auth/token');
    const { accessToken, expiresIn } = await res.json();
    cached = { token: accessToken, expiresAt: Date.now() + expiresIn * 1000 - 60_000 };
    return accessToken;
  },
});
```

## Environments

### Browser

Never ship a personal API token to a browser. Use the signed-in user's access token, obtained through your own backend or auth flow, with a token function as shown above.

### Node.js server, per-request identity

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

app.get('/projects', async (req, res) => {
  const userToken = req.headers.authorization?.split(' ')[1];
  const userEnclii = new EncliiClient({
    baseUrl: 'https://api.enclii.dev/v1',
    token: userToken,
  });
  res.json(await userEnclii.projects.list());
});
```

### CI/CD (GitHub Actions)

```yaml
- name: Call Enclii
  env:
    ENCLII_API_TOKEN: ${{ secrets.ENCLII_API_TOKEN }}
  run: node deploy.mjs
```

```javascript
// deploy.mjs
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});

const dep = await enclii.deployments.deploy('svc_123', {
  release_id: 'rel_1',
  environment_name: 'prod',
});
await enclii.deployments.wait(dep.id, { timeoutMs: 10 * 60_000 });
```

## Handling auth errors

Failed requests throw typed errors; check them with `instanceof`:

```typescript
import { AuthenticationError, AuthorizationError } from '@madfam/enclii-sdk';

try {
  await enclii.projects.list();
} catch (err) {
  if (err instanceof AuthenticationError) {
    // HTTP 401: the token is missing, expired, revoked, or malformed.
  } else if (err instanceof AuthorizationError) {
    // HTTP 403: the identity lacks permission for this resource.
  } else {
    throw err;
  }
}
```

Every SDK error carries `method`, `path`, `status`, and (when the API returns one) `requestId`.

## Related Documentation

- **SDK Overview**: [TypeScript SDK](/sdk/typescript/)
- **CLI tokens**: [`enclii tokens`](/cli/commands/tokens)
- **CLI Auth**: [CLI Authentication](/guides/cli-auth-setup)
- **Auth Troubleshooting**: [Auth Problems](/troubleshooting/auth-problems)
- **API Reference**: [API Docs](/api-reference/)
