/**
 * Bearer-token management for @madfam/enclii-sdk.
 *
 * Two shapes are supported:
 *
 *   1. Static token — passed once at client-construction time. Simplest for
 *      CLI tools, server-side scripts, CI jobs.
 *
 *   2. Token provider — an async function the client calls before every
 *      request. Use this when tokens are short-lived (OIDC access tokens)
 *      or when the token must be refreshed transparently. The provider is
 *      responsible for its own caching — the client never caches the value.
 */

export type TokenProvider = () => Promise<string | null | undefined>;

export interface AuthStrategy {
  /** Resolve the current bearer token; return null/undefined to skip the header. */
  getToken(): Promise<string | null | undefined>;
}

/** Static-token strategy — the common 95% case. */
export class StaticTokenAuth implements AuthStrategy {
  constructor(private readonly token: string | null | undefined) {}
  async getToken(): Promise<string | null | undefined> {
    return this.token ?? null;
  }
}

/** Provider-backed strategy — lets callers refresh tokens on every request. */
export class TokenProviderAuth implements AuthStrategy {
  constructor(private readonly provider: TokenProvider) {}
  async getToken(): Promise<string | null | undefined> {
    return this.provider();
  }
}

/** Anonymous strategy — for endpoints that don't require auth. */
export class AnonymousAuth implements AuthStrategy {
  async getToken(): Promise<null> {
    return null;
  }
}

/**
 * Normalize whatever the caller passed in their EncliiClientOptions into a
 * concrete AuthStrategy. Accepts a string, a provider function, or an
 * already-built strategy.
 */
export function resolveAuthStrategy(
  input: string | TokenProvider | AuthStrategy | null | undefined,
): AuthStrategy {
  if (input == null) return new AnonymousAuth();
  if (typeof input === 'string') return new StaticTokenAuth(input);
  if (typeof input === 'function')
    return new TokenProviderAuth(input as TokenProvider);
  return input;
}
