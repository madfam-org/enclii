import type { EncliiClient } from '../client';
import type { OffsetPageOptions, Page, UnpagedListOptions } from '../types';
import type {
  CreateCronJobRequest,
  CreateOneOffJobRequest,
  CronJob,
  CronJobRun,
  OneOffJob,
} from '../types-ops';
import {
  assertLimit,
  nextOffsetCursor,
  offsetFromCursor,
} from './offset-paging';

/** Server-side cap on `limit` for the run and one-off job lists. */
const MAX_TIMETABLE_LIMIT = 100;

/**
 * Timetable — cron and one-off scheduled jobs
 * (`apps/switchyard-api/internal/api/timetable_handlers.go`).
 *
 * `listCron()` returns every cron job in one response. `listCronRuns()` and
 * `listOneOff()` page newest first with `limit` (1 to 100, default 50) and an
 * opaque `cursor`; pass the returned `nextCursor` back for the next page.
 * A server that predates paging returns the 50 newest rows with a null
 * `nextCursor`.
 */
export class JobsResource {
  constructor(private readonly client: EncliiClient) {}

  // ---------------------------------------------------------------------------
  // Cron
  // ---------------------------------------------------------------------------

  async listCron(
    projectSlug: string,
    _options: UnpagedListOptions = {},
  ): Promise<Page<CronJob>> {
    const resp = await this.client.get<{ cron_jobs: CronJob[]; total: number }>(
      `/projects/${encodeURIComponent(projectSlug)}/cron-jobs`,
    );
    return { data: resp.cron_jobs ?? [], nextCursor: null };
  }

  async getCron(jobId: string): Promise<CronJob> {
    return this.client.get<CronJob>(
      `/cron-jobs/${encodeURIComponent(jobId)}`,
    );
  }

  /** Create a cron job. The API wraps it as `{ cron_job, message }`. */
  async createCron(
    projectSlug: string,
    input: CreateCronJobRequest,
  ): Promise<CronJob> {
    const resp = await this.client.post<{ cron_job: CronJob; message: string }>(
      `/projects/${encodeURIComponent(projectSlug)}/cron-jobs`,
      input,
    );
    return resp.cron_job;
  }

  async updateCron(
    jobId: string,
    input: Partial<CreateCronJobRequest> & {
      suspended?: boolean;
    },
  ): Promise<CronJob> {
    return this.client.patch<CronJob>(
      `/cron-jobs/${encodeURIComponent(jobId)}`,
      input,
    );
  }

  async deleteCron(jobId: string): Promise<void> {
    await this.client.del(`/cron-jobs/${encodeURIComponent(jobId)}`);
  }

  /** One page of a cron job's runs, newest first. */
  async listCronRuns(
    jobId: string,
    options: OffsetPageOptions = {},
  ): Promise<Page<CronJobRun>> {
    assertLimit('jobs.listCronRuns', options.limit, MAX_TIMETABLE_LIMIT);
    const resp = await this.client.get<{
      runs: CronJobRun[] | null;
      total: number;
      limit?: number;
      offset?: number;
    }>(`/cron-jobs/${encodeURIComponent(jobId)}/runs`, {
      limit: options.limit,
      offset: offsetFromCursor('jobs.listCronRuns', options.cursor),
    });
    const data = resp.runs ?? [];
    return { data, nextCursor: nextOffsetCursor(data.length, resp) };
  }

  // ---------------------------------------------------------------------------
  // One-off
  // ---------------------------------------------------------------------------

  /** One page of a project's one-off jobs, newest first. */
  async listOneOff(
    projectSlug: string,
    options: OffsetPageOptions = {},
  ): Promise<Page<OneOffJob>> {
    assertLimit('jobs.listOneOff', options.limit, MAX_TIMETABLE_LIMIT);
    const resp = await this.client.get<{
      one_off_jobs: OneOffJob[] | null;
      total: number;
      limit?: number;
      offset?: number;
    }>(`/projects/${encodeURIComponent(projectSlug)}/one-off-jobs`, {
      limit: options.limit,
      offset: offsetFromCursor('jobs.listOneOff', options.cursor),
    });
    const data = resp.one_off_jobs ?? [];
    return { data, nextCursor: nextOffsetCursor(data.length, resp) };
  }

  /** Create a one-off job. The API wraps it as `{ one_off_job, message }`. */
  async createOneOff(
    projectSlug: string,
    input: CreateOneOffJobRequest,
  ): Promise<OneOffJob> {
    const resp = await this.client.post<{
      one_off_job: OneOffJob;
      message: string;
    }>(`/projects/${encodeURIComponent(projectSlug)}/one-off-jobs`, input);
    return resp.one_off_job;
  }
}
