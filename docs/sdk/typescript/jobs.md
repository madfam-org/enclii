---
title: Jobs
description: Manage cron and one-off jobs with the Enclii TypeScript SDK
sidebar_position: 10
tags: [sdk, typescript, jobs, cron]
---

# Jobs

`enclii.jobs` (`JobsResource`, `packages/sdk-ts/src/resources/jobs.ts`) manages scheduled (cron) and one-off jobs. Jobs belong to a project (by **slug**) and run with a service's image unless `image` is set.

| Method | Signature | HTTP |
|--------|-----------|------|
| `listCron` | `listCron(projectSlug: string): Promise<Page<CronJob>>` | `GET /projects/{slug}/cron-jobs` |
| `getCron` | `getCron(jobId: string): Promise<CronJob>` | `GET /cron-jobs/{id}` |
| `createCron` | `createCron(projectSlug: string, input: CreateCronJobRequest): Promise<CronJob>` | `POST /projects/{slug}/cron-jobs` |
| `updateCron` | `updateCron(jobId: string, input: Partial<CreateCronJobRequest> & { suspended?: boolean }): Promise<CronJob>` | `PATCH /cron-jobs/{id}` |
| `deleteCron` | `deleteCron(jobId: string): Promise<void>` | `DELETE /cron-jobs/{id}` |
| `listCronRuns` | `listCronRuns(jobId: string): Promise<Page<CronJobRun>>` | `GET /cron-jobs/{id}/runs` |
| `listOneOff` | `listOneOff(projectSlug: string): Promise<Page<OneOffJob>>` | `GET /projects/{slug}/one-off-jobs` |
| `createOneOff` | `createOneOff(projectSlug: string, input: CreateOneOffJobRequest): Promise<OneOffJob>` | `POST /projects/{slug}/one-off-jobs` |

The API handlers are in `apps/switchyard-api/internal/api/timetable_handlers.go`. The create endpoints wrap the new job as `{ cron_job, message }` and `{ one_off_job, message }`; `createCron()` and `createOneOff()` return the job itself.

None of the list endpoints page, so `nextCursor` is always `null` and the list methods' `limit`/`cursor` options are deprecated and not sent. `listCron()` returns every cron job of the project. `listCronRuns()` and `listOneOff()` return only the **50 most recent** rows; older runs and jobs are not reachable through the API.

There are no `iter()` methods on `jobs`, and no methods to get a single one-off job or its logs (the API routes `GET /one-off-jobs/{id}` and `GET /one-off-jobs/{id}/logs` exist; call them with [`client.get()`](./index.md#low-level-requests)).

## Setup

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});
```

## Cron jobs

```typescript
const job = await enclii.jobs.createCron('my-project', {
  name: 'nightly-sync',
  schedule: '0 2 * * *',
  command: 'npm run sync',
  service_id: serviceId,
  concurrency: 'forbid',
});

// Pause it
await enclii.jobs.updateCron(job.id, { suspended: true });

// Inspect recent runs
const { data: runs } = await enclii.jobs.listCronRuns(job.id);
for (const r of runs) {
  console.log(r.started_at, r.status, r.exit_code);
}

// Remove it (the API route requires the admin role)
await enclii.jobs.deleteCron(job.id);
```

`CreateCronJobRequest`:

| Field | Type | Required |
|-------|------|----------|
| `name` | `string` | yes |
| `schedule` | `string` | yes (cron expression) |
| `command` | `string` | yes |
| `service_id` | `string` | yes |
| `image` | `string` | no |
| `timeout` | `number` | no |
| `retries` | `number` | no |
| `concurrency` | `'allow' \| 'forbid' \| 'replace'` | no |

## One-off jobs

```typescript
const oneOff = await enclii.jobs.createOneOff('my-project', {
  name: 'migrate-users-v2',
  command: 'npm run migrate',
  service_id: serviceId,
  run_at: '2026-10-01T03:00:00Z', // optional RFC 3339 time; omit to run now
});
console.log(oneOff.id, oneOff.status);

const { data } = await enclii.jobs.listOneOff('my-project'); // 50 most recent
for (const j of data) {
  if (j.status === 'failed') console.error(j.name, j.failure_reason ?? `exit ${j.exit_code}`);
}
```

`CreateOneOffJobRequest` has `name`, `command`, and `service_id` (required) and `image`, `timeout`, and `run_at` (optional).

## Types

```typescript
interface CronJob {
  id: UUID;
  project_id: UUID;
  service_id: UUID;
  name: string;
  schedule: string;
  command: string;
  image?: string;
  timeout: number;
  retries: number;
  suspended: boolean;
  concurrency: 'allow' | 'forbid' | 'replace';
  created_at: ISODateTime;
  updated_at: ISODateTime;
  last_run_at?: ISODateTime | null;
  next_run_at?: ISODateTime | null;
}

interface CronJobRun {
  id: UUID;
  cron_job_id: UUID;
  status: 'running' | 'completed' | 'failed';
  exit_code?: number | null;
  started_at: ISODateTime;
  ended_at?: ISODateTime | null;
  log_output?: string;
}

interface OneOffJob {
  id: UUID;
  project_id: UUID;
  service_id: UUID;
  name: string;
  command: string;
  image?: string;
  timeout: number;
  run_at?: ISODateTime | null;
  status: 'pending' | 'running' | 'completed' | 'failed';
  exit_code?: number | null;
  failure_reason?: string; // why it failed before producing a pod, e.g. an admission denial
  created_at: ISODateTime;
  started_at?: ISODateTime | null;
  ended_at?: ISODateTime | null;
}
```

## Related documentation

- [TypeScript SDK overview](./index.md)
- [CLI: `enclii jobs`](../../cli/commands/jobs.md)
