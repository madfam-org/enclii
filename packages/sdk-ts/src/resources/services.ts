import type { EncliiClient } from '../client';
import type {
  CreateServiceRequest,
  Page,
  Service,
  UnpagedIterOptions,
  UnpagedListOptions,
} from '../types';

/**
 * Options of `restart()`/`scale()`. The API reads `env` (default
 * `production`) only as a label for its log, audit record, response and the
 * `service.scaled` webhook; the workload it acts on is chosen from the
 * service's project, not from this value (infra_handlers.go).
 */
export interface ServiceOperationOptions {
  /** Sent as `env`. */
  environment?: string;
}

export class ServicesResource {
  constructor(private readonly client: EncliiClient) {}

  /** Fetch a single service by ID. */
  async get(serviceId: string): Promise<Service> {
    return this.client.get<Service>(
      `/services/${encodeURIComponent(serviceId)}`,
    );
  }

  /** List every service of a project. The endpoint returns all rows at once. */
  async list(
    projectSlug: string,
    _options: UnpagedListOptions = {},
  ): Promise<Page<Service>> {
    const resp = await this.client.get<{ services: Service[] | null }>(
      `/projects/${encodeURIComponent(projectSlug)}/services`,
    );
    return { data: resp.services ?? [], nextCursor: null };
  }

  /** Iterate every service of a project (one request; see `list()`). */
  async *iter(
    projectSlug: string,
    _options: UnpagedIterOptions = {},
  ): AsyncIterable<Service> {
    const page = await this.list(projectSlug);
    yield* page.data;
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

  /** Rolling restart of the service's workload. Requires the admin role. */
  async restart(
    serviceId: string,
    options: ServiceOperationOptions & { reason?: string } = {},
  ): Promise<void> {
    await this.client.post(
      `/services/${encodeURIComponent(serviceId)}/restart`,
      { env: options.environment, reason: options.reason },
    );
  }

  /**
   * Scale the service's workload to 1 to 10 replicas. Requires the admin role.
   * The API rejects `0` (its `replicas` field is `binding:"required"`, which
   * treats zero as missing) and anything above 10.
   */
  async scale(
    serviceId: string,
    replicas: number,
    options: ServiceOperationOptions = {},
  ): Promise<void> {
    await this.client.post(
      `/services/${encodeURIComponent(serviceId)}/scale`,
      { replicas, env: options.environment },
    );
  }
}
