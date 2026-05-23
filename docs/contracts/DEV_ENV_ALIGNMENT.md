# Development environment alignment (UI, API, CLI)

Use these variables together for local development:

| Surface | Variable | Local default |
|---------|----------|---------------|
| Switchyard API | `ENCLII_PORT` | `4200` |
| Web UI | `NEXT_PUBLIC_API_URL` | `http://localhost:4200` |
| CLI | `ENCLII_API_ENDPOINT` | `http://localhost:4200` (override prod default) |
| Roundhouse → API | `ENCLII_ROUNDHOUSE_API_KEY` | shared secret; required in production |

## Log streaming

| Consumer | Endpoint | Backend |
|----------|----------|---------|
| Web UI tail | WebSocket `/v1/services/:id/logs/tail` (via `lib/ws-url.ts`) | Loki |
| CLI stream | WebSocket `/v1/services/:id/logs/stream` | Kubernetes |

## HTTP client conventions

| Surface | Client |
|---------|--------|
| Web UI | `lib/api.ts` — `apiGet`, `apiPost`, `apiFetchResponse`, `apiPublicGet` |
| CLI | `packages/cli/internal/cmd/apirequest.go` — `apiRequest`, `apiRequestResponse` |

## Authentication

- **Web:** OIDC via Janua (`NEXT_PUBLIC_AUTH_MODE=oidc`) or local bootstrap.
- **CLI:** PKCE login; must use the same OIDC client registered for CLI, not the web SPA client.
- **Workers:** `Authorization: Bearer $ENCLII_ROUNDHOUSE_API_KEY` on callbacks and internal reads.
