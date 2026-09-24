import { describe, expect, it } from 'vitest';
import { goCronJob, goOneOffJob } from '../fixtures';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';

describe('JobsResource', () => {
  // Contract: ListCronJobs writes {"cron_jobs": [...], "total": n}.
  it('listCron() reads cron_jobs and sends no paging params', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ cron_jobs: [goCronJob('c1')], total: 1 }),
    );
    const client = newClient({ fetch });
    const page = await client.jobs.listCron('proj-1', { limit: 5 });
    expect(page).toEqual({ data: [goCronJob('c1')], nextCursor: null });
    expect(calls[0]!.url).toBe('https://api.enclii.test/v1/projects/proj-1/cron-jobs');
  });

  // Contract: CreateCronJob answers 201 {"cron_job": {...}, "message"}.
  it('createCron() posts the request and unwraps cron_job', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse(
        { cron_job: goCronJob('c1'), message: 'Cron job created successfully' },
        { status: 201 },
      ),
    );
    const client = newClient({ fetch });
    const job = await client.jobs.createCron('proj-1', {
      name: 'nightly',
      schedule: '0 2 * * *',
      command: 'npm run sync',
      service_id: 'svc-1',
      concurrency: 'forbid',
    });
    expect(job.id).toBe('c1');
    expect(job.schedule).toBe('0 2 * * *');
    expect(calls[0]!.method).toBe('POST');
    expect(new URL(calls[0]!.url).pathname).toBe('/v1/projects/proj-1/cron-jobs');
    expect(JSON.parse(calls[0]!.body!)).toEqual({
      name: 'nightly',
      schedule: '0 2 * * *',
      command: 'npm run sync',
      service_id: 'svc-1',
      concurrency: 'forbid',
    });
  });

  // Contract: ListCronJobRuns writes {"runs": [...], "total": n} (50 newest).
  it('listCronRuns() reads runs', async () => {
    const run = {
      id: 'r1',
      cron_job_id: 'c1',
      status: 'completed',
      exit_code: 0,
      started_at: '2026-09-20T02:00:00Z',
    };
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ runs: [run], total: 1 }),
    );
    const client = newClient({ fetch });
    const page = await client.jobs.listCronRuns('c1');
    expect(page.data).toEqual([run]);
    expect(calls[0]!.url).toBe('https://api.enclii.test/v1/cron-jobs/c1/runs');
  });

  // Contract: ListOneOffJobs writes {"one_off_jobs": [...], "total": n}.
  it('listOneOff() reads one_off_jobs', async () => {
    const failed = goOneOffJob('o2', {
      status: 'failed',
      failure_reason: 'admission webhook denied the pod',
    });
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ one_off_jobs: [goOneOffJob('o1'), failed], total: 2 }),
    );
    const client = newClient({ fetch });
    const page = await client.jobs.listOneOff('proj-1');
    expect(page.data.map((j) => j.id)).toEqual(['o1', 'o2']);
    expect(page.data[1]!.failure_reason).toMatch(/admission/);
    expect(page.nextCursor).toBeNull();
    expect(calls[0]!.method).toBe('GET');
    expect(calls[0]!.url).toBe('https://api.enclii.test/v1/projects/proj-1/one-off-jobs');
  });

  // Contract: CreateOneOffJob answers 201 {"one_off_job": {...}, "message"}.
  it('createOneOff() posts the request and unwraps one_off_job', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse(
        { one_off_job: goOneOffJob('o1'), message: 'One-off job created successfully' },
        { status: 201 },
      ),
    );
    const client = newClient({ fetch });
    const job = await client.jobs.createOneOff('proj-1', {
      name: 'migrate',
      command: 'npm run migrate',
      service_id: 'svc-1',
      run_at: '2026-10-01T03:00:00Z',
    });
    expect(job.id).toBe('o1');
    expect(job.status).toBe('pending');
    expect(calls[0]!.method).toBe('POST');
    expect(new URL(calls[0]!.url).pathname).toBe('/v1/projects/proj-1/one-off-jobs');
    expect(JSON.parse(calls[0]!.body!)).toEqual({
      name: 'migrate',
      command: 'npm run migrate',
      service_id: 'svc-1',
      run_at: '2026-10-01T03:00:00Z',
    });
  });
});
