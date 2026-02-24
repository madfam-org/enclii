"use client";

/**
 * Authentication Context for Enclii Switchyard UI
 *
 * Supports both local authentication and OIDC/OAuth via Janua.
 *
 * Authentication Modes:
 * - Local: Email/password directly to Switchyard API
 * - OIDC: OAuth 2.0 flow via external identity provider (Janua)
 *
 * The auth mode is determined by NEXT_PUBLIC_AUTH_MODE environment variable.
 *
 * Split structure:
 * - auth-types.ts: Type definitions
 * - auth-storage.ts: Storage and JWT utilities
 */

import React, {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
  useRef,
  ReactNode,
} from "react";
import type {
  User,
  TokenInfo,
  AuthContextType,
  AuthMode,
  RedirectTokens,
} from "./auth-types";
import {
  storage,
  parseJwt,
  isTokenExpired,
  parseErrorResponse,
} from "./auth-storage";

// =============================================================================
// CONFIGURATION
// =============================================================================

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:4200";
const AUTH_MODE = (process.env.NEXT_PUBLIC_AUTH_MODE || "local") as AuthMode;

// =============================================================================
// CONTEXT
// =============================================================================

const AuthContext = createContext<AuthContextType | undefined>(undefined);

// =============================================================================
// PROVIDER
// =============================================================================

interface AuthProviderProps {
  children: ReactNode;
}

export function AuthProvider({ children }: AuthProviderProps) {
  const [user, setUser] = useState<User | null>(null);
  const [tokens, setTokens] = useState<TokenInfo | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [authError, setAuthError] = useState<string | null>(null);
  const [refreshTimer, setRefreshTimer] = useState<NodeJS.Timeout | null>(null);
  const isRefreshingRef = useRef(false); // Prevent concurrent refresh attempts
  const refreshTokensRef = useRef<() => Promise<boolean>>(null!); // Stable ref for token refresh
  const isInitializingRef = useRef(true); // Prevent logout during init
  const refreshFailCountRef = useRef(0); // Track consecutive refresh failures

  const clearAuthError = useCallback(() => {
    setAuthError(null);
  }, []);

  // ==========================================================================
  // TOKEN REFRESH SCHEDULING
  // ==========================================================================

  const scheduleTokenRefresh = useCallback(
    (expiresAt: number) => {
      // Clear existing timer
      if (refreshTimer) {
        clearTimeout(refreshTimer);
      }

      // Calculate when to refresh (5 minutes before expiry)
      const TOKEN_REFRESH_BUFFER_MS = 5 * 60 * 1000;
      const refreshIn = expiresAt - Date.now() - TOKEN_REFRESH_BUFFER_MS;

      if (refreshIn > 0) {
        const timer = setTimeout(() => {
          // Use ref to avoid stale closure - always calls latest refreshTokens
          refreshTokensRef.current?.();
        }, refreshIn);
        setRefreshTimer(timer);
      }
    },
    [refreshTimer]
  );

  // ==========================================================================
  // INITIALIZATION
  // ==========================================================================

  useEffect(() => {
    const initAuth = async () => {
      try {
        const storedTokens = storage.getTokens();
        const storedUser = storage.getUser();

        if (storedTokens && storedUser) {
          // Check if token is truly expired (past actual expiry, ignoring buffer)
          const isActuallyExpired = Date.now() >= storedTokens.expiresAt;
          const isInBufferZone = isTokenExpired(storedTokens.expiresAt) && !isActuallyExpired;

          if (!isTokenExpired(storedTokens.expiresAt) || isInBufferZone) {
            // Token still valid (or within 5-min buffer — API will accept it)
            setTokens(storedTokens);
            setUser(storedUser);
            scheduleTokenRefresh(storedTokens.expiresAt);
          } else if (storedTokens.refreshToken) {
            // Token truly expired — attempt refresh
            const refreshed = await refreshTokens();
            if (!refreshed) {
              setAuthError('Session expired. Please log in again.');
              // Don't clear storage here — let the user see the error and re-login
            }
          } else {
            storage.clear();
          }
        }
      } finally {
        setIsLoading(false);
        isInitializingRef.current = false;
      }
    };

    initAuth();
  }, []);

  // Cleanup timer on unmount
  useEffect(() => {
    return () => {
      if (refreshTimer) {
        clearTimeout(refreshTimer);
      }
    };
  }, [refreshTimer]);

  // ==========================================================================
  // API HELPERS
  // ==========================================================================

  const apiRequest = async (
    endpoint: string,
    options: RequestInit = {}
  ): Promise<Response> => {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      ...(options.headers as Record<string, string>),
    };

    if (tokens?.accessToken) {
      headers["Authorization"] = `Bearer ${tokens.accessToken}`;
    }

    return fetch(`${API_BASE_URL}${endpoint}`, {
      ...options,
      headers,
    });
  };

  // ==========================================================================
  // LOCAL AUTHENTICATION
  // ==========================================================================

  const login = async (email: string, password: string): Promise<void> => {
    setIsLoading(true);
    setAuthError(null);

    try {
      const response = await apiRequest("/v1/auth/login", {
        method: "POST",
        body: JSON.stringify({ email, password }),
      });

      if (!response.ok) {
        const errorMessage = await parseErrorResponse(response, "Login failed");
        throw new Error(errorMessage);
      }

      const data = await response.json();

      const tokenInfo: TokenInfo = {
        accessToken: data.access_token,
        refreshToken: data.refresh_token,
        expiresAt: new Date(data.expires_at).getTime(),
        tokenType: data.token_type || "Bearer",
      };

      const userData: User = {
        id: data.user?.id || "",
        email: data.user?.email || email,
        name: data.user?.name,
        roles: data.user?.roles || [],
      };

      setTokens(tokenInfo);
      setUser(userData);
      storage.setTokens(tokenInfo);
      storage.setUser(userData);
      scheduleTokenRefresh(tokenInfo.expiresAt);
    } finally {
      setIsLoading(false);
    }
  };

  const register = async (
    email: string,
    password: string,
    name: string
  ): Promise<void> => {
    setIsLoading(true);
    setAuthError(null);

    try {
      const response = await apiRequest("/v1/auth/register", {
        method: "POST",
        body: JSON.stringify({ email, password, name }),
      });

      if (!response.ok) {
        const errorMessage = await parseErrorResponse(
          response,
          "Registration failed"
        );
        throw new Error(errorMessage);
      }

      const data = await response.json();

      const tokenInfo: TokenInfo = {
        accessToken: data.access_token,
        refreshToken: data.refresh_token,
        expiresAt: new Date(data.expires_at).getTime(),
        tokenType: data.token_type || "Bearer",
      };

      const userData: User = {
        id: data.user?.id || "",
        email: data.user?.email || email,
        name: data.user?.name || name,
        roles: data.user?.roles || [],
      };

      setTokens(tokenInfo);
      setUser(userData);
      storage.setTokens(tokenInfo);
      storage.setUser(userData);
      scheduleTokenRefresh(tokenInfo.expiresAt);
    } finally {
      setIsLoading(false);
    }
  };

  // ==========================================================================
  // OIDC AUTHENTICATION
  // ==========================================================================

  const loginWithOIDC = (): void => {
    // Store current URL for redirect after login
    if (typeof window !== "undefined") {
      localStorage.setItem("auth_return_url", window.location.pathname);
    }

    // Clear stale tokens before redirect so that when the callback page loads,
    // initAuth() won't find expired tokens and race with storeTokensFromRedirect()
    storage.clear();

    // Redirect to backend OIDC login endpoint
    // The backend will redirect to the OIDC provider (Janua)
    window.location.href = `${API_BASE_URL}/v1/auth/login`;
  };

  const handleOAuthCallback = async (
    code: string,
    state?: string
  ): Promise<void> => {
    setIsLoading(true);
    setAuthError(null);

    try {
      const params = new URLSearchParams({ code });
      if (state) {
        params.append("state", state);
      }

      const response = await fetch(
        `${API_BASE_URL}/v1/auth/callback?${params.toString()}`,
        {
          method: "GET",
          credentials: "include",
        }
      );

      if (!response.ok) {
        const errorMessage = await parseErrorResponse(
          response,
          "OAuth callback failed"
        );
        throw new Error(errorMessage);
      }

      const data = await response.json();

      const tokenInfo: TokenInfo = {
        accessToken: data.access_token,
        refreshToken: data.refresh_token,
        expiresAt: new Date(data.expires_at).getTime(),
        tokenType: data.token_type || "Bearer",
        idpToken: data.idp_token,
        idpTokenExpiresAt: data.idp_token_expires_at
          ? new Date(data.idp_token_expires_at).getTime()
          : undefined,
      };

      const claims = parseJwt(data.access_token);
      const userData: User = {
        id: (claims?.sub as string) || (claims?.user_id as string) || "",
        email: (claims?.email as string) || "",
        name: claims?.name as string,
        roles: (claims?.roles as string[]) || [],
        foundry_tier: (claims?.foundry_tier as User['foundry_tier']) || null,
      };

      setTokens(tokenInfo);
      setUser(userData);
      storage.setTokens(tokenInfo);
      storage.setUser(userData);
      scheduleTokenRefresh(tokenInfo.expiresAt);
    } finally {
      setIsLoading(false);
    }
  };

  const storeTokensFromRedirect = async (
    redirectTokens: RedirectTokens
  ): Promise<void> => {
    setIsLoading(true);
    setAuthError(null);

    try {
      const tokenInfo: TokenInfo = {
        accessToken: redirectTokens.accessToken,
        refreshToken: redirectTokens.refreshToken,
        expiresAt: redirectTokens.expiresAt.getTime(),
        tokenType: redirectTokens.tokenType,
        idpToken: redirectTokens.idpToken,
        idpTokenExpiresAt: redirectTokens.idpTokenExpiresAt?.getTime(),
      };

      const claims = parseJwt(redirectTokens.accessToken);
      const userData: User = {
        id: (claims?.sub as string) || (claims?.user_id as string) || "",
        email: (claims?.email as string) || "",
        name: claims?.name as string,
        roles: (claims?.roles as string[]) || [],
        foundry_tier: (claims?.foundry_tier as User['foundry_tier']) || null,
      };

      setTokens(tokenInfo);
      setUser(userData);
      storage.setTokens(tokenInfo);
      storage.setUser(userData);
      scheduleTokenRefresh(tokenInfo.expiresAt);
    } finally {
      setIsLoading(false);
    }
  };

  // ==========================================================================
  // COMMON METHODS
  // ==========================================================================

  const logout = async (options?: { skipServerRevocation?: boolean }): Promise<void> => {
    let logoutUrl: string | null = null;

    try {
      // Only call server-side logout (which revokes the session in Redis)
      // for intentional user logouts — NOT for 401 cascade / forced logouts.
      if (tokens?.accessToken && !options?.skipServerRevocation) {
        const response = await apiRequest("/v1/auth/logout", {
          method: "POST",
        }).catch(() => null);

        if (response?.ok) {
          try {
            const data = await response.json();
            if (data?.logout_url) {
              logoutUrl = data.logout_url;
            }
          } catch {
            // JSON parsing failed, ignore
          }
        }
      }
    } finally {
      setTokens(null);
      setUser(null);
      storage.clear();

      if (refreshTimer) {
        clearTimeout(refreshTimer);
        setRefreshTimer(null);
      }

      if (logoutUrl) {
        const returnUrl = encodeURIComponent(`${window.location.origin}/login`);
        window.location.href = `${logoutUrl}?return_url=${returnUrl}`;
      }
    }
  };

  const refreshTokens = async (): Promise<boolean> => {
    // Prevent concurrent refresh attempts
    if (isRefreshingRef.current) {
      console.debug("Token refresh already in progress, skipping");
      return false;
    }

    const currentTokens = tokens || storage.getTokens();

    if (!currentTokens?.refreshToken) {
      return false;
    }

    isRefreshingRef.current = true;

    try {
      // Both local and OIDC modes use the refresh token endpoint
      const response = await fetch(`${API_BASE_URL}/v1/auth/refresh`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          refresh_token: currentTokens.refreshToken,
        }),
      });

      if (!response.ok) {
        throw new Error("Token refresh failed");
      }

      const data = await response.json();

      const newTokenInfo: TokenInfo = {
        accessToken: data.access_token,
        refreshToken: data.refresh_token || currentTokens.refreshToken,
        expiresAt: data.expires_at
          ? new Date(data.expires_at).getTime()
          : currentTokens.expiresAt,
        tokenType: data.token_type || "Bearer",
        idpToken: data.idp_token || currentTokens.idpToken,
        idpTokenExpiresAt: data.idp_token_expires_at
          ? new Date(data.idp_token_expires_at).getTime()
          : currentTokens.idpTokenExpiresAt,
      };

      setTokens(newTokenInfo);
      storage.setTokens(newTokenInfo);
      scheduleTokenRefresh(newTokenInfo.expiresAt);
      refreshFailCountRef.current = 0;

      return true;
    } catch (error) {
      console.error("Token refresh failed:", error);
      refreshFailCountRef.current += 1;
      // After 3 consecutive failures, surface an error so the UI can show
      // "Session expired" — but never auto-logout / auto-redirect.
      if (refreshFailCountRef.current >= 3) {
        setAuthError("Session expired. Please log in again.");
      }
      return false;
    } finally {
      isRefreshingRef.current = false;
    }
  };

  // Keep the ref updated with the latest refreshTokens function
  // This prevents stale closures in scheduled timeouts
  useEffect(() => {
    refreshTokensRef.current = refreshTokens;
  });

  const getAccessToken = (): string | null => {
    const currentTokens = tokens || storage.getTokens();

    if (!currentTokens) {
      return null;
    }

    // Trigger background refresh if token is approaching expiry (within 5-min buffer)
    // Note: isTokenExpired checks the buffer, so token is still usable
    if (isTokenExpired(currentTokens.expiresAt)) {
      // Fire and forget - the scheduled refresh should handle this,
      // but this is a safety net in case it was missed
      refreshTokens().catch(() => {
        // Silently ignore - refresh failure is handled within refreshTokens
      });
    }

    return currentTokens.accessToken;
  };

  const getIDPToken = (): string | null => {
    const currentTokens = tokens || storage.getTokens();

    if (!currentTokens?.idpToken) {
      return null;
    }

    if (
      currentTokens.idpTokenExpiresAt &&
      Date.now() >= currentTokens.idpTokenExpiresAt
    ) {
      return null;
    }

    return currentTokens.idpToken;
  };

  // ==========================================================================
  // CONTEXT VALUE
  // ==========================================================================

  const value: AuthContextType = {
    user,
    isAuthenticated: !!user && !!tokens,
    isLoading,
    authMode: AUTH_MODE,
    authError,
    clearAuthError,
    login,
    register,
    loginWithOIDC,
    handleOAuthCallback,
    storeTokensFromRedirect,
    logout,
    refreshTokens,
    getAccessToken,
    getIDPToken,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

// =============================================================================
// HOOKS
// =============================================================================

export function useAuth(): AuthContextType {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
}

/**
 * Hook for protecting routes that require authentication.
 * Returns redirect info for unauthenticated users.
 */
export function useRequireAuth(): {
  isAuthenticated: boolean;
  isLoading: boolean;
  shouldRedirect: boolean;
} {
  const { isAuthenticated, isLoading } = useAuth();

  return {
    isAuthenticated,
    isLoading,
    shouldRedirect: !isLoading && !isAuthenticated,
  };
}

/**
 * Hook for getting the access token for API requests.
 * Automatically handles token refresh.
 */
export function useAccessToken(): string | null {
  const { getAccessToken, isAuthenticated } = useAuth();

  if (!isAuthenticated) {
    return null;
  }

  return getAccessToken();
}
