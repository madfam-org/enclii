/**
 * Auth Storage
 * Storage helpers and JWT utilities for authentication
 */

import type { TokenInfo, User } from "./auth-types";

// =============================================================================
// CONFIGURATION
// =============================================================================

// Token refresh buffer - refresh 5 minutes before expiry
export const TOKEN_REFRESH_BUFFER_MS = 5 * 60 * 1000;

// =============================================================================
// STORAGE HELPERS
// =============================================================================

export const storage = {
  getTokens(): TokenInfo | null {
    if (typeof window === "undefined") return null;
    const stored = localStorage.getItem("enclii_tokens");
    if (!stored) return null;
    try {
      return JSON.parse(stored);
    } catch {
      return null;
    }
  },

  setTokens(tokens: TokenInfo): void {
    if (typeof window === "undefined") return;
    localStorage.setItem("enclii_tokens", JSON.stringify(tokens));
    // Sync cookie for middleware.ts (SSR reads cookie, not localStorage)
    const maxAge = Math.floor((tokens.expiresAt - Date.now()) / 1000);
    if (maxAge > 0) {
      document.cookie = `enclii_auth=${tokens.accessToken}; path=/; secure; samesite=lax; max-age=${maxAge}`;
    }
  },

  clearTokens(): void {
    if (typeof window === "undefined") return;
    localStorage.removeItem("enclii_tokens");
    document.cookie = 'enclii_auth=; path=/; secure; samesite=lax; max-age=0';
  },

  getUser(): User | null {
    if (typeof window === "undefined") return null;
    const stored = localStorage.getItem("enclii_user");
    if (!stored) return null;
    try {
      return JSON.parse(stored);
    } catch {
      return null;
    }
  },

  setUser(user: User): void {
    if (typeof window === "undefined") return;
    localStorage.setItem("enclii_user", JSON.stringify(user));
  },

  clearUser(): void {
    if (typeof window === "undefined") return;
    localStorage.removeItem("enclii_user");
  },

  clear(): void {
    this.clearTokens();
    this.clearUser();
  },
};

// =============================================================================
// JWT HELPERS
// =============================================================================

/**
 * Parse JWT token payload
 * @param token - JWT token string
 * @returns Decoded payload or null if parsing fails
 */
export function parseJwt(token: string): Record<string, unknown> | null {
  try {
    const base64Url = token.split(".")[1];
    const base64 = base64Url.replace(/-/g, "+").replace(/_/g, "/");
    const jsonPayload = decodeURIComponent(
      atob(base64)
        .split("")
        .map((c) => "%" + ("00" + c.charCodeAt(0).toString(16)).slice(-2))
        .join("")
    );
    return JSON.parse(jsonPayload);
  } catch {
    return null;
  }
}

/**
 * Check if token is expired (with buffer time)
 * @param expiresAt - Unix timestamp of token expiry
 * @returns true if token is expired or about to expire
 */
export function isTokenExpired(expiresAt: number): boolean {
  return Date.now() >= expiresAt - TOKEN_REFRESH_BUFFER_MS;
}

// =============================================================================
// API HELPERS
// =============================================================================

/**
 * Safely parse error response, handling both JSON and plain text responses.
 * @param response - Fetch Response object
 * @param fallbackMessage - Message to use if parsing fails
 * @returns Error message from the response body
 */
export async function parseErrorResponse(
  response: Response,
  fallbackMessage: string
): Promise<string> {
  const contentType = response.headers.get("content-type");
  const text = await response.text();

  // Try to parse as JSON if content-type suggests it or if it looks like JSON
  if (contentType?.includes("application/json") || text.startsWith("{")) {
    try {
      const json = JSON.parse(text);
      return json.error || json.message || json.detail || fallbackMessage;
    } catch {
      // JSON parsing failed, fall through to use text
    }
  }

  // Return plain text error if not empty, otherwise fallback
  return text.trim() || fallbackMessage;
}
