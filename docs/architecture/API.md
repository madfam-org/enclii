# Enclii API Documentation

## Base URL

```
Development: http://localhost:8080/v1
Staging: https://api.staging.enclii.dev/v1
Production: https://api.enclii.dev/v1
```

## Authentication

All API requests require authentication via JWT bearer token:

```http
Authorization: Bearer <jwt_token>
```

Obtain tokens via:
- OAuth2 flow: `/auth/login`
- API keys: `/auth/api-key`

## Rate Limiting

| Environment | Requests/Minute | Burst |
|------------|----------------|--------|
| Development | 1,000 | 100 |
| Staging | 5,000 | 500 |
| Production | 10,000 | 1,000 |

Rate limit headers:
- `X-RateLimit-Limit`: Request limit
- `X-RateLimit-Remaining`: Remaining requests
- `X-RateLimit-Reset`: Reset timestamp

## Error Responses

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Invalid input parameters",
    "details": {
      "field": "name",
      "reason": "Required field missing"
    },
    "request_id": "req_abc123"
  }
}
```

Error codes:
- `400`: Bad Request
- `401`: Unauthorized
- `403`: Forbidden
- `404`: Not Found
- `429`: Rate Limited
- `500`: Internal Server Error

---

## Endpoints

### Health Check

#### GET /health

Check API health status.

**Response:**
```json
{
  "status": "healthy",
  "service": "switchyard-api",
  "version": "1.0.0",
  "timestamp": "2024-01-01T00:00:00Z"
}
```

#### GET /health/ready

Readiness probe endpoint.

#### GET /health/live

Liveness probe endpoint.

---

### Projects

#### GET /projects

List all projects.

**Query Parameters:**
- `page` (int): Page number (default: 1)
- `limit` (int): Items per page (default: 20, max: 100)
- `sort` (string): Sort field (name, created_at)
- `order` (string): Sort order (asc, desc)

**Response:**
```json
{
  "projects": [
    {
      "id": "proj_abc123",
      "name": "My Project",
      "slug": "my-project",
      "description": "Project description",
      "created_at": "2024-01-01T00:00:00Z",
      "updated_at": "2024-01-01T00:00:00Z"
    }
  ],
  "pagination": {
    "page": 1,
    "limit": 20,
    "total": 100,
    "pages": 5
  }
}
```

#### POST /projects

Create a new project.

**Request:**
```json
{
  "name": "My Project",
  "slug": "my-project",
  "description": "Project description"
}
```

**Response:** `201 Created`
```json
{
  "id": "proj_abc123",
  "name": "My Project",
  "slug": "my-project",
  "description": "Project description",
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

#### GET /projects/`:slug`

Get project details.

**Response:**
```json
{
  "id": "proj_abc123",
  "name": "My Project",
  "slug": "my-project",
  "description": "Project description",
  "services": [
    {
      "id": "svc_123",
      "name": "api",
      "status": "running",
      "replicas": 3
    }
  ],
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

#### GET /project-processes/summary

Return a compact, project-card optimized process summary for one or more
projects. This is the dashboard batch endpoint; clients should use it instead
of calling per-service build-status endpoints from project cards.

**Query Parameters:**
- `project_ids` (string): Comma-separated project UUIDs (required)
- `limit_per_project` (int): Processes returned per project (default: 5, max: 20)
- `active_only` (bool): Include only active, failed, or blocked processes

**Response:**
```json
{
  "count": 1,
  "summaries": [
    {
      "project_id": "proj_abc123",
      "project_slug": "my-project",
      "active_count": 2,
      "failed_count": 0,
      "blocked_count": 1,
      "latest": {
        "id": "evt_123",
        "correlation_id": "svc_123:abc123:production",
        "kind": "rollout",
        "status": "blocked",
        "phase": "deploy_degraded",
        "service_id": "svc_123",
        "service_name": "api",
        "updated_at": "2026-05-06T12:00:00Z"
      },
      "processes": [],
      "services": []
    }
  ]
}
```

#### GET /project-processes/stream

Server-sent event stream for one or more project-card process summaries.

**Query Parameters:**
- `project_ids` (string): Comma-separated project UUIDs (required)
- `limit_per_project` (int): Processes returned per project (default: 5, max: 20)
- `active_only` (bool): Defaults to active/failed/blocked only; pass `false` for all

Clients should prefer authenticated `fetch()` streaming with an
`Authorization` header. Avoid query-string bearer tokens for project-card
streams because URLs can leak through logs, browser history, and observability
tooling.

**Events:**
- `summary`: JSON payload with the same shape as `/project-processes/summary`
- `error`: Recoverable stream refresh error
- heartbeat comments every ~25 seconds

#### GET /projects/`:slug`/processes

Return the process timeline for a single project.

**Query Parameters:**
- `limit` (int): Processes returned (default: 50, max: 100)
- `active_only` (bool): Include only active, failed, or blocked processes

#### GET /projects/`:slug`/processes/stream

Server-sent event stream for a single project's process summary. Emits the
same `summary` and `error` events as `/project-processes/stream`.

#### PUT /projects/`:slug`

Update project.

**Request:**
```json
{
  "name": "Updated Project",
  "description": "New description"
}
```

#### DELETE /projects/`:slug`

Delete project.

**Response:** `204 No Content`

---

### Services

#### GET /services

List all services.

**Query Parameters:**
- `project_id` (string): Filter by project
- `status` (string): Filter by status (running, stopped, failed)

**Response:**
```json
{
  "services": [
    {
      "id": "svc_123",
      "name": "api",
      "project_id": "proj_abc123",
      "status": "running",
      "replicas": 3,
      "image": "ghcr.io/org/api:v1.0.0",
      "created_at": "2024-01-01T00:00:00Z",
      "updated_at": "2024-01-01T00:00:00Z"
    }
  ]
}
```

#### POST /services

Create a new service.

**Request:**
```json
{
  "name": "api",
  "project_id": "proj_abc123",
  "config": {
    "replicas": 3,
    "cpu": "500m",
    "memory": "512Mi",
    "env": {
      "NODE_ENV": "production"
    },
    "ports": [
      {
        "port": 8080,
        "protocol": "TCP"
      }
    ]
  }
}
```

**Response:** `201 Created`

#### GET /services/`:id`

Get service details.

**Response:**
```json
{
  "id": "svc_123",
  "name": "api",
  "project_id": "proj_abc123",
  "status": "running",
  "replicas": 3,
  "image": "ghcr.io/org/api:v1.0.0",
  "config": {
    "cpu": "500m",
    "memory": "512Mi",
    "env": {
      "NODE_ENV": "production"
    }
  },
  "endpoints": [
    {
      "url": "https://api.my-project.enclii.dev",
      "type": "public"
    }
  ],
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

#### PUT /services/`:id`

Update service configuration.

#### DELETE /services/`:id`

Delete service.

---

### Builds

#### POST /services/`:id`/build

Trigger a new build.

**Request:**
```json
{
  "git_sha": "abc123def456",
  "branch": "main",
  "dockerfile": "Dockerfile",
  "build_args": {
    "VERSION": "1.0.0"
  }
}
```

**Response:** `201 Created`
```json
{
  "id": "build_123",
  "service_id": "svc_123",
  "status": "building",
  "git_sha": "abc123def456",
  "started_at": "2024-01-01T00:00:00Z"
}
```

#### GET /builds/`:id`

Get build status.

**Response:**
```json
{
  "id": "build_123",
  "service_id": "svc_123",
  "status": "success",
  "git_sha": "abc123def456",
  "image": "ghcr.io/org/api:v1.0.0",
  "logs": "Build output...",
  "started_at": "2024-01-01T00:00:00Z",
  "completed_at": "2024-01-01T00:05:00Z",
  "duration_seconds": 300
}
```

#### GET /builds/`:id`/logs

Stream build logs.

**Response:** Server-sent events stream
```
data: {"timestamp": "2024-01-01T00:00:00Z", "message": "Building image..."}
data: {"timestamp": "2024-01-01T00:00:01Z", "message": "Pushing to registry..."}
```

---

### Deployments

#### POST /services/`:id`/deploy

Deploy a service.

**Request:**
```json
{
  "release_id": "rel_123",
  "environment": {
    "NODE_ENV": "production"
  },
  "replicas": 3,
  "strategy": "rolling"
}
```

**Response:** `201 Created`
```json
{
  "id": "deploy_123",
  "service_id": "svc_123",
  "release_id": "rel_123",
  "status": "pending",
  "created_at": "2024-01-01T00:00:00Z"
}
```

#### GET /deployments/`:id`

Get deployment status.

**Response:**
```json
{
  "id": "deploy_123",
  "service_id": "svc_123",
  "release_id": "rel_123",
  "status": "running",
  "replicas": {
    "desired": 3,
    "current": 3,
    "ready": 3,
    "updated": 3
  },
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:05:00Z"
}
```

#### POST /deployments/`:id`/rollback

Rollback deployment.

**Response:** `202 Accepted`

---

### Logs

#### GET /services/`:id`/logs

Retrieve service logs.

**Query Parameters:**
- `lines` (int): Number of lines (default: 100, max: 1000)
- `since` (string): Time filter (e.g., "1h", "30m")
- `follow` (bool): Stream logs in real-time
- `container` (string): Specific container name

**Response:**
```json
{
  "logs": [
    {
      "timestamp": "2024-01-01T00:00:00Z",
      "level": "info",
      "message": "Server started",
      "container": "api",
      "pod": "api-abc123"
    }
  ]
}
```

---

### Metrics

#### GET /metrics

Prometheus metrics endpoint.

**Response:** Prometheus text format
```
# HELP http_requests_total Total HTTP requests
# TYPE http_requests_total counter
http_requests_total{method="GET",status="200"} 1234
```

#### GET /services/`:id`/metrics

Service-specific metrics.

**Response:**
```json
{
  "cpu": {
    "usage": "250m",
    "limit": "500m",
    "percentage": 50
  },
  "memory": {
    "usage": "256Mi",
    "limit": "512Mi",
    "percentage": 50
  },
  "network": {
    "rx_bytes": 1234567,
    "tx_bytes": 7654321
  },
  "requests": {
    "rate": 100,
    "errors": 2,
    "latency_p95": 250
  }
}
```

---

### Secrets

#### GET /projects/`:slug`/secrets

List project secrets.

**Response:**
```json
{
  "secrets": [
    {
      "key": "DATABASE_URL",
      "created_at": "2024-01-01T00:00:00Z",
      "updated_at": "2024-01-01T00:00:00Z"
    }
  ]
}
```

#### POST /projects/`:slug`/secrets

Create or update secret.

**Request:**
```json
{
  "key": "API_KEY",
  "value": "secret_value",
  "service": "api"
}
```

**Response:** `201 Created`

#### DELETE /projects/`:slug`/secrets/`:key`

Delete secret.

**Response:** `204 No Content`

---

### Authentication

#### POST /auth/login

OAuth2 login flow.

**Request:**
```json
{
  "code": "oauth_code",
  "redirect_uri": "http://localhost:3000/callback"
}
```

**Response:**
```json
{
  "access_token": "jwt_token",
  "refresh_token": "refresh_token",
  "expires_in": 86400,
  "user": {
    "id": "user_123",
    "email": "user@example.com",
    "name": "User Name",
    "role": "developer"
  }
}
```

#### POST /auth/refresh

Refresh access token.

**Request:**
```json
{
  "refresh_token": "refresh_token"
}
```

**Response:**
```json
{
  "access_token": "new_jwt_token",
  "expires_in": 86400
}
```

#### POST /auth/logout

Logout and revoke tokens.

**Request:**
```json
{
  "refresh_token": "refresh_token"
}
```

**Response:** `204 No Content`

#### POST /auth/api-key

Generate API key.

**Request:**
```json
{
  "name": "CI/CD Token",
  "expires_in": 2592000
}
```

**Response:**
```json
{
  "id": "key_123",
  "key": "enclii_live_abc123...",
  "name": "CI/CD Token",
  "created_at": "2024-01-01T00:00:00Z",
  "expires_at": "2024-02-01T00:00:00Z"
}
```

---

## Webhooks

### Event Types

- `build.started`
- `build.completed`
- `build.failed`
- `deployment.started`
- `deployment.completed`
- `deployment.failed`
- `service.scaled`
- `service.crashed`

### Webhook Payload

```json
{
  "id": "evt_123",
  "type": "deployment.completed",
  "timestamp": "2024-01-01T00:00:00Z",
  "data": {
    "deployment_id": "deploy_123",
    "service_id": "svc_123",
    "status": "success"
  }
}
```

### Webhook Security

Webhooks include HMAC signature:
```http
X-Enclii-Signature: sha256=abc123...
```

Verify signature:
```javascript
const signature = crypto
  .createHmac('sha256', webhookSecret)
  .update(rawBody)
  .digest('hex');
```

---

## SDK Examples

### JavaScript/TypeScript

```typescript
import { EncliiClient } from '@enclii/sdk';

const client = new EncliiClient({
  apiKey: process.env.ENCLII_API_KEY,
  baseUrl: 'https://api.enclii.dev'
});

// List projects
const projects = await client.projects.list();

// Deploy service
const deployment = await client.services.deploy('svc_123', {
  releaseId: 'rel_123',
  replicas: 3
});

// Stream logs
const logStream = await client.services.logs('svc_123', {
  follow: true
});

logStream.on('data', (log) => {
  console.log(log.message);
});
```

### Go

```go
package main

import (
    "github.com/madfam/enclii/sdk-go"
)

func main() {
    client := enclii.NewClient(
        enclii.WithAPIKey(os.Getenv("ENCLII_API_KEY")),
    )
    
    // List projects
    projects, err := client.Projects.List(ctx)
    
    // Deploy service
    deployment, err := client.Services.Deploy(ctx, "svc_123", &enclii.DeployRequest{
        ReleaseID: "rel_123",
        Replicas:  3,
    })
    
    // Stream logs
    logs, err := client.Services.Logs(ctx, "svc_123", &enclii.LogOptions{
        Follow: true,
    })
}
```

### Python

```python
from enclii import Client

client = Client(api_key=os.environ['ENCLII_API_KEY'])

# List projects
projects = client.projects.list()

# Deploy service
deployment = client.services.deploy('svc_123', 
    release_id='rel_123',
    replicas=3
)

# Stream logs
for log in client.services.logs('svc_123', follow=True):
    print(log['message'])
```

---

## API Versioning

The API uses URL-based versioning:
- Current: `/v1`
- Beta: `/v2-beta`

Deprecation policy:
- 6 months notice before deprecation
- 12 months support after new version
- Migration guides provided

---

## Support

- Documentation: https://docs.enclii.dev
- Status Page: https://status.enclii.dev
- Support: support@enclii.dev
