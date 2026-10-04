/**
 * API utility for making authenticated requests to the Switchyard API
 *
 * SECURITY: Implements authentication with JWT tokens and CSRF protection.
 * For production deployment with OAuth 2.0 / OIDC, see:
 * - contexts/AuthContext.tsx
 * - SECURITY_AUDIT_COMPREHENSIVE_2025.md
 */

import { API_BASE_URL, AUTH_MODE, JANUA_BASE_URL, OAUTH_CLIENT_ID } from '@/lib/constants';
import { getStoredTokens, setStoredTokens, clearStorage, TOKEN_STORAGE_KEY } from '@/lib/auth-session';

// CSRF token cache
let csrfToken: string | null = null;

// Sticky flag: if /v1/csrf returns 404 once we stop calling it for the rest
// of the session. CSRF is needed for write operations, but a missing endpoint
// means the deployed control plane doesn't support it — retrying every write
// just spams the console with 404s without producing a token. The user can
// reload the tab to retry after a deploy.
//
// Truthfulness audit (2026-05-04): the endpoint /v1/csrf IS implemented in
// switchyard-api/internal/api/csrf_handler.go — this guard exists for the
// case where a stale UI is served against a backend whose CSRF route hasn't
// rolled out yet, or vice versa. It avoids polluting the dashboard console
// with 404s that obscure real signal.
let csrfEndpointAvailable: boolean | null = null;

// One rotation shared by provider timers and requests in this tab.
let refreshPromise: Promise<boolean> | null = null;

async function attemptTokenRefresh(): Promise<boolean> {
  if (typeof window === 'undefined') return false;
  if (refreshPromise) return refreshPromise;
  const snapshot = localStorage.getItem(TOKEN_STORAGE_KEY);
  const tokens = getStoredTokens();
  if (!tokens || typeof tokens.refreshToken !== 'string' || !tokens.refreshToken) return false;

  const rotate = async () => {
    // Another tab may have completed rotation while this tab awaited the lock.
    if (localStorage.getItem(TOKEN_STORAGE_KEY) !== snapshot) {
      const current = getStoredTokens();
      return !!current && current.expiresAt > Date.now();
    }
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 15_000);
    try {
      const oidc = AUTH_MODE === 'oidc';
      const response = await fetch(oidc ? `${JANUA_BASE_URL}/api/v1/oauth/token` : `${API_BASE_URL}/v1/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': oidc ? 'application/x-www-form-urlencoded' : 'application/json' },
        ...(oidc ? {} : { credentials: 'include' as const }),
        signal: controller.signal,
        body: oidc ? new URLSearchParams({
          grant_type: 'refresh_token', client_id: OAUTH_CLIENT_ID, refresh_token: tokens.refreshToken!,
        }).toString() : JSON.stringify({ refresh_token: tokens.refreshToken }),
      });
      // A logout or another sign-in while awaiting the issuer wins over this response.
      if (localStorage.getItem(TOKEN_STORAGE_KEY) !== snapshot) return false;
      if (!response.ok) {
        const error = await response.json().catch(() => ({}));
        if (response.status === 400 && (error.error === 'invalid_grant' ||
            (typeof error.detail === 'string' && error.detail.startsWith('invalid_grant:'))) && tokens.expiresAt <= Date.now()) {
          if (localStorage.getItem(TOKEN_STORAGE_KEY) === snapshot) clearStorage();
        }
        return false;
      }
      const data = await response.json();
      const expiresAt = oidc
        ? (typeof data.expires_in === 'number' && Number.isFinite(data.expires_in) && data.expires_in > 0
          ? Date.now() + data.expires_in * 1000 : NaN)
        : new Date(data.expires_at).getTime();
      if (typeof data.access_token !== 'string' || !data.access_token ||
          !Number.isFinite(expiresAt) || expiresAt <= Date.now() ||
          (data.refresh_token !== undefined && (typeof data.refresh_token !== 'string' || !data.refresh_token))) return false;
      if (localStorage.getItem(TOKEN_STORAGE_KEY) !== snapshot) return false;
      setStoredTokens({ ...tokens, accessToken: data.access_token,
        refreshToken: data.refresh_token ?? tokens.refreshToken, expiresAt });
      return true;
    } catch {
      // Network failure does not invalidate a still-valid access token.
      return false;
    } finally { clearTimeout(timeout); }
  };
  refreshPromise = Promise.resolve(typeof navigator !== 'undefined' && navigator.locks
    ? navigator.locks.request('enclii-token-refresh', rotate)
    : rotate());
  try { return await refreshPromise; }
  finally { refreshPromise = null; }
}

/** A late 401 from an old request can use an already-rotated token. */
async function refreshForRequest(accessToken: string | undefined): Promise<boolean> {
  const current = getStoredTokens();
  if (current && current.accessToken !== accessToken && current.expiresAt > Date.now()) return true;
  return attemptTokenRefresh();
}

/**
 * Get authentication headers for API requests
 *
 * Retrieves JWT token from localStorage (set by AuthContext)
 * Includes CSRF token for write operations
 */
/** Plain-object auth headers for fetch/WebSocket APIs that cannot use HeadersInit. */
export function getAuthHeadersRecord(includeCSRF: boolean = false): Record<string, string> {
  const headers = getAuthHeaders(includeCSRF);
  if (headers instanceof Headers) {
    return Object.fromEntries(headers.entries());
  }
  if (Array.isArray(headers)) {
    return Object.fromEntries(headers);
  }
  return headers as Record<string, string>;
}

export function getAuthHeaders(includeCSRF: boolean = false): HeadersInit {
  const headers: HeadersInit = {
    "Content-Type": "application/json",
  };

  // Get JWT token from localStorage - matches AuthContext storage key
  if (typeof window !== "undefined") {
    const storedTokens = localStorage.getItem("enclii_tokens");
    if (storedTokens) {
      try {
        const tokens = JSON.parse(storedTokens);
        if (tokens.accessToken) {
          headers["Authorization"] = `Bearer ${tokens.accessToken}`;
        }
        // Include IDP token for Janua API calls (e.g., GitHub integration status)
        if (tokens.idpToken) {
          headers["X-IDP-Token"] = tokens.idpToken;
        }
      } catch {
        // Invalid JSON, ignore
      }
    }

    // Development fallback
    if (!headers["Authorization"]) {
      const devToken = process.env.NEXT_PUBLIC_API_TOKEN;
      if (devToken) {
        headers["Authorization"] = `Bearer ${devToken}`;
      }
    }
  }

  // Add CSRF token for write operations
  if (includeCSRF && csrfToken) {
    headers["X-CSRF-Token"] = csrfToken;
  }

  return headers;
}

/**
 * Fetch and cache CSRF token
 *
 * Behaviour on the unhappy paths:
 *   - 404: backend doesn't expose /v1/csrf. We mark the endpoint as
 *     unavailable for the rest of the session so a write loop doesn't
 *     re-fire and re-log on every attempt. The write itself proceeds
 *     without a CSRF header — the backend's CSRF middleware will reject
 *     it cleanly with its own 403 if validation is enforced, which is a
 *     real, surface-able error for the operator.
 *   - Network error: log once via console.warn (not error — operators
 *     misread console.error as "the page is broken").
 *   - 5xx: same as network error — no retry, log once.
 */
async function fetchCSRFToken(): Promise<void> {
  if (csrfEndpointAvailable === false) {
    return; // already learned the endpoint isn't there
  }
  try {
    const response = await fetch(`${API_BASE_URL}/v1/csrf`, {
      credentials: "include", // Include cookies
    });

    if (response.ok) {
      csrfEndpointAvailable = true;
      const token = response.headers.get("X-CSRF-Token");
      if (token) {
        csrfToken = token;
      }
      return;
    }

    if (response.status === 404) {
      // We're guaranteed to be the first 404 in this session — the
      // early-return at the top of this function bails before we
      // reach `fetch()` once `csrfEndpointAvailable === false`. So
      // exactly one warn fires per page load, never more.
      console.warn(
        "CSRF endpoint /v1/csrf returned 404; continuing without CSRF token. Verify switchyard-api is up to date.",
      );
      csrfEndpointAvailable = false;
      return;
    }

    // Other non-200 — keep the endpoint flag null so a later page load
    // can retry, but don't log the body (avoids leaking error details).
    console.warn(
      `CSRF endpoint /v1/csrf returned ${response.status}; proceeding without token.`,
    );
  } catch (error) {
    // Network-level failure — the dashboard's other API calls would also
    // be failing, so this is unlikely to be the visible signal. Warn (not
    // error) so it doesn't masquerade as a UI bug.
    console.warn("CSRF token fetch failed:", error);
  }
}

// Default request timeout in ms. Cloudflare's edge timeout is ~100s; the
// browser and UI must give up earlier so a slow endpoint surfaces as a
// real error state instead of an indefinite spinner.
//
// 35s = 25s server budget (healthHandlerBudget in observability_handlers.go)
// + 10s headroom for p99 network jitter, TLS handshake, and gateway hops.
// Previously 30s, which sat exactly on the server budget — any jitter
// converted a bounded server-side partial response into a client-side
// fetch abort, and the dashboard rendered "Loading..." instead of the
// X-Enclii-Partial-Response payload the server actually sent.
const DEFAULT_REQUEST_TIMEOUT_MS = 35_000;

/**
 * Make an authenticated API request with CSRF protection
 *
 * @param endpoint - API endpoint path (e.g., '/v1/projects')
 * @param options - Fetch options (method, body, etc.). Supports
 *                  AbortSignal via options.signal; the helper composes
 *                  it with its own DEFAULT_REQUEST_TIMEOUT_MS budget.
 * @returns Promise with the response
 */
export async function apiRequest<T = unknown>(
  endpoint: string,
  options: RequestInit = {},
): Promise<T> {
  const url = `${API_BASE_URL}${endpoint}`;
  const method = options.method || "GET";
  const isWriteOperation = ["POST", "PUT", "DELETE", "PATCH"].includes(
    method.toUpperCase(),
  );

  // Fetch CSRF token for write operations if not cached and the endpoint
  // hasn't already been marked unavailable for this session. The
  // endpoint-availability sticky flag means a 404 on the first write
  // attempt does not spawn N+1 404s if the user fires multiple writes
  // before page reload.
  if (
    isWriteOperation &&
    !csrfToken &&
    csrfEndpointAvailable !== false
  ) {
    await fetchCSRFToken();
  }

  const requestAccessToken = getStoredTokens()?.accessToken;
  const headers: HeadersInit = {
    ...getAuthHeaders(isWriteOperation),
    ...options.headers,
  };

  // Wire up a timeout via AbortController. If the caller passed their own
  // signal we compose the two so cancellation from either source aborts
  // the underlying fetch — important for React strict-mode cleanups and
  // route changes during a slow request.
  const timeoutController = new AbortController();
  const timeoutId = setTimeout(() => timeoutController.abort(), DEFAULT_REQUEST_TIMEOUT_MS);
  const externalSignal = options.signal;
  const onExternalAbort = () => timeoutController.abort();
  if (externalSignal) {
    if (externalSignal.aborted) {
      timeoutController.abort();
    } else {
      externalSignal.addEventListener("abort", onExternalAbort, { once: true });
    }
  }

  try {
    const response = await fetch(url, {
      ...options,
      headers,
      credentials: "include", // Include cookies for CSRF
      signal: timeoutController.signal,
    });

    // Handle authentication errors with retry
    if (response.status === 401) {
      // Attempt to refresh the token before giving up
      const refreshed = await refreshForRequest(requestAccessToken);
      if (refreshed) {
        // Retry the request with the new token
        const retryHeaders: HeadersInit = {
          ...options.headers,
          ...getAuthHeaders(isWriteOperation),
        };
        const retryResponse = await fetch(url, {
          ...options,
          headers: retryHeaders,
          credentials: "include",
          // Reuse the same timeout controller so the retry inherits the
          // request budget instead of doubling it.
          signal: timeoutController.signal,
        });

        if (retryResponse.ok) {
          return await retryResponse.json();
        }

        // If retry also fails with 401, fall through to clear tokens
        if (retryResponse.status !== 401) {
          // Handle other errors from retry
          const error = await retryResponse.json().catch(() => ({}));
          throw new Error(
            error.message ||
              `API request failed: ${retryResponse.status} ${retryResponse.statusText}`,
          );
        }
      }

      // Token refresh failed or retry still returned 401
      // Check if the stored token is actually expired before triggering logout.
      // A 401 with a non-expired token is transient (e.g., race condition during
      // auth callback); only escalate when the token is genuinely expired or missing.
      let tokenActuallyExpired = true;
      if (typeof window !== "undefined") {
        try {
          const stored = localStorage.getItem("enclii_tokens");
          if (stored) {
            const t = JSON.parse(stored);
            if (t.expiresAt && t.expiresAt > Date.now()) {
              tokenActuallyExpired = false;
            }
          }
        } catch {
          // parse error → treat as expired
        }
      }

      if (tokenActuallyExpired) {
        if (AUTH_MODE !== "oidc") {
          // Local auth: clear tokens so user is prompted to log in
          if (typeof window !== "undefined") {
            localStorage.removeItem("enclii_tokens");
            localStorage.removeItem("enclii_user");
          }
        }
        // OIDC preserves the refresh credential on transient issuer failures.
        // The provider removes authenticated UI state when access expires.
        throw new Error("Authentication required. Please log in again.");
      }

      // Token not expired — transient 401, don't trigger logout cascade
      throw new Error("Request unauthorized. Retrying may resolve this.");
    }

    if (response.status === 403) {
      throw new Error(
        "Access denied. You do not have permission to perform this action.",
      );
    }

    if (!response.ok) {
      const error = await response.json().catch(() => ({}));
      throw new Error(
        error.message ||
          `API request failed: ${response.status} ${response.statusText}`,
      );
    }

    return await response.json();
  } catch (error) {
    // Surface timeouts (DOMException name='AbortError' from our own
    // controller) as a friendlier error message so consumer components
    // can render a clear "took too long" state instead of "Failed to
    // fetch", which historically read as "service down" to operators.
    if (error instanceof DOMException && error.name === "AbortError") {
      // External cancellation (route change, strict-mode unmount) —
      // re-throw the same kind so callers can ignore as needed.
      if (externalSignal?.aborted) throw error;
      const timeoutError = new Error(
        `Request to ${endpoint} timed out after ${DEFAULT_REQUEST_TIMEOUT_MS / 1000}s`,
      );
      console.error(`API request timed out for ${endpoint}`);
      throw timeoutError;
    }
    console.error(`API request failed for ${endpoint}:`, error);
    throw error;
  } finally {
    clearTimeout(timeoutId);
    if (externalSignal) {
      externalSignal.removeEventListener("abort", onExternalAbort);
    }
  }
}

/**
 * GET request helper
 */
export async function apiGet<T = unknown>(endpoint: string): Promise<T> {
  return apiRequest<T>(endpoint, { method: "GET" });
}

/**
 * POST request helper
 */
export async function apiPost<T = unknown>(
  endpoint: string,
  data: unknown,
): Promise<T> {
  return apiRequest<T>(endpoint, {
    method: "POST",
    body: JSON.stringify(data),
  });
}

/**
 * PUT request helper
 */
export async function apiPut<T = unknown>(endpoint: string, data: unknown): Promise<T> {
  return apiRequest<T>(endpoint, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

/**
 * DELETE request helper
 */
export async function apiDelete<T = unknown>(endpoint: string): Promise<T> {
  return apiRequest<T>(endpoint, { method: "DELETE" });
}

/**
 * PATCH request helper
 */
export async function apiPatch<T = unknown>(endpoint: string, data: unknown): Promise<T> {
  return apiRequest<T>(endpoint, {
    method: "PATCH",
    body: JSON.stringify(data),
  });
}

/**
 * Test-only: reset internal CSRF state.
 *
 * Tests for fetchCSRFToken / apiRequest write paths share module-level
 * state (csrfToken, csrfEndpointAvailable). This helper lets a test
 * decline that contagion.
 *
 * Not part of the runtime API surface — `__` prefix indicates internal.
 */
export function __resetCSRFForTesting(): void {
  csrfToken = null;
  csrfEndpointAvailable = null;
}

/**
 * Test-only: fetchCSRFToken invocation. Exposed so unit tests can verify
 * the 404 graceful-handling path without going through a write operation.
 */
export const __fetchCSRFTokenForTesting = fetchCSRFToken;

/**
 * Pagination parameters
 */
export interface PaginationParams {
  page?: number;
  limit?: number;
  sort?: string;
  order?: "asc" | "desc";
}

/**
 * Paginated response
 */
export interface PaginatedResponse<T> {
  data: T[];
  pagination: {
    page: number;
    limit: number;
    total: number;
    totalPages: number;
    hasNext: boolean;
    hasPrev: boolean;
  };
}

/**
 * GET request with pagination support
 */
export async function apiGetPaginated<T = unknown>(
  endpoint: string,
  params?: PaginationParams,
): Promise<PaginatedResponse<T>> {
  const queryParams = new URLSearchParams();

  if (params?.page) queryParams.append("page", params.page.toString());
  if (params?.limit) queryParams.append("limit", params.limit.toString());
  if (params?.sort) queryParams.append("sort", params.sort);
  if (params?.order) queryParams.append("order", params.order);

  const url = queryParams.toString()
    ? `${endpoint}?${queryParams.toString()}`
    : endpoint;

  return apiRequest<PaginatedResponse<T>>(url, { method: "GET" });
}

/**
 * GET without Authorization (health probes, pre-auth signup status).
 * Still applies timeout and credentials for CSRF cookies when present.
 */
export async function apiPublicGet<T = unknown>(
  endpoint: string,
  options: RequestInit = {},
): Promise<T> {
  const url = `${API_BASE_URL}${endpoint}`;
  const timeoutController = new AbortController();
  const timeoutId = setTimeout(() => timeoutController.abort(), DEFAULT_REQUEST_TIMEOUT_MS);
  try {
    const response = await fetch(url, {
      ...options,
      method: "GET",
      credentials: "include",
      signal: timeoutController.signal,
      headers: {
        Accept: "application/json",
        ...(options.headers as Record<string, string> | undefined),
      },
    });
    if (!response.ok) {
      const error = await response.json().catch(() => ({}));
      throw new Error(
        (error as { message?: string }).message ||
          `API request failed: ${response.status} ${response.statusText}`,
      );
    }
    return (await response.json()) as T;
  } finally {
    clearTimeout(timeoutId);
  }
}

/**
 * Low-level fetch returning the Response (SSE streams, CSV/blob downloads).
 * Applies auth headers, timeout, and a single 401 refresh retry.
 */
export async function apiFetchResponse(
  endpoint: string,
  options: RequestInit = {},
): Promise<Response> {
  const url = `${API_BASE_URL}${endpoint}`;
  const method = options.method || "GET";
  const isWriteOperation = ["POST", "PUT", "DELETE", "PATCH"].includes(
    method.toUpperCase(),
  );
  if (
    isWriteOperation &&
    !csrfToken &&
    csrfEndpointAvailable !== false
  ) {
    await fetchCSRFToken();
  }

  const timeoutController = new AbortController();
  const timeoutId = setTimeout(() => timeoutController.abort(), DEFAULT_REQUEST_TIMEOUT_MS);

  const doFetch = (retry = false) =>
    fetch(url, {
      ...options,
      headers: {
        ...getAuthHeadersRecord(isWriteOperation),
        ...(options.headers as Record<string, string> | undefined),
        ...(retry ? getAuthHeadersRecord(isWriteOperation) : {}),
      },
      credentials: "include",
      signal: timeoutController.signal,
    });

  try {
    const requestAccessToken = getStoredTokens()?.accessToken;
    let response = await doFetch();
    if (response.status === 401) {
      const refreshed = await refreshForRequest(requestAccessToken);
      if (refreshed) {
        response = await doFetch(true);
      }
    }
    return response;
  } finally {
    clearTimeout(timeoutId);
  }
}

/**
 * Pre-auth Switchyard requests (login/register) without Bearer or CSRF headers.
 */
export async function apiPublicFetchResponse(
  endpoint: string,
  options: RequestInit = {},
): Promise<Response> {
  const url = `${API_BASE_URL}${endpoint}`;
  const timeoutController = new AbortController();
  const timeoutId = setTimeout(() => timeoutController.abort(), DEFAULT_REQUEST_TIMEOUT_MS);
  try {
    return await fetch(url, {
      ...options,
      credentials: "include",
      signal: timeoutController.signal,
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
        ...(options.headers as Record<string, string> | undefined),
      },
    });
  } finally {
    clearTimeout(timeoutId);
  }
}

/** Exported for AuthContext to dedupe token refresh with apiRequest. */
export { attemptTokenRefresh };
