import type { EncliiClient } from '../client';
import type {
  DeployRequest,
  Deployment,
  LatestDeploymentResponse,
  Page,
  Release,
  UnpagedIterOptions,
  UnpagedListOptions,
} from '../types';

/**
 * Parses a Heroku-style v-label ("v42") into its integer component. Returns
 * null if the input is not a v-label. Mirrors `ParseVersionLabel` in the Go
 * SDK so formatting stays consistent across tooling.
 */
export function parseVersionLabel(label: string): number | null {
  const trimmed = label.trim();
  if (trimmed.length < 2) return null;
  const prefix = trimmed[0];
  if (prefix !== 'v' && prefix !== 'V') return null;
  const n = Number.parseInt(trimmed.slice(1), 10);
  if (Number.isNaN(n) || n <= 0) return null;
  return n;
}

export class DeploymentsResource {
  constructor(private readonly client: EncliiClient) {}

  /**
   * Fetch a deployment. Accepts either a UUID or a Heroku-style v-label
   * (e.g. "v42"). For v-labels, the serviceId argument is required — it's
   * what disambiguates v42 across services.
   */
  async get(
    serviceIdOrDeploymentId: string,
    vLabelOrUndefined?: string,
  ): Promise<Deployment> {
    // Two-arg form: get(serviceId, "v42")
    if (vLabelOrUndefined !== undefined) {
      const n = parseVersionLabel(vLabelOrUndefined);
      if (n === null) {
        throw new Error(
          `invalid v-label: ${vLabelOrUndefined} (expected vN where N is a positive integer)`,
        );
      }
      return this.client.get<Deployment>(
        `/services/${encodeURIComponent(serviceIdOrDeploymentId)}/versions/${n}`,
      );
    }
    // One-arg form: get(deploymentId)
    return this.client.get<Deployment>(
      `/deployments/${encodeURIComponent(serviceIdOrDeploymentId)}`,
    );
  }

  /** Resolve a deployment by integer version number (v42 → 42). */
  async getByVersion(
    serviceId: string,
    versionNumber: number,
  ): Promise<Deployment> {
    return this.client.get<Deployment>(
      `/services/${encodeURIComponent(serviceId)}/versions/${versionNumber}`,
    );
  }

  /**
   * List every deployment of a service, newest release first. The endpoint
   * returns all rows in one response.
   */
  async list(
    serviceId: string,
    _options: UnpagedListOptions = {},
  ): Promise<Page<Deployment>> {
    const resp = await this.client.get<{
      service_id: string;
      deployments: Deployment[] | null;
      count: number;
    }>(`/services/${encodeURIComponent(serviceId)}/deployments`);
    return { data: resp.deployments ?? [], nextCursor: null };
  }

  /** Iterate every deployment of a service (one request; see `list()`). */
  async *iter(
    serviceId: string,
    _options: UnpagedIterOptions = {},
  ): AsyncIterable<Deployment> {
    const page = await this.list(serviceId);
    yield* page.data;
  }

  /**
   * Most recent deployment for a service. The API wraps it as
   * `{ deployment, release? }`; this returns the deployment.
   */
  async latest(serviceId: string): Promise<Deployment> {
    const resp = await this.client.get<LatestDeploymentResponse>(
      `/services/${encodeURIComponent(serviceId)}/deployments/latest`,
    );
    return resp.deployment;
  }

  /** Trigger a deployment (build must already be ready). */
  async deploy(
    serviceId: string,
    input: DeployRequest,
  ): Promise<Deployment> {
    return this.client.post<Deployment>(
      `/services/${encodeURIComponent(serviceId)}/deploy`,
      input,
    );
  }

  /** Trigger a build (returns the Release, which transitions to `ready`). */
  async build(serviceId: string, gitSha: string): Promise<Release> {
    return this.client.post<Release>(
      `/services/${encodeURIComponent(serviceId)}/build`,
      { git_sha: gitSha },
    );
  }

  /** List every release (built image) of a service in one response. */
  async listReleases(
    serviceId: string,
    _options: UnpagedListOptions = {},
  ): Promise<Page<Release>> {
    const resp = await this.client.get<{ releases: Release[] | null }>(
      `/services/${encodeURIComponent(serviceId)}/releases`,
    );
    return { data: resp.releases ?? [], nextCursor: null };
  }

  /**
   * Poll a deployment until it reaches a terminal state (running or failed).
   * Rejects on timeout or if the caller signals an abort.
   */
  async wait(
    deploymentId: string,
    options: {
      /** Poll interval in ms (default 3_000). */
      intervalMs?: number;
      /** Overall timeout in ms (default 600_000 = 10 minutes). */
      timeoutMs?: number;
      signal?: AbortSignal;
    } = {},
  ): Promise<Deployment> {
    const interval = options.intervalMs ?? 3_000;
    const timeout = options.timeoutMs ?? 600_000;
    const start = Date.now();

    for (;;) {
      if (options.signal?.aborted) {
        throw new Error(
          `deployments.wait aborted by caller (deployment ${deploymentId})`,
        );
      }
      if (Date.now() - start > timeout) {
        throw new Error(
          `deployments.wait timed out after ${timeout}ms (deployment ${deploymentId})`,
        );
      }
      const d = await this.get(deploymentId);
      if (
        d.status === 'running' ||
        d.status === 'failed' ||
        d.status === 'rolled_back'
      ) {
        return d;
      }
      await new Promise<void>((resolve) => setTimeout(resolve, interval));
    }
  }
}
