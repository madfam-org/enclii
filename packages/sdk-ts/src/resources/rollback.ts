import type { EncliiClient } from '../client';
import type {
  InstantRollbackRequest,
  InstantRollbackResponse,
  RollbackRequest,
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
 *   - `manifest(deploymentId, {to_release})` — manifest-commit rollback. Writes
 *     a new image tag; ArgoCD does a rolling update (2-3 min). Used when you
 *     want the rollback durably captured in git.
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

  /** Manifest-commit rollback — slow path, ArgoCD reconciles. */
  async manifest(
    deploymentId: string,
    input: RollbackRequest = {},
  ): Promise<void> {
    await this.client.post(
      `/deployments/${encodeURIComponent(deploymentId)}/rollback`,
      input,
    );
  }
}
