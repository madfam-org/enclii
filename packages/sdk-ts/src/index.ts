/**
 * @madfam/enclii-sdk — TypeScript SDK for the Enclii DevOps platform.
 *
 * ```ts
 * import { EncliiClient } from '@madfam/enclii-sdk';
 *
 * const enclii = new EncliiClient({
 *   baseUrl: 'https://api.enclii.dev/v1',
 *   token: process.env.ENCLII_TOKEN,
 * });
 *
 * const deploy = await enclii.deployments.get('svc_123', 'v42');
 * for await (const entry of enclii.logs.tail('svc_123', { level: 'error' })) {
 *   console.log(entry.timestamp, entry.message);
 * }
 * ```
 */

export { EncliiClient } from './client';
export type {
  EncliiClientOptions,
  RequestOptions,
  RetryOptions,
} from './client';

export type {
  AuthStrategy,
  TokenProvider,
} from './auth';
export {
  AnonymousAuth,
  StaticTokenAuth,
  TokenProviderAuth,
  resolveAuthStrategy,
} from './auth';

export {
  AuthenticationError,
  AuthorizationError,
  ConflictError,
  EncliiError,
  NetworkError,
  NotFoundError,
  RateLimitError,
  ServerError,
  ValidationError,
} from './errors';
export type { EncliiErrorContext } from './errors';

export { parseVersionLabel } from './resources/deployments';
export { isTerminal as isCanaryTerminal } from './resources/canary';
export {
  DEFAULT_SIGNATURE_TOLERANCE_SECONDS,
  verifyWebhookSignature,
} from './resources/webhooks';
export type { VerifyWebhookSignatureOptions } from './resources/webhooks';

export * from './types';
