import type { EncliiClient } from '../client';
import type {
  CreateCronJobRequest,
  CronJob,
  CronJobRun,
  OneOffJob,
  Page,
} from '../types';

/** Timetable — cron and one-off scheduled jobs. */
export class JobsResource {
  constructor(private readonly client: EncliiClient) {}

  // ---------------------------------------------------------------------------
  // Cron
  // ---------------------------------------------------------------------------

  async listCron(
    projectSlug: string,
    options: { limit?: number; cursor?: string } = {},
  ): Promise<Page<CronJob>> {
    const resp = await this.client.get<{
      cron_jobs: CronJob[];
      next_cursor?: string | null;
    }>(
      `/projects/${encodeURIComponent(projectSlug)}/cron-jobs`,
      options,
    );
    return {
      data: resp.cron_jobs ?? [],
      nextCursor: resp.next_cursor ?? null,
    };
  }

  async getCron(jobId: string): Promise<CronJob> {
    return this.client.get<CronJob>(
      `/cron-jobs/${encodeURIComponent(jobId)}`,
    );
  }

  async createCron(
    projectSlug: string,
    input: CreateCronJobRequest,
  ): Promise<CronJob> {
    return this.client.post<CronJob>(
      `/projects/${encodeURIComponent(projectSlug)}/cron-jobs`,
      input,
    );
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

  /** List historical runs of a cron job. */
  async listCronRuns(
    jobId: string,
    options: { limit?: number; cursor?: string } = {},
  ): Promise<Page<CronJobRun>> {
    const resp = await this.client.get<{
      runs: CronJobRun[];
      next_cursor?: string | null;
    }>(
      `/cron-jobs/${encodeURIComponent(jobId)}/runs`,
      options,
    );
    return {
      data: resp.runs ?? [],
      nextCursor: resp.next_cursor ?? null,
    };
  }

  // ---------------------------------------------------------------------------
  // One-off
  // ---------------------------------------------------------------------------

  async listOneOff(
    projectSlug: string,
    options: { limit?: number; cursor?: string } = {},
  ): Promise<Page<OneOffJob>> {
    const resp = await this.client.get<{
      jobs: OneOffJob[];
      next_cursor?: string | null;
    }>(
      `/projects/${encodeURIComponent(projectSlug)}/one-off-jobs`,
      options,
    );
    return {
      data: resp.jobs ?? [],
      nextCursor: resp.next_cursor ?? null,
    };
  }

  async createOneOff(
    projectSlug: string,
    input: {
      name: string;
      command: string;
      service_id: string;
      image?: string;
      timeout?: number;
      run_at?: string;
    },
  ): Promise<OneOffJob> {
    return this.client.post<OneOffJob>(
      `/projects/${encodeURIComponent(projectSlug)}/one-off-jobs`,
      input,
    );
  }
}
