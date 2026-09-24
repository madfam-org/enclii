---
title: Projects
description: Manage Enclii projects with the TypeScript SDK
sidebar_position: 3
tags: [sdk, typescript, projects]
---

# Projects

`enclii.projects` (`ProjectsResource`, `packages/sdk-ts/src/resources/projects.ts`) covers the project collection. Projects are addressed by **slug**, not by ID.

| Method | Signature | HTTP |
|--------|-----------|------|
| `get` | `get(slug: string): Promise<Project>` | `GET /projects/{slug}` |
| `list` | `list(): Promise<Page<Project>>` | `GET /projects` |
| `iter` | `iter(): AsyncIterable<Project>` | `GET /projects` |
| `create` | `create(input: CreateProjectRequest): Promise<Project>` | `POST /projects` |
| `delete` | `delete(slug: string): Promise<void>` | `DELETE /projects/{slug}` |

There is no `update`, `updateSettings`, `createFromGitHub`, environment, member, or project-variable method. Use the [`enclii` CLI](../../cli/commands/projects.md) or the HTTP API (for example through [`client.request()`](./index.md#low-level-requests)) for those operations.

## Setup

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});
```

## Get a project

```typescript
const project = await enclii.projects.get('my-project');
console.log(project.id, project.name, project.ci_runner_mode);
```

Throws `NotFoundError` if the slug does not exist or is not visible to the caller.

## List projects

```typescript
const { data } = await enclii.projects.list();
for (const p of data) {
  console.log(`${p.slug}\t${p.name}`);
}

for await (const p of enclii.projects.iter()) {
  console.log(p.slug);
}
```

`GET /projects` returns every project visible to the caller in one response (`{ projects }`), so `nextCursor` is `null` and `iter()` makes one request. The deprecated `limit`/`cursor`/`pageSize` options are not sent. See [Pagination](./index.md#pagination).

## Create a project

```typescript
const project = await enclii.projects.create({
  name: 'My Project',
  slug: 'my-project',
});
```

`CreateProjectRequest`:

| Field | Type | Required |
|-------|------|----------|
| `name` | `string` | yes |
| `slug` | `string` | yes |
| `description` | `string` | no |
| `ci_runner_mode` | `'github' \| 'self-hosted'` | no; deprecated |

The create handler reads only `name`, `slug`, and `description`. `ci_runner_mode` is deprecated because the handler ignores it; set the runner mode afterwards with `PUT /projects/{slug}/ci-runner-config` (for example through [`client.put()`](./index.md#low-level-requests)). The API route requires the admin role.

## Delete a project

```typescript
await enclii.projects.delete('old-project');
```

Resolves to `undefined`. The API route requires the admin role; a caller without it gets `AuthorizationError`.

## Types

```typescript
type CIRunnerMode = 'github' | 'self-hosted';

interface Project {
  id: UUID;
  name: string;
  slug: string;
  ci_runner_mode: CIRunnerMode;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}
```

`UUID` and `ISODateTime` are both `string` aliases. `Page<T>` is `{ data: T[]; nextCursor: string | null }`.

## Error handling

```typescript
import { NotFoundError, ConflictError, ValidationError } from '@madfam/enclii-sdk';

try {
  await enclii.projects.create({ name: 'API', slug: 'api' });
} catch (err) {
  if (err instanceof ConflictError) {
    console.error('Slug already taken');
  } else if (err instanceof ValidationError) {
    console.error('Invalid input:', err.message, err.details);
  } else {
    throw err;
  }
}
```

## Related documentation

- [TypeScript SDK overview](./index.md)
- [Services](./services.md)
- [CLI: `enclii projects`](../../cli/commands/projects.md)
