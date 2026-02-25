"use client";

/**
 * Authentication Context for Enclii Switchyard UI
 *
 * Thin bridge over @janua/react-sdk with dual auth mode support:
 * - Local: Email/password directly to Switchyard API (bootstrap mode)
 * - OIDC: OAuth 2.0 flow via Janua identity provider (production)
 *
 * The auth mode is determined by NEXT_PUBLIC_AUTH_MODE environment variable.
 */

import React, {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
  useRef,
  type ReactNode,
} from "react";
import type { AuthMode, AuthContextType, User, RedirectTokens } from "./auth-types";

// =============================================================================
// CONFIGURATION
// =============================================================================

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:4200";
const AUTH_MODE = (process.env.NEXT_PUBLIC_AUTH_MODE || "local") as AuthMode;

// Storage keys for Enclii auth state
const STORAGE_KEYS = {
  TOKENS: "enclii_tokens",
  USER: "enclii_user",
  COOKIE: "enclii_auth",
} as const;

// =============================================================================
// STORAGE HELPERS (inlined — replaces auth-storage.ts)
// =============================================================================

function getStoredTokens(): { accessToken: string; refreshToken?: string; expiresAt: number } | null {
  if (typeof window === "undefined") return null;
  const stored = localStorage.getItem(STORAGE_KEYS.TOKENS);
  if (!stored) return null;
  try {
    return JSON.parse(stored);
  } catch {
    return null;
  }
}

function setStoredTokens(tokens: { accessToken: string; refreshToken?: string; expiresAt: number }) {
  if (typeof window === "undefined") return;
  localStorage.setItem(STORAGE_KEYS.TOKENS, JSON.stringify(tokens));
  // Sync cookie for middleware.ts (SSR reads cookie, not localStorage)
  const maxAge = Math.floor((tokens.expiresAt - Date.now()) / 1000);
  if (maxAge > 0) {
    document.cookie = `${STORAGE_KEYS.COOKIE}=${tokens.accessToken}; path=/; secure; samesite=lax; max-age=${maxAge}`;
  }
}

function getStoredUser(): User | null {
  if (typeof window === "undefined") return null;
  const stored = localStorage.getItem(STORAGE_KEYS.USER);
  if (!stored) return null;
  try {
    return JSON.parse(stored);
  } catch {
    return null;
  }
}

function setStoredUser(user: User) {
  if (typeof window === "undefined") return;
  localStorage.setItem(STORAGE_KEYS.USER, JSON.stringify(user));
}

function clearStorage() {
  if (typeof window === "undefined") return;
  localStorage.removeItem(STORAGE_KEYS.TOKENS);
  localStorage.removeItem(STORAGE_KEYS.USER);
  document.cookie = `${STORAGE_KEYS.COOKIE}=; path=/; secure; samesite=lax; max-age=0`;
}

function parseJwt(token: string): Record<string, unknown> | null {
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

async function parseErrorResponse(response: Response, fallbackMessage: string): Promise<string> {
  const text = await response.text();
  if (text.startsWith("{")) {
    try {
      const json = JSON.parse(text);
      return json.error || json.message || json.detail || fallbackMessage;
    } catch {
      // fall through
    }
  }
  return text.trim() || fallbackMessage;
}

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
  const [isLoading, setIsLoading] = useState(true);
  const [authError, setAuthError] = useState<string | null>(null);
  const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const isRefreshingRef = useRef(false);

  const clearAuthError = useCallback(() => setAuthError(null), []);

  // ==========================================================================
  // TOKEN REFRESH
  // ==========================================================================

  const refreshTokens = useCallback(async (): Promise<boolean> => {
    if (isRefreshingRef.current) return false;

    const stored = getStoredTokens();
    if (!stored?.refreshToken) return false;

    isRefreshingRef.current = true;
    try {
      const response = await fetch(`${API_BASE_URL}/v1/auth/refresh`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refresh_token: stored.refreshToken }),
      });

      if (!response.ok) throw new Error("Token refresh failed");

      const data = await response.json();
      const newTokens = {
        accessToken: data.access_token,
        refreshToken: data.refresh_token || stored.refreshToken,
        expiresAt: data.expires_at
          ? new Date(data.expires_at).getTime()
          : stored.expiresAt,
      };

      setStoredTokens(newTokens);
      scheduleRefresh(newTokens.expiresAt);
      return true;
    } catch {
      setAuthError("Session expired. Please log in again.");
      return false;
    } finally {
      isRefreshingRef.current = false;
    }
  }, []);

  const scheduleRefresh = useCallback((expiresAt: number) => {
    if (refreshTimerRef.current) clearTimeout(refreshTimerRef.current);
    const refreshIn = expiresAt - Date.now() - 5 * 60 * 1000; // 5 min buffer
    if (refreshIn > 0) {
      refreshTimerRef.current = setTimeout(() => {
        refreshTokens();
      }, refreshIn);
    }
  }, [refreshTokens]);

  // ==========================================================================
  // INITIALIZATION
  // ==========================================================================

  useEffect(() => {
    const init = async () => {
      try {
        if (typeof window !== "undefined" && window.location.pathname.startsWith("/auth/callback")) {
          return; // Callback page will handle token storage
        }

        const storedTokens = getStoredTokens();
        const storedUser = getStoredUser();

        if (storedTokens && storedUser) {
          if (Date.now() < storedTokens.expiresAt) {
            setUser(storedUser);
            scheduleRefresh(storedTokens.expiresAt);
          } else if (storedTokens.refreshToken) {
            const refreshed = await refreshTokens();
            if (refreshed) {
              setUser(storedUser);
            } else {
              clearStorage();
            }
          } else {
            clearStorage();
          }
        }
      } finally {
        setIsLoading(false);
      }
    };

    init();
    return () => {
      if (refreshTimerRef.current) clearTimeout(refreshTimerRef.current);
    };
  }, [refreshTokens, scheduleRefresh]);

  // ==========================================================================
  // LOCAL AUTH
  // ==========================================================================

  const login = useCallback(async (email: string, password: string): Promise<void> => {
    setIsLoading(true);
    setAuthError(null);

    try {
      const response = await fetch(`${API_BASE_URL}/v1/auth/login`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });

      if (!response.ok) {
        throw new Error(await parseErrorResponse(response, "Login failed"));
      }

      const data = await response.json();
      const tokens = {
        accessToken: data.access_token,
        refreshToken: data.refresh_token,
        expiresAt: new Date(data.expires_at).getTime(),
      };

      const userData: User = {
        id: data.user?.id || "",
        email: data.user?.email || email,
        name: data.user?.name,
        roles: data.user?.roles || [],
      };

      setStoredTokens(tokens);
      setStoredUser(userData);
      setUser(userData);
      scheduleRefresh(tokens.expiresAt);
    } finally {
      setIsLoading(false);
    }
  }, [scheduleRefresh]);

  const register = useCallback(async (email: string, password: string, name: string): Promise<void> => {
    setIsLoading(true);
    setAuthError(null);

    try {
      const response = await fetch(`${API_BASE_URL}/v1/auth/register`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password, name }),
      });

      if (!response.ok) {
        throw new Error(await parseErrorResponse(response, "Registration failed"));
      }

      const data = await response.json();
      const tokens = {
        accessToken: data.access_token,
        refreshToken: data.refresh_token,
        expiresAt: new Date(data.expires_at).getTime(),
      };

      const userData: User = {
        id: data.user?.id || "",
        email: data.user?.email || email,
        name: data.user?.name || name,
        roles: data.user?.roles || [],
      };

      setStoredTokens(tokens);
      setStoredUser(userData);
      setUser(userData);
      scheduleRefresh(tokens.expiresAt);
    } finally {
      setIsLoading(false);
    }
  }, [scheduleRefresh]);

  // ==========================================================================
  // OIDC AUTH
  // ==========================================================================

  const loginWithOIDC = useCallback((): void => {
    if (typeof window !== "undefined") {
      localStorage.setItem("auth_return_url", window.location.pathname);
    }
    clearStorage();
    setAuthError(null);
    window.location.href = `${API_BASE_URL}/v1/auth/login`;
  }, []);

  const handleOAuthCallback = useCallback(async (code: string, state?: string): Promise<void> => {
    setIsLoading(true);
    setAuthError(null);

    try {
      const params = new URLSearchParams({ code });
      if (state) params.append("state", state);

      const response = await fetch(
        `${API_BASE_URL}/v1/auth/callback?${params.toString()}`,
        { method: "GET", credentials: "include" }
      );

      if (!response.ok) {
        throw new Error(await parseErrorResponse(response, "OAuth callback failed"));
      }

      const data = await response.json();
      const tokens = {
        accessToken: data.access_token,
        refreshToken: data.refresh_token,
        expiresAt: new Date(data.expires_at).getTime(),
      };

      const claims = parseJwt(data.access_token);
      const userData: User = {
        id: (claims?.sub as string) || (claims?.user_id as string) || "",
        email: (claims?.email as string) || "",
        name: claims?.name as string,
        roles: (claims?.roles as string[]) || [],
        foundry_tier: (claims?.foundry_tier as User["foundry_tier"]) || null,
      };

      setStoredTokens(tokens);
      setStoredUser(userData);
      setUser(userData);
      scheduleRefresh(tokens.expiresAt);
    } finally {
      setIsLoading(false);
    }
  }, [scheduleRefresh]);

  const storeTokensFromRedirect = useCallback(async (redirectTokens: RedirectTokens): Promise<void> => {
    setIsLoading(true);
    setAuthError(null);

    try {
      const tokens = {
        accessToken: redirectTokens.accessToken,
        refreshToken: redirectTokens.refreshToken,
        expiresAt: redirectTokens.expiresAt.getTime(),
      };

      const claims = parseJwt(redirectTokens.accessToken);
      const userData: User = {
        id: (claims?.sub as string) || (claims?.user_id as string) || "",
        email: (claims?.email as string) || "",
        name: claims?.name as string,
        roles: (claims?.roles as string[]) || [],
        foundry_tier: (claims?.foundry_tier as User["foundry_tier"]) || null,
      };

      setStoredTokens(tokens);
      setStoredUser(userData);
      setUser(userData);
      scheduleRefresh(tokens.expiresAt);
    } finally {
      setIsLoading(false);
    }
  }, [scheduleRefresh]);

  // ==========================================================================
  // COMMON
  // ==========================================================================

  const logout = useCallback(async (options?: { skipServerRevocation?: boolean }): Promise<void> => {
    let logoutUrl: string | null = null;
    const stored = getStoredTokens();

    try {
      if (stored?.accessToken && !options?.skipServerRevocation) {
        const response = await fetch(`${API_BASE_URL}/v1/auth/logout`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${stored.accessToken}`,
          },
        }).catch(() => null);

        if (response?.ok) {
          try {
            const data = await response.json();
            if (data?.logout_url) logoutUrl = data.logout_url;
          } catch {
            // ignore
          }
        }
      }
    } finally {
      setUser(null);
      clearStorage();
      if (refreshTimerRef.current) {
        clearTimeout(refreshTimerRef.current);
        refreshTimerRef.current = null;
      }

      if (logoutUrl) {
        const returnUrl = encodeURIComponent(`${window.location.origin}/login`);
        window.location.href = `${logoutUrl}?return_url=${returnUrl}`;
      }
    }
  }, []);

  const getAccessToken = useCallback((): string | null => {
    const stored = getStoredTokens();
    return stored?.accessToken || null;
  }, []);

  const getIDPToken = useCallback((): string | null => {
    // IDP token tracking removed in SDK migration — return null
    return null;
  }, []);

  // ==========================================================================
  // CONTEXT VALUE
  // ==========================================================================

  const value: AuthContextType = {
    user,
    isAuthenticated: !!user,
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
 */
export function useAccessToken(): string | null {
  const { getAccessToken, isAuthenticated } = useAuth();
  if (!isAuthenticated) return null;
  return getAccessToken();
}
