# Switchyard UI

The Enclii web dashboard.

## Overview

Switchyard UI provides a modern web interface for:
- Project and service management
- Deployment monitoring and rollbacks
- Log viewing and search
- Team collaboration
- Domain and environment configuration

## Tech Stack

- **Framework**: Next.js 14 (App Router)
- **UI**: React 18 + Tailwind CSS + shadcn/ui
- **State**: TanStack Query (React Query)
- **Auth**: Janua SSO (OIDC)
- **Charts**: Recharts
- **Forms**: React Hook Form + Zod

## Quick Start

### Prerequisites

- Node.js 20+
- pnpm 8+

### Development Setup

```bash
# Install dependencies
pnpm install

# Copy environment template
cp .env.example .env.local
# Edit .env.local with your configuration

# Start development server
pnpm dev
# Dashboard available at http://localhost:3000
```

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `NEXT_PUBLIC_API_URL` | `http://localhost:8080` | Switchyard API URL |
| `NEXT_PUBLIC_AUTH_URL` | `https://auth.madfam.io` | Janua SSO URL |
| `NEXTAUTH_SECRET` | - | NextAuth.js secret |
| `NEXTAUTH_URL` | `http://localhost:3000` | App URL for OAuth |

## Project Structure

```
apps/switchyard-ui/
├── app/                    # Next.js App Router
│   ├── (auth)/             # Auth pages (login, logout)
│   ├── (dashboard)/        # Authenticated pages
│   │   ├── projects/       # Project management
│   │   ├── services/       # Service configuration
│   │   ├── deployments/    # Deployment history
│   │   ├── logs/           # Log viewer
│   │   └── settings/       # User settings
│   ├── api/                # API routes
│   └── layout.tsx          # Root layout
├── components/
│   ├── ui/                 # shadcn/ui components
│   ├── forms/              # Form components
│   ├── charts/             # Dashboard charts
│   └── layouts/            # Layout components
├── lib/
│   ├── api/                # API client
│   ├── auth/               # Auth utilities
│   └── utils/              # Helper functions
├── hooks/                  # Custom React hooks
├── types/                  # TypeScript types
└── styles/                 # Global styles
```

## Key Features

### Dashboard
- Real-time deployment status
- Resource usage metrics
- Recent activity feed
- Quick actions
- Truthful project card status computed from both service status/health and
  rollout state so stale `healthy` flags do not hide in-flight or blocked
  deployments.
- Shared `/v1/projects/:slug/services` mapping logic in
  `lib/project-card-transform.ts` keeps `/` and `/projects` cards aligned.

### Project Management
- Create/edit projects
- Environment configuration
- Team member management
- Access control settings

### Service Configuration
- Service settings editor
- Environment variables
- Custom domains
- Health check configuration

### Deployment Monitoring
- Deployment history
- Rollback capability
- Canary progress tracking
- Build logs viewer

### Log Viewer
- Real-time log streaming
- Filter by level/service/time
- Search functionality
- Download logs

## Development

### Running Tests

```bash
# Unit tests
pnpm test

# E2E tests (Playwright)
pnpm test:e2e

# Component tests
pnpm test:components
```

### Linting & Formatting

```bash
# Lint
pnpm lint

# Format
pnpm format

# Type check
pnpm typecheck
```

### Building

```bash
# Production build
pnpm build

# Analyze bundle
pnpm analyze
```

## Component Library

We use [shadcn/ui](https://ui.shadcn.com/) components. Add new components:

```bash
pnpm dlx shadcn-ui@latest add button
pnpm dlx shadcn-ui@latest add dialog
```

## API Integration

The UI uses TanStack Query for API calls:

```typescript
// hooks/useProjects.ts
import { useQuery, useMutation } from '@tanstack/react-query'
import { api } from '@/lib/api'

export function useProjects() {
  return useQuery({
    queryKey: ['projects'],
    queryFn: () => api.projects.list(),
  })
}

export function useCreateProject() {
  return useMutation({
    mutationFn: (data: CreateProjectInput) => api.projects.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] })
    },
  })
}
```

## Authentication

Authentication is handled directly in `contexts/AuthContext.tsx` (no NextAuth.js
dependency). The provider runs in one of two modes, selected by
`NEXT_PUBLIC_AUTH_MODE`:

- **`oidc`** (production) — a direct OAuth 2.0 / OIDC **PKCE** flow against
  Janua SSO (`auth.madfam.io`). `loginWithOIDC()` generates the PKCE verifier,
  stores it, and performs a top-level navigation to Janua's `authorize`
  endpoint.
- **`local`** (bootstrap/dev) — email/password directly against the Switchyard
  API, using the Janua `SignIn` component.

See [`AUTH_CONFIGURATION.md`](./AUTH_CONFIGURATION.md) for the full environment
matrix and Janua OAuth-client setup.

### Sign-in controls (account switching)

The login page (`app/login/page.tsx`) exposes three OIDC entry points, matching
the enclii admin-console (DISPATCH) switching model. They differ only by the
OIDC `prompt` parameter appended to the Janua `authorize` URL:

| Control | `prompt` | Behavior |
|---------|----------|----------|
| **Sign in with Janua SSO** | *(none)* | Default. Janua may silently reuse an existing SSO session. |
| **Switch account** | `select_account` | Janua shows its account chooser (for operators holding more than one MADFAM account). |
| **Sign in as someone else** | `login` | Forces a fresh credential entry, ignoring any existing SSO session. |

The `prompt` is appended **only when supplied**, so the default sign-in keeps
sending no prompt (silent session reuse). This is implemented in
`OIDCAuthProvider.login()` and covered by `contexts/AuthContext.test.tsx`.

> **Ecosystem note:** every MADFAM platform adopts this «Switch account /
> Sign in as someone else» model, **except Crea Tu Mundo MAP**.

## Deployment

The UI is deployed on Enclii (self-hosted):

```bash
enclii deploy --service switchyard-ui --env production
```

Production URL: https://app.enclii.dev

## Related Components

- **[Switchyard API](../switchyard-api/)** - Backend API
- **[CLI](../../packages/cli/)** - Command-line alternative

## License

Apache 2.0 - See [LICENSE](../../LICENSE)
