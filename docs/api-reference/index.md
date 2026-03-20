---
title: API Reference
description: Enclii REST API reference documentation
sidebar_label: API Reference
tags: [api, reference]
---

# API Reference

The Enclii REST API powers the control plane for managing projects, services, deployments, and infrastructure.

## Base URL

```
https://api.enclii.dev
```

## Authentication

All API requests require authentication via Bearer token (JWT) or API key:

```bash
curl -H "Authorization: Bearer <token>" https://api.enclii.dev/v1/projects
```

See the [CLI Auth Setup](/guides/cli-auth-setup) guide for obtaining tokens.

## OpenAPI Specification

The full OpenAPI specification is maintained at [`docs/api/openapi.yaml`](https://github.com/madfam-io/enclii/blob/main/docs/api/openapi.yaml) in the repository.

## Key Endpoints

| Resource | Method | Path | Description |
|----------|--------|------|-------------|
| Health | GET | `/health` | Health check |
| Projects | GET | `/v1/projects` | List projects |
| Projects | POST | `/v1/projects` | Create project |
| Services | GET | `/v1/projects/:id/services` | List services |
| Deployments | POST | `/v1/projects/:id/services/:sid/deploy` | Deploy service |
| Builds | GET | `/v1/builds` | List builds |
| Logs | GET | `/v1/projects/:id/services/:sid/logs` | Stream logs |

## SDKs

- [TypeScript SDK](/sdk/typescript/) -- programmatic access from JavaScript/TypeScript
- [Go SDK](https://github.com/madfam-io/enclii/tree/main/packages/sdk-go) -- Go client library

## Related

- [Architecture Overview](/architecture/) -- system design and data flow
- [CLI Reference](/cli/) -- command-line interface documentation
- [Service Specification](/reference/service-spec) -- `enclii.yaml` format reference
