import type { EncliiClient } from '../client';
import type { CreateProjectRequest, Page, Project } from '../types';

export class ProjectsResource {
  constructor(private readonly client: EncliiClient) {}

  /** Fetch a single project by slug. */
  async get(slug: string): Promise<Project> {
    return this.client.get<Project>(`/projects/${encodeURIComponent(slug)}`);
  }

  /** List projects accessible to the caller. Cursor-paginated. */
  async list(
    options: { limit?: number; cursor?: string } = {},
  ): Promise<Page<Project>> {
    const resp = await this.client.get<{
      projects: Project[];
      next_cursor?: string | null;
    }>('/projects', options);
    return {
      data: resp.projects ?? [],
      nextCursor: resp.next_cursor ?? null,
    };
  }

  /**
   * Iterate over every project lazily. Fetches pages on demand; the entire
   * set never lives in memory at once.
   */
  iter(options: { pageSize?: number } = {}): AsyncIterable<Project> {
    return this.client.paginate<Project>('/projects', {
      itemsField: 'projects',
      pageSize: options.pageSize,
    });
  }

  async create(input: CreateProjectRequest): Promise<Project> {
    return this.client.post<Project>('/projects', input);
  }

  async delete(slug: string): Promise<void> {
    await this.client.del(`/projects/${encodeURIComponent(slug)}`);
  }
}
