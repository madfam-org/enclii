import type { EncliiClient } from '../client';
import type { Page, UnpagedListOptions } from '../types';
import type {
  BulkEnvVar,
  BulkSetEnvVarsResponse,
  EnvVar,
  SetEnvVarRequest,
} from '../types-ops';

/** Server-side cap on one bulk upsert (envvar_handlers.go BulkUpsertEnvVars). */
const MAX_BULK_VARIABLES = 100;

/**
 * Service environment variables and secrets
 * (`apps/switchyard-api/internal/api/envvar_handlers.go`).
 *
 * A variable is plaintext or a secret (`is_secret: true`). List and create
 * responses mask secret values as `••••••••`; `reveal()` returns the value and
 * the API records the reveal in its audit trail. Variables are addressed by
 * ID, not by key.
 */
export class SecretsResource {
  constructor(private readonly client: EncliiClient) {}

  /**
   * List a service's variables. `environment_id` narrows the list to one
   * environment; the endpoint returns every row in one response.
   */
  async list(
    serviceId: string,
    options: UnpagedListOptions & { environment_id?: string } = {},
  ): Promise<Page<EnvVar>> {
    const resp = await this.client.get<{
      environment_variables: EnvVar[] | null;
    }>(`/services/${encodeURIComponent(serviceId)}/env-vars`, {
      environment_id: options.environment_id,
    });
    return { data: resp.environment_variables ?? [], nextCursor: null };
  }

  /** Create a single variable. Set `is_secret: true` for secrets. */
  async set(serviceId: string, input: SetEnvVarRequest): Promise<EnvVar> {
    return this.client.post<EnvVar>(
      `/services/${encodeURIComponent(serviceId)}/env-vars`,
      input,
    );
  }

  /**
   * Create or update up to 100 variables by key in one call. `environment_id`
   * scopes the whole batch (omit for all environments). The API returns only
   * the number of variables written, not the variables.
   */
  async bulkSet(
    serviceId: string,
    vars: BulkEnvVar[],
    options: { environment_id?: string } = {},
  ): Promise<BulkSetEnvVarsResponse> {
    if (vars.length === 0 || vars.length > MAX_BULK_VARIABLES) {
      throw new Error(
        `secrets.bulkSet: expected 1 to ${MAX_BULK_VARIABLES} variables, got ${vars.length}`,
      );
    }
    return this.client.post<BulkSetEnvVarsResponse>(
      `/services/${encodeURIComponent(serviceId)}/env-vars/bulk`,
      {
        variables: vars,
        ...(options.environment_id
          ? { environment_id: options.environment_id }
          : {}),
      },
    );
  }

  /** Delete a variable by ID. */
  async delete(serviceId: string, varId: string): Promise<void> {
    await this.client.del(
      `/services/${encodeURIComponent(serviceId)}/env-vars/${encodeURIComponent(varId)}`,
    );
  }

  /**
   * Reveal the plaintext value of a secret. The call is logged for audit
   * and requires the developer role.
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
