"use client";

/**
 * Authentication Context for Enclii Switchyard UI
 *
 * Thin bridge over @janua/react-sdk with dual auth mode support:
 * - Local: Email/password directly to Switchyard API (bootstrap mode)
 * - OIDC: Delegates to JanuaProvider from @janua/nextjs (production)
 *
 * All consumers use the same useAuth() interface regardless of mode.
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
import { JanuaProvider, useJanua, useAuth as useJanuaAuth } from "@janua/nextjs";
import type { AuthMode, AuthContextType, User, RedirectTokens } from "./auth-types";

// =============================================================================
// CONFIGURATION
// =============================================================================

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:4200";
const AUTH_MODE = (process.env.NEXT_PUBLIC_AUTH_MODE || "local") as AuthMode;
const JANUA_BASE_URL = process.env.NEXT_PUBLIC_JANUA_URL || "https://auth.madfam.io";

const STORAGE_KEYS = {
  TOKENS: "enclii_tokens",
  USER: "enclii_user",
  COOKIE: "enclii_auth",
} as const;

// =============================================================================
// STORAGE HELPERS (still needed for cookie sync with middleware)
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
  const maxAge = Math.floor((tokens.expiresAt - Date.now()) / 1000);
  if (maxAge > 0) {
    document.cookie = `${STORAGE_KEYS.COOKIE}=${tokens.accessToken}; path=/; secure; samesite=lax; max-age=${maxAge}`;
  }
}

function setStoredUser(user: User) {
  if (typeof window === "undefined") return;
  localStorage.setItem(STORAGE_KEYS.USER, JSON.stringify(user));
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
    } catch { /* fall through */ }
  }
  return text.trim() || fallbackMessage;
}

// =============================================================================
// CONTEXT
// =============================================================================

const AuthContext = createContext<AuthContextType | undefined>(undefined);

// =============================================================================
// OIDC MODE BRIDGE — delegates to Janua SDK
// =============================================================================

function OIDCAuthBridge({ children }: { children: ReactNode }) {
  const janua = useJanua();
  const januaAuth = useJanuaAuth();
  const [authError, setAuthError] = useState<string | null>(null);

  // Sync Janua token to cookie for middleware compatibility
  useEffect(() => {
    if (januaAuth.isAuthenticated && janua.client) {
      const token = janua.client.getAccessToken?.();
      if (token) {
        const maxAge = 15 * 60; // 15 minutes
        document.cookie = `${STORAGE_KEYS.COOKIE}=${token}; path=/; secure; samesite=lax; max-age=${maxAge}`;
      }
    } else if (!januaAuth.isAuthenticated && !januaAuth.isLoading) {
      document.cookie = `${STORAGE_KEYS.COOKIE}=; path=/; secure; samesite=lax; max-age=0`;
    }
  }, [januaAuth.isAuthenticated, januaAuth.isLoading, janua.client]);

  // Map Janua user to Enclii User type
  const user: User | null = januaAuth.user
    ? {
        id: januaAuth.user.id || "",
        email: januaAuth.user.email || "",
        name: januaAuth.user.name || januaAuth.user.display_name,
        roles: (januaAuth.user as Record<string, unknown>).roles as string[] || [],
        foundry_tier: (januaAuth.user as Record<string, unknown>).foundry_tier as User["foundry_tier"] || null,
      }
    : null;

  const value: AuthContextType = {
    user,
    isAuthenticated: januaAuth.isAuthenticated,
    isLoading: januaAuth.isLoading,
    authMode: "oidc",
    authError,
    clearAuthError: () => setAuthError(null),
    // Local auth methods are no-ops in OIDC mode
    login: async () => { throw new Error("Local login not available in OIDC mode"); },
    register: async () => { throw new Error("Registration not available in OIDC mode"); },
    // OIDC methods delegate to Janua SDK
    loginWithOIDC: () => {
      if (typeof window !== "undefined") {
        localStorage.setItem("auth_return_url", window.location.pathname);
      }
      window.location.href = `${API_BASE_URL}/v1/auth/login`;
    },
    handleOAuthCallback: async () => { /* Handled by Janua SDK */ },
    storeTokensFromRedirect: async () => { /* Handled by Janua SDK */ },
    logout: async () => {
      await janua.signOut();
      clearStorage();
    },
    refreshTokens: async () => true, // SDK handles refresh automatically
    getAccessToken: () => janua.client?.getAccessToken?.() || null,
    getIDPToken: () => null,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

// =============================================================================
// LOCAL MODE PROVIDER — kept for dev bootstrap
// =============================================================================

function LocalAuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [authError, setAuthError] = useState<string | null>(null);
  const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const isRefreshingRef = useRef(false);

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
        expiresAt: data.expires_at ? new Date(data.expires_at).getTime() : stored.expiresAt,
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
    const refreshIn = expiresAt - Date.now() - 5 * 60 * 1000;
    if (refreshIn > 0) {
      refreshTimerRef.current = setTimeout(() => { refreshTokens(); }, refreshIn);
    }
  }, [refreshTokens]);

  useEffect(() => {
    const init = async () => {
      try {
        if (typeof window !== "undefined" && window.location.pathname.startsWith("/auth/callback")) return;
        const storedTokens = getStoredTokens();
        const storedUser = getStoredUser();
        if (storedTokens && storedUser) {
          if (Date.now() < storedTokens.expiresAt) {
            setUser(storedUser);
            scheduleRefresh(storedTokens.expiresAt);
          } else if (storedTokens.refreshToken) {
            const refreshed = await refreshTokens();
            if (refreshed) setUser(storedUser);
            else clearStorage();
          } else {
            clearStorage();
          }
        }
      } finally {
        setIsLoading(false);
      }
    };
    init();
    return () => { if (refreshTimerRef.current) clearTimeout(refreshTimerRef.current); };
  }, [refreshTokens, scheduleRefresh]);

  const login = useCallback(async (email: string, password: string): Promise<void> => {
    setIsLoading(true);
    setAuthError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/v1/auth/login`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });
      if (!response.ok) throw new Error(await parseErrorResponse(response, "Login failed"));
      const data = await response.json();
      const tokens = { accessToken: data.access_token, refreshToken: data.refresh_token, expiresAt: new Date(data.expires_at).getTime() };
      const userData: User = { id: data.user?.id || "", email: data.user?.email || email, name: data.user?.name, roles: data.user?.roles || [] };
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
      if (!response.ok) throw new Error(await parseErrorResponse(response, "Registration failed"));
      const data = await response.json();
      const tokens = { accessToken: data.access_token, refreshToken: data.refresh_token, expiresAt: new Date(data.expires_at).getTime() };
      const userData: User = { id: data.user?.id || "", email: data.user?.email || email, name: data.user?.name || name, roles: data.user?.roles || [] };
      setStoredTokens(tokens);
      setStoredUser(userData);
      setUser(userData);
      scheduleRefresh(tokens.expiresAt);
    } finally {
      setIsLoading(false);
    }
  }, [scheduleRefresh]);

  const handleOAuthCallback = useCallback(async (code: string, state?: string): Promise<void> => {
    setIsLoading(true);
    setAuthError(null);
    try {
      const params = new URLSearchParams({ code });
      if (state) params.append("state", state);
      const response = await fetch(`${API_BASE_URL}/v1/auth/callback?${params.toString()}`, { method: "GET", credentials: "include" });
      if (!response.ok) throw new Error(await parseErrorResponse(response, "OAuth callback failed"));
      const data = await response.json();
      const tokens = { accessToken: data.access_token, refreshToken: data.refresh_token, expiresAt: new Date(data.expires_at).getTime() };
      const claims = parseJwt(data.access_token);
      const userData: User = { id: (claims?.sub as string) || "", email: (claims?.email as string) || "", name: claims?.name as string, roles: (claims?.roles as string[]) || [], foundry_tier: (claims?.foundry_tier as User["foundry_tier"]) || null };
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
      const tokens = { accessToken: redirectTokens.accessToken, refreshToken: redirectTokens.refreshToken, expiresAt: redirectTokens.expiresAt.getTime() };
      const claims = parseJwt(redirectTokens.accessToken);
      const userData: User = { id: (claims?.sub as string) || "", email: (claims?.email as string) || "", name: claims?.name as string, roles: (claims?.roles as string[]) || [], foundry_tier: (claims?.foundry_tier as User["foundry_tier"]) || null };
      setStoredTokens(tokens);
      setStoredUser(userData);
      setUser(userData);
      scheduleRefresh(tokens.expiresAt);
    } finally {
      setIsLoading(false);
    }
  }, [scheduleRefresh]);

  const logout = useCallback(async (options?: { skipServerRevocation?: boolean }): Promise<void> => {
    let logoutUrl: string | null = null;
    const stored = getStoredTokens();
    try {
      if (stored?.accessToken && !options?.skipServerRevocation) {
        const response = await fetch(`${API_BASE_URL}/v1/auth/logout`, {
          method: "POST",
          headers: { "Content-Type": "application/json", Authorization: `Bearer ${stored.accessToken}` },
        }).catch(() => null);
        if (response?.ok) {
          try { const data = await response.json(); if (data?.logout_url) logoutUrl = data.logout_url; } catch { /* ignore */ }
        }
      }
    } finally {
      setUser(null);
      clearStorage();
      if (refreshTimerRef.current) { clearTimeout(refreshTimerRef.current); refreshTimerRef.current = null; }
      if (logoutUrl) {
        const returnUrl = encodeURIComponent(`${window.location.origin}/login`);
        window.location.href = `${logoutUrl}?return_url=${returnUrl}`;
      }
    }
  }, []);

  const value: AuthContextType = {
    user,
    isAuthenticated: !!user,
    isLoading,
    authMode: "local",
    authError,
    clearAuthError: () => setAuthError(null),
    login,
    register,
    loginWithOIDC: () => {
      if (typeof window !== "undefined") localStorage.setItem("auth_return_url", window.location.pathname);
      clearStorage();
      window.location.href = `${API_BASE_URL}/v1/auth/login`;
    },
    handleOAuthCallback,
    storeTokensFromRedirect,
    logout,
    refreshTokens,
    getAccessToken: () => getStoredTokens()?.accessToken || null,
    getIDPToken: () => null,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

// =============================================================================
// PUBLIC API — AuthProvider + Hooks
// =============================================================================

const januaConfig = {
  baseURL: JANUA_BASE_URL,
};

export function AuthProvider({ children }: { children: ReactNode }) {
  if (AUTH_MODE === "oidc") {
    return (
      <JanuaProvider config={januaConfig}>
        <OIDCAuthBridge>{children}</OIDCAuthBridge>
      </JanuaProvider>
    );
  }
  return <LocalAuthProvider>{children}</LocalAuthProvider>;
}

export function useAuth(): AuthContextType {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
}

export function useRequireAuth(): {
  isAuthenticated: boolean;
  isLoading: boolean;
  shouldRedirect: boolean;
} {
  const { isAuthenticated, isLoading } = useAuth();
  return { isAuthenticated, isLoading, shouldRedirect: !isLoading && !isAuthenticated };
}

export function useAccessToken(): string | null {
  const { getAccessToken, isAuthenticated } = useAuth();
  if (!isAuthenticated) return null;
  return getAccessToken();
}
