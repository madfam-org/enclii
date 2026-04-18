import type { EncliiClient } from '../client';
import type { EnvVar, Page, SetEnvVarRequest } from '../types';

/**
 * Service env-vars and secrets (via RFC 0005 bridge).
 *
 * The Enclii API exposes a bridge to the RFC 0005 Vault for read-back-disabled
 * secret values. Calls here either:
 *   - Create/update an env-var (plaintext or secret marker),
 *   - List env-vars (values elided unless caller has reveal permission),
 *   - Reveal a single secret value (logged for audit).
 *
 * Writing raw secret values through this bridge is expected to be replaced by
 * direct Selva-Vault tooling once RFC 0005 Sprint 3 lands; the API surface
 * will stay the same for consumers.
 */
export class SecretsResource {
  constructor(private readonly client: EncliiClient) {}

  /** List env-vars for a service. Values are elided for secrets. */
  async list(
    serviceId: string,
    options: { limit?: number; cursor?: string } = {},
  ): Promise<Page<EnvVar>> {
    const resp = await this.client.get<{
      env_vars: EnvVar[];
      next_cursor?: string | null;
    }>(
      `/services/${encodeURIComponent(serviceId)}/env-vars`,
      options,
    );
    return {
      data: resp.env_vars ?? [],
      nextCursor: resp.next_cursor ?? null,
    };
  }

  /** Create or update a single env-var. Set `is_secret: true` for secrets. */
  async set(serviceId: string, input: SetEnvVarRequest): Promise<EnvVar> {
    return this.client.post<EnvVar>(
      `/services/${encodeURIComponent(serviceId)}/env-vars`,
      input,
    );
  }

  /** Bulk set — idempotent upsert. */
  async bulkSet(
    serviceId: string,
    vars: SetEnvVarRequest[],
  ): Promise<EnvVar[]> {
    const resp = await this.client.post<{ env_vars: EnvVar[] }>(
      `/services/${encodeURIComponent(serviceId)}/env-vars/bulk`,
      { env_vars: vars },
    );
    return resp.env_vars ?? [];
  }

  /** Delete an env-var by ID. */
  async delete(serviceId: string, varId: string): Promise<void> {
    await this.client.del(
      `/services/${encodeURIComponent(serviceId)}/env-vars/${encodeURIComponent(varId)}`,
    );
  }

  /**
   * Reveal the plaintext value of a secret. The call is logged for audit
   * and may require elevated permissions.
   */
  async reveal(
    serviceId: string,
    varId: string,
  ): Promise<{ key: string; value: string }> {
    return this.client.post<{ key: string; value: string }>(
      `/services/${encodeURIComponent(serviceId)}/env-vars/${encodeURIComponent(varId)}/reveal`,
      {},
    );
  }
}
