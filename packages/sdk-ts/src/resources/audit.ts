import type { EncliiClient } from '../client';
import type { AuditEvent, AuditQueryOptions, Page } from '../types';

/**
 * Audit / activity event querying.
 *
 * Backed by `/activity` on the API side — every resource mutation (service
 * updates, deploys, rollbacks, secret rotations, webhook CRUD, ...) produces
 * an audit row. Useful for compliance reporting and debugging.
 */
export class AuditResource {
  constructor(private readonly client: EncliiClient) {}

  async list(options: AuditQueryOptions = {}): Promise<Page<AuditEvent>> {
    const resp = await this.client.get<{
      activities: AuditEvent[];
      next_cursor?: string | null;
    }>('/activity', {
      action: options.action,
      resource_type: options.resource_type,
      project_id: options.project_id,
      actor_id: options.actor_id,
      limit: options.limit,
      cursor: options.cursor,
    });
    return {
      data: resp.activities ?? [],
      nextCursor: resp.next_cursor ?? null,
    };
  }

  iter(
    options: Omit<AuditQueryOptions, 'cursor'> = {},
  ): AsyncIterable<AuditEvent> {
    return this.client.paginate<AuditEvent>('/activity', {
      itemsField: 'activities',
      query: {
        action: options.action,
        resource_type: options.resource_type,
        project_id: options.project_id,
        actor_id: options.actor_id,
      },
      pageSize: options.limit,
    });
  }

  /** List of action types available for filtering. */
  async actions(): Promise<string[]> {
    const resp = await this.client.get<{ actions: string[] }>(
      '/activity/actions',
    );
    return resp.actions ?? [];
  }

  /** List of resource types available for filtering. */
  async resourceTypes(): Promise<string[]> {
    const resp = await this.client.get<{ resource_types: string[] }>(
      '/activity/resource-types',
    );
    return resp.resource_types ?? [];
  }
}
