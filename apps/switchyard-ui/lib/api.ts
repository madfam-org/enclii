/**
 * API utility for making authenticated requests to the Switchyard API
 *
 * SECURITY: Implements authentication with JWT tokens and CSRF protection.
 * For production deployment with OAuth 2.0 / OIDC, see:
 * - contexts/AuthContext.tsx
 * - SECURITY_AUDIT_COMPREHENSIVE_2025.md
 */

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:4200";
const AUTH_MODE = process.env.NEXT_PUBLIC_AUTH_MODE || "local";

// CSRF token cache
let csrfToken: string | null = null;

// Token refresh state management
let isRefreshing = false;
let refreshPromise: Promise<boolean> | null = null;

/**
 * Attempt to refresh the access token using the refresh token
 * Prevents concurrent refresh attempts by returning shared promise
 * Works for both local and OIDC modes.
 */
async function attemptTokenRefresh(): Promise<boolean> {
  if (typeof window === "undefined") {
    return false;
  }

  // Return existing refresh promise if already refreshing
  if (isRefreshing && refreshPromise) {
    return refreshPromise;
  }

  const storedTokens = localStorage.getItem("enclii_tokens");
  if (!storedTokens) {
    return false;
  }

  try {
    const tokens = JSON.parse(storedTokens);
    if (!tokens.refreshToken) {
      return false;
    }

    isRefreshing = true;
    refreshPromise = (async () => {
      try {
        const response = await fetch(`${API_BASE_URL}/v1/auth/refresh`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          credentials: "include",
          body: JSON.stringify({ refresh_token: tokens.refreshToken }),
        });

        if (response.ok) {
          const data = await response.json();
          const newTokens = {
            ...tokens,
            accessToken: data.access_token,
            expiresAt: data.expires_at
              ? new Date(data.expires_at).getTime()
              : tokens.expiresAt,
          };
          localStorage.setItem("enclii_tokens", JSON.stringify(newTokens));
          return true;
        }
        return false;
      } catch (e) {
        console.error("Token refresh failed:", e);
        return false;
      } finally {
        isRefreshing = false;
        refreshPromise = null;
      }
    })();

    return refreshPromise;
  } catch {
    isRefreshing = false;
    refreshPromise = null;
    return false;
  }
}

/**
 * Get authentication headers for API requests
 *
 * Retrieves JWT token from localStorage (set by AuthContext)
 * Includes CSRF token for write operations
 */
function getAuthHeaders(includeCSRF: boolean = false): HeadersInit {
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
 */
async function fetchCSRFToken(): Promise<void> {
  try {
    const response = await fetch(`${API_BASE_URL}/v1/csrf`, {
      credentials: "include", // Include cookies
    });

    if (response.ok) {
      const token = response.headers.get("X-CSRF-Token");
      if (token) {
        csrfToken = token;
      }
    }
  } catch (error) {
    console.error("Failed to fetch CSRF token:", error);
  }
}

/**
 * Make an authenticated API request with CSRF protection
 *
 * @param endpoint - API endpoint path (e.g., '/v1/projects')
 * @param options - Fetch options (method, body, etc.)
 * @returns Promise with the response
 */
export async function apiRequest<T = any>(
  endpoint: string,
  options: RequestInit = {},
): Promise<T> {
  const url = `${API_BASE_URL}${endpoint}`;
  const method = options.method || "GET";
  const isWriteOperation = ["POST", "PUT", "DELETE", "PATCH"].includes(
    method.toUpperCase(),
  );

  // Fetch CSRF token for write operations if not cached
  if (isWriteOperation && !csrfToken) {
    await fetchCSRFToken();
  }

  const headers: HeadersInit = {
    ...getAuthHeaders(isWriteOperation),
    ...options.headers,
  };

  try {
    const response = await fetch(url, {
      ...options,
      headers,
      credentials: "include", // Include cookies for CSRF
    });

    // Handle authentication errors with retry
    if (response.status === 401) {
      // Attempt to refresh the token before giving up
      const refreshed = await attemptTokenRefresh();
      if (refreshed) {
        // Retry the request with the new token
        const retryHeaders: HeadersInit = {
          ...getAuthHeaders(isWriteOperation),
          ...options.headers,
        };
        const retryResponse = await fetch(url, {
          ...options,
          headers: retryHeaders,
          credentials: "include",
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
        // OIDC: don't dispatch events or clear storage — let the scheduled
        // refresh in AuthContext handle it. Components show local error states.
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
    console.error(`API request failed for ${endpoint}:`, error);
    throw error;
  }
}

/**
 * GET request helper
 */
export async function apiGet<T = any>(endpoint: string): Promise<T> {
  return apiRequest<T>(endpoint, { method: "GET" });
}

/**
 * POST request helper
 */
export async function apiPost<T = any>(
  endpoint: string,
  data: any,
): Promise<T> {
  return apiRequest<T>(endpoint, {
    method: "POST",
    body: JSON.stringify(data),
  });
}

/**
 * PUT request helper
 */
export async function apiPut<T = any>(endpoint: string, data: any): Promise<T> {
  return apiRequest<T>(endpoint, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

/**
 * DELETE request helper
 */
export async function apiDelete<T = any>(endpoint: string): Promise<T> {
  return apiRequest<T>(endpoint, { method: "DELETE" });
}

/**
 * PATCH request helper
 */
export async function apiPatch<T = any>(endpoint: string, data: any): Promise<T> {
  return apiRequest<T>(endpoint, {
    method: "PATCH",
    body: JSON.stringify(data),
  });
}

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
export async function apiGetPaginated<T = any>(
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
