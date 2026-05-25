---
title: Migrating from Vercel
description: 10-minute path from a Vercel project to Enclii
sidebar_position: 1
tags: [migration, vercel, guides]
---

# Migrating from Vercel to Enclii

This is the short path: get your Vercel project running on Enclii in about 10 minutes. For deep-dive edge cases, redirects, ISR, and incremental rollouts, see the [full Vercel migration guide](./VERCEL_MIGRATION_GUIDE.md).

## Feature parity at a glance

| Capability | Vercel | Enclii |
|---|---|---|
| Git-push deploys | Native | Native (GitHub App) |
| Preview environments per PR | Native | Native (P1.7) |
| Custom domains + automatic TLS | Native | Native (Cloudflare for SaaS) |
| Next.js SSR / ISR | Native | Supported (Paketo buildpack) |
| Serverless functions | Native | Supported (`enclii functions`) |
| **Edge Functions / Edge Middleware** | Native | **Not supported** — use Cloudflare Workers |
| Analytics | Native | PostHog integration |
| Environment variables + secrets | Native | `enclii secrets set` |
| Rollback | One click | `enclii rollback` |

**Known gaps:**
- Vercel Edge Runtime (V8 isolates) — not available. Port edge routes to Cloudflare Workers or convert to regular SSR.
- Vercel Analytics — use the PostHog integration instead.
- `vercel.json` `rewrites`/`redirects` — map to your framework's config (Next.js `next.config.js`) or a Cloudflare Worker.

## 10-minute migration

### 1. Export your Vercel configuration (2 min)

```bash
# From your project directory
vercel env pull .env.production
```

Note your build settings in the Vercel dashboard:
- **Framework preset** (Next.js, Remix, Astro, etc.)
- **Build command** (default: `next build`)
- **Output directory** (default: `.next`)
- **Node version**
- **Environment variables** (now in `.env.production`)

### 2. Initialize Enclii in the same repo (1 min)

```bash
enclii init
```

The CLI auto-detects Next.js and writes `service.yaml`:

```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: my-app
spec:
  build:
    type: auto        # Paketo auto-detect
  runtime:
    port: 3000
    replicas: 2
    healthCheck: /api/health
```

If you used a custom build command in Vercel, add it:

```yaml
spec:
  build:
    type: auto
    command: npm run build:custom
```

### 3. Port environment variables (2 min)

```bash
# Import everything from .env.production as Enclii secrets
while IFS='=' read -r key value; do
  [ -z "$key" ] || [[ "$key" =~ ^# ]] && continue
  enclii secrets set "$key" "$value" --env prod
done < .env.production
```

Or set them one at a time:

```bash
enclii secrets set DATABASE_URL "postgresql://..." --env prod
enclii secrets set STRIPE_SECRET_KEY "sk_live_..." --env prod
```

### 4. Add a `/health` endpoint (2 min)

Enclii uses HTTP health probes. In Next.js App Router:

```typescript
// app/api/health/route.ts
export async function GET() {
  return Response.json({ status: 'ok' });
}
```

Pages Router:

```typescript
// pages/api/health.ts
export default function handler(req, res) {
  res.status(200).json({ status: 'ok' });
}
```

### 5. Deploy (2 min)

```bash
enclii deploy --env prod
```

Enclii runs the Paketo Node.js buildpack, pushes to `ghcr.io`, and rolls out to `my-app.enclii.dev`.

### 6. Point your domain (1 min)

```bash
enclii domains add myapp.com --env prod
enclii domains verify myapp.com
```

Update DNS per the verification output (usually a single `CNAME` to `<service>.enclii.dev`). Cloudflare for SaaS issues the TLS cert automatically.

When the new domain is healthy, delete the Vercel project or point its domain away.

## What's different at runtime

- Your app runs as a container (not a Lambda). Cold starts are gone, but idle apps hold their 2 replicas by default. Scale to zero via `spec.runtime.replicas: 0` with Knative (P3.x).
- No platform-enforced request timeout. Long-running requests just work.
- Build output goes to a container image in `ghcr.io`, not to Vercel's edge cache. Static assets are served through Cloudflare by default.

## Next steps

- [Full Vercel migration guide](./VERCEL_MIGRATION_GUIDE.md) — edge cases, ISR, incremental rollouts
- [Templates](../templates/templates.md) — Next.js starter (coming soon)
- [Onboarding guide](./ONBOARDING_GUIDE.md) — per-PR preview environments (P1.7)
- [Custom domains](../cli/commands/domains.md) — apex domains, wildcards, multi-environment

<!-- TODO(post-first-customer): Document Vercel cron → Enclii jobs conversion with real customer examples -->
<!-- TODO(post-first-customer): Add Image Optimization equivalent guidance once a customer hits it -->
