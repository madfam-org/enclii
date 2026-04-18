import { describe, expect, it } from 'vitest';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';

describe('JobsResource', () => {
  it('lists cron jobs for a project', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({
        cron_jobs: [
          {
            id: 'c1',
            name: 'nightly',
            schedule: '0 2 * * *',
            command: 'npm run sync',
          },
        ],
        next_cursor: null,
      }),
    );
    const client = newClient({ fetch });
    const page = await client.jobs.listCron('proj-1');
    expect(page.data).toHaveLength(1);
    expect(calls[0]!.url).toContain('/projects/proj-1/cron-jobs');
  });

  it('creates a cron job', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ id: 'c1' }, { status: 201 }),
    );
    const client = newClient({ fetch });
    await client.jobs.createCron('proj-1', {
      name: 'nightly',
      schedule: '0 2 * * *',
      command: 'npm run sync',
      service_id: 'svc-1',
    });
    const body = JSON.parse(calls[0]!.body!);
    expect(body.schedule).toBe('0 2 * * *');
  });

  it('creates a one-off job', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ id: 'o1' }, { status: 201 }),
    );
    const client = newClient({ fetch });
    await client.jobs.createOneOff('proj-1', {
      name: 'migrate',
      command: 'npm run migrate',
      service_id: 'svc-1',
    });
    expect(calls[0]!.url).toContain('/projects/proj-1/one-off-jobs');
  });
});
