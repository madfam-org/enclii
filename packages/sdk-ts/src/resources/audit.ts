import type { EncliiClient } from '../client';
import type { Page } from '../types';
import type { AuditEvent, AuditQueryOptions } from '../types-ops';
import {
  assertLimit,
  nextOffsetCursor,
  offsetFromCursor,
} from './offset-paging';

/** Server-side cap on `limit` for `GET /activity` (activity_handlers.go). */
const MAX_ACTIVITY_LIMIT = 100;

/**
 * Audit / activity event querying.
 *
 * Backed by `/activity` on the API side — every resource mutation (service
 * updates, deploys, rollbacks, secret rotations, webhook CRUD, ...) produces
 * an audit row. The endpoint pages with `limit`/`offset`; the SDK exposes that
 * as an opaque `cursor`/`nextCursor`.
 */
export class AuditResource {
  constructor(private readonly client: EncliiClient) {}

  async list(options: AuditQueryOptions = {}): Promise<Page<AuditEvent>> {
    assertLimit('audit.list', options.limit, MAX_ACTIVITY_LIMIT);
    const resp = await this.client.get<{
      activities: AuditEvent[] | null;
      count: number;
      limit: number;
      offset: number;
    }>('/activity', {
      action: options.action,
      resource_type: options.resource_type,
      project_id: options.project_id,
      actor_id: options.actor_id,
      limit: options.limit,
      offset: offsetFromCursor('audit.list', options.cursor),
    });
    const data = resp.activities ?? [];
    return { data, nextCursor: nextOffsetCursor(data.length, resp) };
  }

  /** Walk every matching event, one page (of `limit`, default 50) at a time. */
  async *iter(
    options: Omit<AuditQueryOptions, 'cursor'> = {},
  ): AsyncIterable<AuditEvent> {
    let cursor: string | undefined;
    do {
      const page = await this.list({ ...options, cursor });
      for (const event of page.data) yield event;
      cursor = page.nextCursor ?? undefined;
    } while (cursor !== undefined);
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
