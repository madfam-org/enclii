import type { EncliiClient } from '../client';
import type {
  CanaryRollout,
  CanaryRolloutState,
  CanaryStartRequest,
} from '../types';

const TERMINAL_STATES: ReadonlySet<CanaryRolloutState> = new Set([
  'succeeded',
  'auto_rolled_back',
  'manual_rolled_back',
  'failed',
]);

/** True once the canary has reached an end state (no further transitions). */
export function isTerminal(state: CanaryRolloutState): boolean {
  return TERMINAL_STATES.has(state);
}

/**
 * Canary rollouts (P2.7).
 *
 * The API splits traffic via replica-count proportion; there's no service mesh
 * involved. A 20% canary at 5 total replicas = 4 stable + 1 canary.
 *
 * Lifecycle:
 *   pending → running → validating → promoting → succeeded
 *                     ↘ auto_rolled_back
 *                     ↘ manual_rolled_back
 *       ↘ failed (from any non-terminal state on reconciler error)
 */
export class CanaryResource {
  constructor(private readonly client: EncliiClient) {}

  /**
   * Kick off a new canary. Rejects with 409 if a rollout is already in flight
   * for this service.
   */
  async start(
    serviceId: string,
    input: CanaryStartRequest,
  ): Promise<CanaryRollout> {
    // Convert the SDK's `validation_window_minutes` shorthand to the API's
    // minutes form transparently — the API accepts either.
    const payload: Record<string, unknown> = {
      digest: input.digest,
      percentage: input.percentage,
    };
    if (input.validation_window_minutes !== undefined) {
      payload['validation_window_minutes'] = input.validation_window_minutes;
    }
    if (input.smoke_endpoint !== undefined) {
      payload['smoke_endpoint'] = input.smoke_endpoint;
    }
    if (input.error_rate_threshold !== undefined) {
      payload['error_rate_threshold'] = input.error_rate_threshold;
    }
    if (input.environment_name !== undefined) {
      payload['environment_name'] = input.environment_name;
    }
    if (input.change_ticket_url !== undefined) {
      payload['change_ticket_url'] = input.change_ticket_url;
    }
    if (input.total_replicas !== undefined) {
      payload['total_replicas'] = input.total_replicas;
    }
    return this.client.post<CanaryRollout>(
      `/services/${encodeURIComponent(serviceId)}/canary`,
      payload,
    );
  }

  async get(serviceId: string, rolloutId: string): Promise<CanaryRollout> {
    return this.client.get<CanaryRollout>(
      `/services/${encodeURIComponent(serviceId)}/canary/${encodeURIComponent(rolloutId)}`,
    );
  }

  /** Short-circuit the validation window and promote the canary now. */
  async promote(serviceId: string, rolloutId: string): Promise<void> {
    await this.client.post(
      `/services/${encodeURIComponent(serviceId)}/canary/${encodeURIComponent(rolloutId)}/promote`,
      {},
    );
  }

  /** Abort the rollout and shift all traffic back to stable. */
  async rollback(
    serviceId: string,
    rolloutId: string,
    options: { reason?: string } = {},
  ): Promise<void> {
    await this.client.post(
      `/services/${encodeURIComponent(serviceId)}/canary/${encodeURIComponent(rolloutId)}/rollback`,
      options.reason ? { reason: options.reason } : {},
    );
  }

  /**
   * Poll until the rollout reaches a terminal state. Resolves with the final
   * `CanaryRollout`; rejects on timeout or caller-signalled abort.
   */
  async wait(
    serviceId: string,
    rolloutId: string,
    options: {
      intervalMs?: number;
      timeoutMs?: number;
      signal?: AbortSignal;
    } = {},
  ): Promise<CanaryRollout> {
    const interval = options.intervalMs ?? 5_000;
    const timeout = options.timeoutMs ?? 30 * 60_000;
    const start = Date.now();

    for (;;) {
      if (options.signal?.aborted) {
        throw new Error(`canary.wait aborted (rollout ${rolloutId})`);
      }
      if (Date.now() - start > timeout) {
        throw new Error(
          `canary.wait timed out after ${timeout}ms (rollout ${rolloutId})`,
        );
      }
      const r = await this.get(serviceId, rolloutId);
      if (isTerminal(r.state)) return r;
      await new Promise<void>((resolve) => setTimeout(resolve, interval));
    }
  }
}
