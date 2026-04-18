/**
 * Typed error hierarchy for @madfam/enclii-sdk.
 *
 * Consumers should `instanceof` these rather than string-matching `.message`.
 * The base `EncliiError` carries the request context (method, path, status)
 * so operators can trace failed calls without re-fetching logs.
 */

export interface EncliiErrorContext {
  method: string;
  path: string;
  status?: number;
  requestId?: string;
}

export class EncliiError extends Error {
  public readonly method: string;
  public readonly path: string;
  public readonly status?: number;
  public readonly requestId?: string;
  public readonly details?: unknown;

  constructor(message: string, ctx: EncliiErrorContext, details?: unknown) {
    super(message);
    this.name = 'EncliiError';
    this.method = ctx.method;
    this.path = ctx.path;
    this.status = ctx.status;
    this.requestId = ctx.requestId;
    this.details = details;
    // Restore prototype chain across the compile target boundary.
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

export class AuthenticationError extends EncliiError {
  constructor(message: string, ctx: EncliiErrorContext, details?: unknown) {
    super(message, ctx, details);
    this.name = 'AuthenticationError';
    Object.setPrototypeOf(this, AuthenticationError.prototype);
  }
}

export class AuthorizationError extends EncliiError {
  constructor(message: string, ctx: EncliiErrorContext, details?: unknown) {
    super(message, ctx, details);
    this.name = 'AuthorizationError';
    Object.setPrototypeOf(this, AuthorizationError.prototype);
  }
}

export class NotFoundError extends EncliiError {
  constructor(message: string, ctx: EncliiErrorContext, details?: unknown) {
    super(message, ctx, details);
    this.name = 'NotFoundError';
    Object.setPrototypeOf(this, NotFoundError.prototype);
  }
}

export class ValidationError extends EncliiError {
  constructor(message: string, ctx: EncliiErrorContext, details?: unknown) {
    super(message, ctx, details);
    this.name = 'ValidationError';
    Object.setPrototypeOf(this, ValidationError.prototype);
  }
}

export class ConflictError extends EncliiError {
  constructor(message: string, ctx: EncliiErrorContext, details?: unknown) {
    super(message, ctx, details);
    this.name = 'ConflictError';
    Object.setPrototypeOf(this, ConflictError.prototype);
  }
}

export class RateLimitError extends EncliiError {
  public readonly retryAfterSeconds?: number;
  constructor(
    message: string,
    ctx: EncliiErrorContext,
    retryAfterSeconds?: number,
    details?: unknown,
  ) {
    super(message, ctx, details);
    this.name = 'RateLimitError';
    this.retryAfterSeconds = retryAfterSeconds;
    Object.setPrototypeOf(this, RateLimitError.prototype);
  }
}

export class ServerError extends EncliiError {
  constructor(message: string, ctx: EncliiErrorContext, details?: unknown) {
    super(message, ctx, details);
    this.name = 'ServerError';
    Object.setPrototypeOf(this, ServerError.prototype);
  }
}

export class NetworkError extends EncliiError {
  constructor(message: string, ctx: EncliiErrorContext, cause?: unknown) {
    super(message, ctx, cause);
    this.name = 'NetworkError';
    Object.setPrototypeOf(this, NetworkError.prototype);
  }
}

/** Maps an HTTP status + body snippet to the right typed error. */
export function errorFromResponse(
  status: number,
  body: unknown,
  ctx: EncliiErrorContext,
  retryAfterSeconds?: number,
): EncliiError {
  const msg = extractMessage(body) ?? `HTTP ${status}`;
  const errCtx = { ...ctx, status };

  if (status === 401) return new AuthenticationError(msg, errCtx, body);
  if (status === 403) return new AuthorizationError(msg, errCtx, body);
  if (status === 404) return new NotFoundError(msg, errCtx, body);
  if (status === 409) return new ConflictError(msg, errCtx, body);
  if (status === 422 || status === 400)
    return new ValidationError(msg, errCtx, body);
  if (status === 429)
    return new RateLimitError(msg, errCtx, retryAfterSeconds, body);
  if (status >= 500) return new ServerError(msg, errCtx, body);

  return new EncliiError(msg, errCtx, body);
}

function extractMessage(body: unknown): string | undefined {
  if (body == null || typeof body !== 'object') {
    return typeof body === 'string' ? body : undefined;
  }
  const record = body as Record<string, unknown>;
  if (typeof record['error'] === 'string') return record['error'];
  if (typeof record['message'] === 'string')
    return record['message'] as string;
  return undefined;
}
