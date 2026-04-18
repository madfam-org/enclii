import type { EncliiClient } from '../client';
import type { CreateServiceRequest, Page, Service } from '../types';

export class ServicesResource {
  constructor(private readonly client: EncliiClient) {}

  /** Fetch a single service by ID. */
  async get(serviceId: string): Promise<Service> {
    return this.client.get<Service>(
      `/services/${encodeURIComponent(serviceId)}`,
    );
  }

  /** List services for a project. Cursor-paginated. */
  async list(
    projectSlug: string,
    options: { limit?: number; cursor?: string } = {},
  ): Promise<Page<Service>> {
    const resp = await this.client.get<{
      services: Service[];
      next_cursor?: string | null;
    }>(`/projects/${encodeURIComponent(projectSlug)}/services`, options);
    return {
      data: resp.services ?? [],
      nextCursor: resp.next_cursor ?? null,
    };
  }

  iter(
    projectSlug: string,
    options: { pageSize?: number } = {},
  ): AsyncIterable<Service> {
    return this.client.paginate<Service>(
      `/projects/${encodeURIComponent(projectSlug)}/services`,
      { itemsField: 'services', pageSize: options.pageSize },
    );
  }

  async create(
    projectSlug: string,
    input: CreateServiceRequest,
  ): Promise<Service> {
    return this.client.post<Service>(
      `/projects/${encodeURIComponent(projectSlug)}/services`,
      input,
    );
  }

  async delete(serviceId: string): Promise<void> {
    await this.client.del(`/services/${encodeURIComponent(serviceId)}`);
  }

  /** Trigger a restart of all replicas for a service. */
  async restart(
    serviceId: string,
    options: { environment?: string } = {},
  ): Promise<void> {
    await this.client.post(
      `/services/${encodeURIComponent(serviceId)}/restart`,
      options,
    );
  }

  /** Scale a service to a specific replica count. */
  async scale(
    serviceId: string,
    replicas: number,
    options: { environment?: string } = {},
  ): Promise<void> {
    await this.client.post(
      `/services/${encodeURIComponent(serviceId)}/scale`,
      { replicas, ...options },
    );
  }
}
