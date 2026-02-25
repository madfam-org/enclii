# Authentication Configuration

## Overview

Switchyard UI supports two authentication modes:
1. **Local Mode** (default) - Email/password authentication directly against Switchyard API
2. **OIDC Mode** - SSO via external identity provider (Janua)

## Environment Variables

### UI Configuration (Next.js)

```bash
# API endpoint (required)
NEXT_PUBLIC_API_URL=https://api.enclii.dev

# Authentication mode: "local" or "oidc"
NEXT_PUBLIC_AUTH_MODE=local

# Janua URL (required when AUTH_MODE=oidc)
# Note: Janua is a MADFAM shared service, not Enclii-specific
NEXT_PUBLIC_JANUA_URL=https://auth.madfam.io
```

### API Configuration (Switchyard API)

```bash
# Authentication mode: "local" or "oidc"
ENCLII_AUTH_MODE=local

# OIDC Configuration (required when AUTH_MODE=oidc)
# Janua is deployed at auth.madfam.io (shared MADFAM SSO)
ENCLII_OIDC_ISSUER=https://auth.madfam.io
ENCLII_OIDC_CLIENT_ID=switchyard
ENCLII_OIDC_CLIENT_SECRET=your-client-secret
ENCLII_OIDC_REDIRECT_URL=https://api.enclii.dev/v1/auth/callback

# External token validation (CLI direct access)
ENCLII_EXTERNAL_JWKS_URL=https://auth.madfam.io/.well-known/jwks.json
ENCLII_EXTERNAL_ISSUER=https://auth.madfam.io
```

## Deployment Configurations

### Local Development (Local Auth)

```bash
# .env.local
NEXT_PUBLIC_API_URL=http://localhost:4200
NEXT_PUBLIC_AUTH_MODE=local
```

### Production with Local Auth (Bootstrap)

```bash
# Build args for Docker
docker build \
  --build-arg NEXT_PUBLIC_API_URL=https://api.enclii.dev \
  --build-arg NEXT_PUBLIC_AUTH_MODE=local \
  -t switchyard-ui:latest .
```

### Production with Janua SSO

```bash
# Build args for Docker
# Note: Janua is at auth.madfam.io (MADFAM shared SSO)
docker build \
  --build-arg NEXT_PUBLIC_API_URL=https://api.enclii.dev \
  --build-arg NEXT_PUBLIC_AUTH_MODE=oidc \
  --build-arg NEXT_PUBLIC_JANUA_URL=https://auth.madfam.io \
  -t switchyard-ui:latest .
```

## Janua SSO Setup

### 1. Deploy Janua

Deploy Janua via Enclii or manually:
```bash
# Via Enclii (future)
enclii deploy --template janua --env production

# Or via Kubernetes
kubectl apply -f dogfooding/janua.yaml
```

### 2. Create OAuth Client in Janua

1. Access Janua admin: `https://auth.madfam.io/admin`
2. Create a new OAuth2 client:
   - **Client ID**: `switchyard`
   - **Client Secret**: Generate secure secret
   - **Redirect URIs**:
     - `https://api.enclii.dev/v1/auth/callback`
     - `https://app.enclii.dev/auth/callback`
   - **Grant Types**: `authorization_code`, `refresh_token`
   - **Scopes**: `openid`, `profile`, `email`

### 3. Configure Audience Validation

Enclii validates the `aud` (audience) claim on all external Janua tokens. This
ensures tokens issued for other MADFAM apps (e.g., Dhanam, Tezca) cannot be used
to access Enclii's API.

```bash
# Set in environment or .env
ENCLII_JANUA_AUDIENCE=enclii-api
```

The audience is validated in `apps/switchyard-api/internal/auth/jwt_external.go`.
If the `aud` claim does not contain `enclii-api`, the token is rejected with a
`401 Unauthorized` response.

### 4. Configure API

Set environment variables in Kubernetes:
```yaml
env:
  - name: ENCLII_AUTH_MODE
    value: "oidc"
  - name: ENCLII_OIDC_ISSUER
    value: "https://auth.madfam.io"
  - name: ENCLII_OIDC_CLIENT_ID
    value: "switchyard"
  - name: ENCLII_OIDC_CLIENT_SECRET
    valueFrom:
      secretKeyRef:
        name: janua-credentials
        key: client-secret
  - name: ENCLII_OIDC_REDIRECT_URL
    value: "https://api.enclii.dev/v1/auth/callback"
```

### 4. Rebuild UI with OIDC Mode

```bash
docker build --no-cache \
  --build-arg NEXT_PUBLIC_API_URL=https://api.enclii.dev \
  --build-arg NEXT_PUBLIC_AUTH_MODE=oidc \
  --build-arg NEXT_PUBLIC_JANUA_URL=https://auth.madfam.io \
  -t switchyard-ui:v4-oidc .
```

## Authentication Flow

### Local Mode Flow
```
User → /login → Enter credentials → POST /v1/auth/login → JWT tokens → Dashboard
```

### OIDC Mode Flow
```
User → /login → Click "Sign in with Janua SSO"
     → Redirect to Janua → Authenticate
     → Callback to /v1/auth/callback → Exchange code
     → JWT tokens → Redirect to /auth/callback → Dashboard
```

## Route Protection

All routes except the following require authentication:
- `/login` - Login page
- `/register` - Registration page (local mode only)
- `/auth/callback` - OAuth callback
- `/api/auth/*` - Auth API routes

Protected routes use the `(protected)` route group with `AuthenticatedLayout`.

## User Roles

| Role | Permissions |
|------|-------------|
| `admin` | Full access: projects, services, deployments, users |
| `developer` | Create/deploy services, manage own projects |
| `viewer` | Read-only access |

Roles are enforced at the API level via `RequireRole` middleware.
