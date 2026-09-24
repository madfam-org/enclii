import type { EncliiClient } from '../client';
import type { Page, UnpagedListOptions } from '../types';
import type {
  CreateCronJobRequest,
  CreateOneOffJobRequest,
  CronJob,
  CronJobRun,
  OneOffJob,
} from '../types-ops';

/**
 * Timetable — cron and one-off scheduled jobs
 * (`apps/switchyard-api/internal/api/timetable_handlers.go`).
 *
 * No list endpoint here pages: `listCron()` returns every cron job, while
 * `listCronRuns()` and `listOneOff()` return only the 50 most recent rows.
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

  /** The 50 most recent runs of a cron job; older runs are not reachable. */
  async listCronRuns(
    jobId: string,
    _options: UnpagedListOptions = {},
  ): Promise<Page<CronJobRun>> {
    const resp = await this.client.get<{ runs: CronJobRun[]; total: number }>(
      `/cron-jobs/${encodeURIComponent(jobId)}/runs`,
    );
    return { data: resp.runs ?? [], nextCursor: null };
  }

  // ---------------------------------------------------------------------------
  // One-off
  // ---------------------------------------------------------------------------

  /** The 50 most recent one-off jobs of a project; older jobs are not reachable. */
  async listOneOff(
    projectSlug: string,
    _options: UnpagedListOptions = {},
  ): Promise<Page<OneOffJob>> {
    const resp = await this.client.get<{
      one_off_jobs: OneOffJob[];
      total: number;
    }>(`/projects/${encodeURIComponent(projectSlug)}/one-off-jobs`);
    return { data: resp.one_off_jobs ?? [], nextCursor: null };
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
