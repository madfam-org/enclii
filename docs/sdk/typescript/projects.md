---
title: Projects
description: Manage Enclii projects with the TypeScript SDK
sidebar_position: 3
tags: [sdk, typescript, projects]
---

# Projects

Manage Enclii projects using the TypeScript SDK.

## Overview

Projects are the top-level organizational unit in Enclii. Each project can contain multiple services and environments.

```typescript
import { EncliiClient } from '@enclii/sdk';

const enclii = new EncliiClient();

// Projects module
enclii.projects.list();
enclii.projects.get(id);
enclii.projects.create(data);
enclii.projects.update(id, data);
enclii.projects.delete(id);
```

## List Projects

```typescript
// List all projects
const projects = await enclii.projects.list();

console.log(`You have ${projects.length} projects:`);
for (const project of projects) {
  console.log(`- ${project.name} (${project.id})`);
}
```

### With Pagination

```typescript
// Paginated listing
const { data, pagination } = await enclii.projects.list({
  page: 1,
  limit: 20,
  sort: 'name',
  order: 'asc',
});

console.log(`Showing ${data.length} of ${pagination.total} projects`);
console.log(`Page ${pagination.page} of ${pagination.totalPages}`);
```

### With Filtering

```typescript
// Filter by name
const projects = await enclii.projects.list({
  search: 'api',  // Search in name and description
});

// Filter by status
const activeProjects = await enclii.projects.list({
  status: 'active',
});
```

## Get Project

```typescript
// Get by ID
const project = await enclii.projects.get('proj_abc123');

console.log(`Project: ${project.name}`);
console.log(`Created: ${project.createdAt}`);
console.log(`Services: ${project.serviceCount}`);
console.log(`Environments: ${project.environments.join(', ')}`);
```

### With Related Data

```typescript
// Include services in response
const project = await enclii.projects.get('proj_abc123', {
  include: ['services', 'team'],
});

console.log(`Services:`);
for (const service of project.services) {
  console.log(`  - ${service.name}: ${service.status}`);
}
```

## Create Project

```typescript
// Create a new project
const project = await enclii.projects.create({
  name: 'my-new-project',
  description: 'A sample project created via SDK',
});

console.log(`Created project: ${project.id}`);
```

### With Configuration

```typescript
const project = await enclii.projects.create({
  name: 'production-api',
  description: 'Production API services',

  // Default environments
  environments: ['development', 'staging', 'production'],

  // Team settings
  defaultTeam: 'team_abc123',

  // Resource defaults
  defaults: {
    region: 'eu-central',
    resourceLimits: {
      cpu: '500m',
      memory: '512Mi',
    },
  },

  // Labels for organization
  labels: {
    tier: 'production',
    team: 'backend',
  },
});
```

### From GitHub Repository

```typescript
// Import project from GitHub
const project = await enclii.projects.createFromGitHub({
  name: 'my-app',
  repository: 'madfam-org/my-app',

  // Auto-detect services from monorepo
  autoDetect: true,

  // Or specify service paths
  services: [
    { name: 'api', path: 'apps/api' },
    { name: 'web', path: 'apps/web' },
  ],
});
```

## Update Project

```typescript
// Update project details
const project = await enclii.projects.update('proj_abc123', {
  name: 'updated-name',
  description: 'Updated description',
});
```

### Update Settings

```typescript
// Update project settings
await enclii.projects.updateSettings('proj_abc123', {
  defaultEnvironment: 'staging',
  autoDeployEnabled: true,
  autoDeployBranch: 'main',

  notifications: {
    slack: '#deployments',
    email: ['team@example.com'],
  },
});
```

## Delete Project

```typescript
// Delete a project (requires confirmation)
await enclii.projects.delete('proj_abc123', {
  confirm: true,  // Required to prevent accidental deletion
});

// Force delete (removes all services)
await enclii.projects.delete('proj_abc123', {
  confirm: true,
  force: true,
});
```

## Project Environments

### List Environments

```typescript
const environments = await enclii.projects.listEnvironments('proj_abc123');

for (const env of environments) {
  console.log(`${env.name}: ${env.status}`);
}
```

### Create Environment

```typescript
const env = await enclii.projects.createEnvironment('proj_abc123', {
  name: 'staging',
  copyFrom: 'development',  // Clone from existing

  // Override variables
  variables: {
    API_URL: 'https://api.staging.example.com',
  },
});
```

### Delete Environment

```typescript
await enclii.projects.deleteEnvironment('proj_abc123', 'preview-123', {
  confirm: true,
});
```

## Project Members

### List Members

```typescript
const members = await enclii.projects.listMembers('proj_abc123');

for (const member of members) {
  console.log(`${member.email}: ${member.role}`);
}
```

### Add Member

```typescript
await enclii.projects.addMember('proj_abc123', {
  email: 'developer@example.com',
  role: 'developer',
});
```

### Update Member Role

```typescript
await enclii.projects.updateMember('proj_abc123', 'user_xyz', {
  role: 'admin',
});
```

### Remove Member

```typescript
await enclii.projects.removeMember('proj_abc123', 'user_xyz');
```

## Project Variables

### List Variables

```typescript
const variables = await enclii.projects.listVariables('proj_abc123');

for (const v of variables) {
  const value = v.isSecret ? '***' : v.value;
  console.log(`${v.key}=${value}`);
}
```

### Set Variable

```typescript
// Set a plain variable
await enclii.projects.setVariable('proj_abc123', {
  key: 'API_VERSION',
  value: 'v2',
});

// Set a secret (encrypted)
await enclii.projects.setVariable('proj_abc123', {
  key: 'DATABASE_URL',
  value: 'postgres://...',
  isSecret: true,
});
```

### Delete Variable

```typescript
await enclii.projects.deleteVariable('proj_abc123', 'DEPRECATED_KEY');
```

## Types

```typescript
interface Project {
  id: string;
  name: string;
  description?: string;
  status: 'active' | 'suspended' | 'deleted';
  environments: string[];
  serviceCount: number;
  labels: Record<string, string>;
  createdAt: string;
  updatedAt: string;
}

interface CreateProjectInput {
  name: string;
  description?: string;
  environments?: string[];
  defaultTeam?: string;
  defaults?: ProjectDefaults;
  labels?: Record<string, string>;
}

interface ProjectDefaults {
  region?: string;
  resourceLimits?: {
    cpu?: string;
    memory?: string;
  };
}
```

## Error Handling

```typescript
import { EncliiError, NotFoundError, ValidationError } from '@enclii/sdk';

try {
  const project = await enclii.projects.get('invalid-id');
} catch (error) {
  if (error instanceof NotFoundError) {
    console.log('Project not found');
  } else if (error instanceof ValidationError) {
    console.log(`Validation error: ${error.details}`);
  } else if (error instanceof EncliiError) {
    console.log(`API error: ${error.code}`);
  }
}
```

## Related Documentation

- **SDK Overview**: [TypeScript SDK](/sdk/typescript/)
- **Services**: [Service Management](./services)
- **Deployments**: [Deployments](./deployments)
- **API Reference**: [Projects API](/api-reference/#tag/projects)
