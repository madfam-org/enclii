/**
 * Core Enclii client.
 *
 * Thin wrapper around `fetch` that adds:
 *   - Bearer auth (static or async provider)
 *   - Exponential-backoff retry on 429/5xx
 *   - Cursor-pagination helpers
 *   - Typed error hierarchy
 *   - Resource namespacing (enclii.projects, enclii.deployments, ...)
 *
 * Target: both browser and Node ≥18. The log-stream subpath
 * (`@madfam/enclii-sdk/node`) is the only place that pulls in Node-only deps.
 */

import {
  AnonymousAuth,
  AuthStrategy,
  resolveAuthStrategy,
  TokenProvider,
} from './auth';
import {
  EncliiError,
  NetworkError,
  RateLimitError,
  ServerError,
  errorFromResponse,
} from './errors';

// Resource modules are attached lazily to keep the surface tree-shakeable.
import { ProjectsResource } from './resources/projects';
import { ServicesResource } from './resources/services';
import { DeploymentsResource } from './resources/deployments';
import { RollbackResource } from './resources/rollback';
import { CanaryResource } from './resources/canary';
import { LogsResource } from './resources/logs';
import { AuditResource } from './resources/audit';
import { WebhooksResource } from './resources/webhooks';
import { SecretsResource } from './resources/secrets';
import { JobsResource } from './resources/jobs';

// -----------------------------------------------------------------------------
// Configuration
// -----------------------------------------------------------------------------

export interface EncliiClientOptions {
  /**
   * Base URL including the `/v1` prefix, e.g.
   * `https://api.enclii.dev/v1`. The client does **not** append `/v1` for you —
   * callers can point at `http://localhost:4200/v1` for local dev or a tenant
   * subdomain for self-hosted.
   */
  baseUrl: string;

  /**
   * Bearer token. Accepts:
   *   - A string (static token).
   *   - An async function (token provider — refreshed on every request).
   *   - An `AuthStrategy` object.
   *   - null/undefined for anonymous access (health endpoints only).
   */
  token?: string | TokenProvider | AuthStrategy | null;

  /** Retry configuration; defaults to 3 attempts with exp-backoff on 429/5xx. */
  retry?: RetryOptions;

  /** Request timeout in ms; defaults to 30_000. */
  timeoutMs?: number;

  /** Extra headers appended to every request. */
  defaultHeaders?: Record<string, string>;

  /** Custom fetch implementation (defaults to globalThis.fetch). */
  fetch?: typeof fetch;

  /** User-Agent header; defaults to `@madfam/enclii-sdk/<version>`. */
  userAgent?: string;
}

export interface RetryOptions {
  /** Max attempts (inclusive of the first); defaults to 3. Set to 1 to disable. */
  maxAttempts?: number;
  /** Initial backoff in ms; defaults to 250. */
  initialDelayMs?: number;
  /** Backoff multiplier; defaults to 2. */
  backoffFactor?: number;
  /** Cap on any single delay in ms; defaults to 10_000. */
  maxDelayMs?: number;
}

export interface RequestOptions {
  method?: string;
  path: string;
  query?: Record<string, string | number | boolean | undefined | null>;
  body?: unknown;
  headers?: Record<string, string>;
  /** Skip retry for this request. */
  skipRetry?: boolean;
  /** Abort signal for cancellation. */
  signal?: AbortSignal;
}

const DEFAULT_RETRY: Required<RetryOptions> = {
  maxAttempts: 3,
  initialDelayMs: 250,
  backoffFactor: 2,
  maxDelayMs: 10_000,
};

const DEFAULT_USER_AGENT = '@madfam/enclii-sdk/0.1.0';

// -----------------------------------------------------------------------------
// Client
// -----------------------------------------------------------------------------

export class EncliiClient {
  public readonly baseUrl: string;
  public readonly timeoutMs: number;
  public readonly retry: Required<RetryOptions>;

  private readonly auth: AuthStrategy;
  private readonly fetchImpl: typeof fetch;
  private readonly defaultHeaders: Record<string, string>;
  private readonly userAgent: string;

  // Resource namespaces. These are lazy-assigned so consumers can tree-shake
  // modules they don't use (ESM builds only — CJS keeps them all).
  public readonly projects: ProjectsResource;
  public readonly services: ServicesResource;
  public readonly deployments: DeploymentsResource;
  public readonly rollback: RollbackResource;
  public readonly canary: CanaryResource;
  public readonly logs: LogsResource;
  public readonly audit: AuditResource;
  public readonly webhooks: WebhooksResource;
  public readonly secrets: SecretsResource;
  public readonly jobs: JobsResource;

  constructor(options: EncliiClientOptions) {
    if (!options.baseUrl) {
      throw new Error(
        '@madfam/enclii-sdk: baseUrl is required (e.g. https://api.enclii.dev/v1)',
      );
    }
    // Trim trailing slash so callers can pass either form.
    this.baseUrl = options.baseUrl.replace(/\/$/, '');
    this.timeoutMs = options.timeoutMs ?? 30_000;
    this.retry = { ...DEFAULT_RETRY, ...(options.retry ?? {}) };
    this.auth =
      options.token === undefined
        ? new AnonymousAuth()
        : resolveAuthStrategy(options.token ?? null);

    const providedFetch = options.fetch ?? globalThis.fetch;
    if (typeof providedFetch !== 'function') {
      throw new Error(
        '@madfam/enclii-sdk: no fetch implementation available. Pass `options.fetch` for older runtimes.',
      );
    }
    // Bind to globalThis so browsers don't trip on "Illegal invocation".
    this.fetchImpl = providedFetch.bind(globalThis);
    this.defaultHeaders = options.defaultHeaders ?? {};
    this.userAgent = options.userAgent ?? DEFAULT_USER_AGENT;

    this.projects = new ProjectsResource(this);
    this.services = new ServicesResource(this);
    this.deployments = new DeploymentsResource(this);
    this.rollback = new RollbackResource(this);
    this.canary = new CanaryResource(this);
    this.logs = new LogsResource(this);
    this.audit = new AuditResource(this);
    this.webhooks = new WebhooksResource(this);
    this.secrets = new SecretsResource(this);
    this.jobs = new JobsResource(this);
  }

  // ---------------------------------------------------------------------------
  // Low-level HTTP
  // ---------------------------------------------------------------------------

  /**
   * Execute a request. Most callers should use the resource namespaces
   * instead — this is exposed for advanced cases (new endpoints not yet
   * wrapped in a resource, debugging).
   */
  async request<T = unknown>(opts: RequestOptions): Promise<T> {
    const method = opts.method ?? 'GET';
    const url = this.buildUrl(opts.path, opts.query);
    const headers = await this.buildHeaders(opts.headers);

    const attempts = opts.skipRetry ? 1 : this.retry.maxAttempts;
    let lastErr: unknown;

    for (let attempt = 1; attempt <= attempts; attempt++) {
      try {
        const result = await this.doFetch<T>(method, url, headers, opts);
        return result;
      } catch (err) {
        lastErr = err;
        if (!this.shouldRetry(err, attempt, attempts)) throw err;
        const delay = this.backoffDelay(attempt, err);
        await sleep(delay);
      }
    }
    // Unreachable — the loop either returns or throws.
    throw lastErr;
  }

  private async doFetch<T>(
    method: string,
    url: string,
    headers: Record<string, string>,
    opts: RequestOptions,
  ): Promise<T> {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), this.timeoutMs);
    // Wire up the caller's abort signal without clobbering our timeout.
    if (opts.signal) {
      if (opts.signal.aborted) controller.abort();
      else opts.signal.addEventListener('abort', () => controller.abort());
    }

    let resp: Response;
    try {
      resp = await this.fetchImpl(url, {
        method,
        headers,
        body:
          opts.body === undefined || opts.body === null
            ? undefined
            : JSON.stringify(opts.body),
        signal: controller.signal,
      });
    } catch (err) {
      throw new NetworkError(
        `network error: ${(err as Error).message ?? String(err)}`,
        { method, path: opts.path },
        err,
      );
    } finally {
      clearTimeout(timeout);
    }

    const requestId = resp.headers.get('x-request-id') ?? undefined;
    const ctx = { method, path: opts.path, status: resp.status, requestId };

    if (!resp.ok) {
      const body = await safeJson(resp);
      const retryAfter = parseRetryAfter(resp.headers.get('retry-after'));
      throw errorFromResponse(resp.status, body, ctx, retryAfter);
    }

    // 204 No Content / empty body
    if (resp.status === 204 || resp.headers.get('content-length') === '0') {
      return undefined as T;
    }
    const contentType = resp.headers.get('content-type') ?? '';
    if (!contentType.includes('application/json')) {
      return (await resp.text()) as unknown as T;
    }
    return (await resp.json()) as T;
  }

  private buildUrl(
    path: string,
    query?: RequestOptions['query'],
  ): string {
    const base = path.startsWith('http')
      ? path
      : `${this.baseUrl}${path.startsWith('/') ? '' : '/'}${path}`;
    if (!query) return base;
    const params = new URLSearchParams();
    for (const [k, v] of Object.entries(query)) {
      if (v === undefined || v === null) continue;
      params.append(k, String(v));
    }
    const qs = params.toString();
    return qs ? `${base}${base.includes('?') ? '&' : '?'}${qs}` : base;
  }

  private async buildHeaders(
    extra?: Record<string, string>,
  ): Promise<Record<string, string>> {
    const headers: Record<string, string> = {
      'content-type': 'application/json',
      accept: 'application/json',
      'user-agent': this.userAgent,
      ...this.defaultHeaders,
      ...(extra ?? {}),
    };
    const token = await this.auth.getToken();
    if (token) headers['authorization'] = `Bearer ${token}`;
    return headers;
  }

  private shouldRetry(
    err: unknown,
    attempt: number,
    maxAttempts: number,
  ): boolean {
    if (attempt >= maxAttempts) return false;
    if (err instanceof NetworkError) return true;
    if (err instanceof ServerError) return true;
    if (err instanceof RateLimitError) return true;
    return false;
  }

  private backoffDelay(attempt: number, err: unknown): number {
    // Honor Retry-After when the server sent one.
    if (err instanceof RateLimitError && err.retryAfterSeconds != null) {
      return Math.min(err.retryAfterSeconds * 1000, this.retry.maxDelayMs);
    }
    const base =
      this.retry.initialDelayMs *
      Math.pow(this.retry.backoffFactor, attempt - 1);
    // Small jitter (±20%) to avoid thundering herds on retry storms.
    const jitter = base * (0.8 + Math.random() * 0.4);
    return Math.min(Math.round(jitter), this.retry.maxDelayMs);
  }

  // ---------------------------------------------------------------------------
  // Convenience verbs (used internally by resources)
  // ---------------------------------------------------------------------------

  get<T>(path: string, query?: RequestOptions['query']): Promise<T> {
    return this.request<T>({ method: 'GET', path, query });
  }
  post<T>(
    path: string,
    body?: unknown,
    query?: RequestOptions['query'],
  ): Promise<T> {
    return this.request<T>({ method: 'POST', path, body, query });
  }
  patch<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>({ method: 'PATCH', path, body });
  }
  put<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>({ method: 'PUT', path, body });
  }
  del<T = void>(path: string): Promise<T> {
    return this.request<T>({ method: 'DELETE', path });
  }

  // ---------------------------------------------------------------------------
  // Pagination
  // ---------------------------------------------------------------------------

  /**
   * Walk a cursor-paginated collection lazily. Re-fetches pages one at a time
   * so consumers never hold the entire dataset in memory.
   */
  async *paginate<T>(
    path: string,
    options: {
      query?: RequestOptions['query'];
      /** Name of the array field on each response; defaults to `data`. */
      itemsField?: string;
      /** Name of the cursor field; defaults to `next_cursor`. */
      cursorField?: string;
      /** Page size to request; passed through as `limit`. */
      pageSize?: number;
    } = {},
  ): AsyncIterable<T> {
    const itemsField = options.itemsField ?? 'data';
    const cursorField = options.cursorField ?? 'next_cursor';
    let cursor: string | undefined;

    while (true) {
      const query = {
        ...(options.query ?? {}),
        ...(cursor ? { cursor } : {}),
        ...(options.pageSize ? { limit: options.pageSize } : {}),
      };
      const page = await this.get<Record<string, unknown>>(path, query);
      const items = (page[itemsField] ?? []) as T[];
      for (const item of items) yield item;

      const next = page[cursorField];
      if (typeof next !== 'string' || next.length === 0) return;
      cursor = next;
    }
  }
}

// -----------------------------------------------------------------------------
// Internal helpers
// -----------------------------------------------------------------------------

async function safeJson(resp: Response): Promise<unknown> {
  try {
    const text = await resp.text();
    if (!text) return null;
    try {
      return JSON.parse(text);
    } catch {
      return text;
    }
  } catch {
    return null;
  }
}

function parseRetryAfter(header: string | null): number | undefined {
  if (!header) return undefined;
  const seconds = Number.parseInt(header, 10);
  if (!Number.isNaN(seconds)) return seconds;
  // HTTP-date form (rare from Enclii, but handle it).
  const date = Date.parse(header);
  if (!Number.isNaN(date)) {
    return Math.max(0, Math.round((date - Date.now()) / 1000));
  }
  return undefined;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// Re-export the typed error classes so consumers can `instanceof` against
// them without reaching into a subpath.
export {
  EncliiError,
  NetworkError,
  ServerError,
  RateLimitError,
} from './errors';
