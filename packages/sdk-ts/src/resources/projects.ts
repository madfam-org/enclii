import type { EncliiClient } from '../client';
import type {
  CreateProjectRequest,
  Page,
  Project,
  UnpagedIterOptions,
  UnpagedListOptions,
} from '../types';

export class ProjectsResource {
  constructor(private readonly client: EncliiClient) {}

  /** Fetch a single project by slug. */
  async get(slug: string): Promise<Project> {
    return this.client.get<Project>(`/projects/${encodeURIComponent(slug)}`);
  }

  /**
   * List every project visible to the caller. The endpoint returns all rows
   * in one response.
   */
  async list(_options: UnpagedListOptions = {}): Promise<Page<Project>> {
    const resp = await this.client.get<{ projects: Project[] | null }>(
      '/projects',
    );
    return { data: resp.projects ?? [], nextCursor: null };
  }

  /** Iterate every project (one request; see `list()`). */
  async *iter(_options: UnpagedIterOptions = {}): AsyncIterable<Project> {
    const page = await this.list();
    yield* page.data;
  }

  async create(input: CreateProjectRequest): Promise<Project> {
    return this.client.post<Project>('/projects', input);
  }

  async delete(slug: string): Promise<void> {
    await this.client.del(`/projects/${encodeURIComponent(slug)}`);
  }
}
