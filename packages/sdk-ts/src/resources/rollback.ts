import type { EncliiClient } from '../client';
import type {
  InstantRollbackRequest,
  InstantRollbackResponse,
  ManifestRollbackResponse,
} from '../types';

/**
 * Rollback operations.
 *
 * Two variants are supported, mirroring the CLI:
 *
 *   - `instant(serviceId, {target_deployment_id})` — P0.5 selector-flip
 *     rollback. Traffic shifts in <30s when the target ReplicaSet is still
 *     running, <90s when it needs to scale back up.
 *
 *   - `manifest(deploymentId)` — rolls the deployment's service back to its
 *     most recent other `running` deployment and marks this one `failed`.
 *     The API takes no target: use `instant()` to pick one.
 */
export class RollbackResource {
  constructor(private readonly client: EncliiClient) {}

  /** P0.5 instant rollback — service-selector flip (<30s to <90s). */
  async instant(
    serviceId: string,
    input: InstantRollbackRequest,
  ): Promise<InstantRollbackResponse> {
    return this.client.post<InstantRollbackResponse>(
      `/services/${encodeURIComponent(serviceId)}/rollback`,
      input,
    );
  }

  /**
   * Roll `deploymentId` back to the service's previous `running` deployment.
   * `POST /deployments/{id}/rollback` reads no request body, so the target
   * cannot be chosen; a second argument throws instead of being ignored.
   */
  async manifest(
    deploymentId: string,
    ...unsupported: never[]
  ): Promise<ManifestRollbackResponse> {
    if (unsupported.length > 0) {
      throw new Error(
        'rollback.manifest: the API does not accept a rollback target; ' +
          'use rollback.instant(serviceId, { target_deployment_id }) to choose one',
      );
    }
    return this.client.post<ManifestRollbackResponse>(
      `/deployments/${encodeURIComponent(deploymentId)}/rollback`,
    );
  }
}
