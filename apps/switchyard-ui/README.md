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

Authentication uses NextAuth.js with Janua as the OIDC provider:

```typescript
// app/api/auth/[...nextauth]/route.ts
import NextAuth from 'next-auth'
import { JanuaProvider } from '@/lib/auth/janua'

export const authOptions = {
  providers: [
    JanuaProvider({
      clientId: process.env.JANUA_CLIENT_ID!,
      clientSecret: process.env.JANUA_CLIENT_SECRET!,
      issuer: process.env.NEXT_PUBLIC_AUTH_URL,
    }),
  ],
}
```

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
