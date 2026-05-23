"use client";

/**
 * Authentication Context for Enclii Switchyard UI
 *
 * Dual auth mode support:
 * - Local: Email/password directly to Switchyard API (bootstrap mode)
 * - OIDC: Direct PKCE flow with Janua SSO (production)
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
import type { AuthMode, AuthContextType, User, RedirectTokens } from "./auth-types";
import { apiFetchResponse, apiPublicFetchResponse, attemptTokenRefresh } from "@/lib/api";

// =============================================================================
// CONFIGURATION
// =============================================================================

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:4200";
const AUTH_MODE = (process.env.NEXT_PUBLIC_AUTH_MODE || "local") as AuthMode;
const JANUA_BASE_URL = process.env.NEXT_PUBLIC_JANUA_URL || "https://auth.madfam.io";
const OAUTH_CLIENT_ID = process.env.NEXT_PUBLIC_OAUTH_CLIENT_ID || "jnc_RqeHy54KYGjVr8yQiBeUncMhnQFhS2NA";

const STORAGE_KEYS = {
  TOKENS: "enclii_tokens",
  USER: "enclii_user",
  COOKIE: "enclii_auth",
} as const;

// =============================================================================
// PKCE HELPERS
// =============================================================================

function generateCodeVerifier(): string {
  const array = new Uint8Array(32);
  crypto.getRandomValues(array);
  return Array.from(array, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

async function generateCodeChallenge(verifier: string): Promise<string> {
  const encoder = new TextEncoder();
  const data = encoder.encode(verifier);
  const digest = await crypto.subtle.digest("SHA-256", data);
  const base64 = btoa(String.fromCharCode(...new Uint8Array(digest)));
  return base64.replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

// =============================================================================
// STORAGE HELPERS
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
  document.cookie = `enclii_user_email=; path=/; secure; samesite=lax; max-age=0`;
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

/**
 * Normalize admin-role information across the JWT claim-shape variations
 * Janua issuers can produce. Without this, an admin user can show as
 * "Personal Account" because their JWT carries `is_admin: true` (or
 * `role: "admin"` singular) instead of the canonical `roles: ["admin"]`
 * array — and the rest of the UI keys off `roles?.includes("admin")`.
 *
 * Identity is NEVER hardcoded here. The bootstrap admin email is owned
 * by the platform's ramp-up script (Janua's `ADMIN_BOOTSTRAP_PASSWORD`
 * provisioning, see janua/CLAUDE.md → Admin Bootstrap), and the JWT
 * issuer is responsible for translating that user's stored role/admin
 * flag into one of the claim shapes accepted below.
 *
 * Sources we accept (any one signal admits the user as admin):
 *   - `roles: string[]` — preferred shape; values "admin" or "superadmin"
 *   - `role: string` — singular legacy claim
 *   - `is_admin: true` / `is_superadmin: true` — boolean flags
 */
function extractRoles(claims: Record<string, unknown> | null): string[] {
  if (!claims) return [];
  const roles = new Set<string>();
  if (Array.isArray(claims.roles)) {
    for (const r of claims.roles) if (typeof r === "string") roles.add(r);
  }
  if (typeof claims.role === "string") roles.add(claims.role);
  if (claims.is_admin === true || claims.is_superadmin === true) roles.add("admin");
  // Map superadmin → admin so existing `.includes("admin")` checks engage.
  if (roles.has("superadmin") && !roles.has("admin")) roles.add("admin");
  return Array.from(roles);
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
// OIDC MODE PROVIDER — Direct PKCE flow (no SDK dependency)
// =============================================================================

function OIDCAuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [authError, setAuthError] = useState<string | null>(null);
  const tokenRef = useRef<string | null>(null);

  // Check auth on mount — single /auth/me call with Bearer token (not cookie-based polling)
  useEffect(() => {
    checkAuth();
  }, []);

  const checkAuth = useCallback(async () => {
    try {
      // Read token from cookie (set by server-side /api/auth/session route)
      const cookieToken = document.cookie
        .split("; ")
        .find((r) => r.startsWith("enclii_auth="))
        ?.split("=")[1];

      if (!cookieToken) {
        setIsLoading(false);
        return;
      }

      // Verify token with Janua using Bearer header (not cookie-based session)
      const response = await fetch(`${JANUA_BASE_URL}/api/v1/auth/me`, {
        headers: { Authorization: `Bearer ${cookieToken}` },
      });

      if (response.ok) {
        const userData = await response.json();
        const userRoles: string[] = userData.roles || [];
        if (userData.is_admin && !userRoles.includes("admin")) {
          userRoles.push("admin");
        }

        tokenRef.current = cookieToken;

        // Sync token to localStorage so lib/api.ts can use it for API calls
        if (!localStorage.getItem(STORAGE_KEYS.TOKENS)) {
          localStorage.setItem(STORAGE_KEYS.TOKENS, JSON.stringify({
            accessToken: cookieToken,
            refreshToken: null,
            expiresAt: Date.now() + 24 * 60 * 60 * 1000, // Match cookie maxAge (24h)
          }));
        }

        setUser({
          id: userData.id || "",
          email: userData.email || "",
          name: userData.name || userData.display_name,
          roles: userRoles,
          foundry_tier: (userData.user_metadata?.foundry_tier as User["foundry_tier"]) || null,
        });
      } else {
        // Token invalid — clear cookies via server-side route
        await fetch("/api/auth/session", { method: "DELETE" }).catch(() => {});
        clearStorage();
      }
    } catch (err) {
      console.error("Auth check failed:", err);
      setAuthError("Authentication check failed");
    } finally {
      setIsLoading(false);
    }
  }, []);

  const login = useCallback(async () => {
    // Generate PKCE parameters
    const codeVerifier = generateCodeVerifier();
    const codeChallenge = await generateCodeChallenge(codeVerifier);

    // Store verifier for callback
    sessionStorage.setItem("enclii_code_verifier", codeVerifier);

    // Store return URL
    if (typeof window !== "undefined") {
      localStorage.setItem("auth_return_url", window.location.pathname);
    }

    // Redirect to Janua OAuth authorize endpoint with PKCE
    const params = new URLSearchParams({
      response_type: "code",
      client_id: OAUTH_CLIENT_ID,
      redirect_uri: `${window.location.origin}/auth/callback`,
      scope: "openid profile email",
      code_challenge: codeChallenge,
      code_challenge_method: "S256",
    });
    window.location.href = `${JANUA_BASE_URL}/api/v1/oauth/authorize?${params.toString()}`;
  }, []);

  const logout = useCallback(async () => {
    try {
      const cookieToken = tokenRef.current || document.cookie
        .split("; ")
        .find((r) => r.startsWith("enclii_auth="))
        ?.split("=")[1];

      if (cookieToken) {
        // Notify Janua of logout
        await fetch(`${JANUA_BASE_URL}/api/v1/auth/logout`, {
          method: "POST",
          headers: { Authorization: `Bearer ${cookieToken}` },
        }).catch(() => {});
      }
    } finally {
      tokenRef.current = null;
      setUser(null);
      clearStorage();
      // Clear cookies server-side
      await fetch("/api/auth/session", { method: "DELETE" }).catch(() => {});
      // Redirect to login
      window.location.href = "/login";
    }
  }, []);

  const value: AuthContextType = {
    user,
    isAuthenticated: !!user,
    isLoading,
    authMode: "oidc",
    authError,
    clearAuthError: () => setAuthError(null),
    // Local auth methods are no-ops in OIDC mode
    login: async () => { throw new Error("Local login not available in OIDC mode"); },
    register: async () => { throw new Error("Registration not available in OIDC mode"); },
    // OIDC methods
    loginWithOIDC: login,
    handleOAuthCallback: async () => { /* Handled by callback page */ },
    storeTokensFromRedirect: async () => { /* Not used in PKCE flow */ },
    logout,
    refreshTokens: async () => true,
    getAccessToken: () => tokenRef.current,
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
      const ok = await attemptTokenRefresh();
      if (!ok) throw new Error("Token refresh failed");
      const newTokens = getStoredTokens();
      if (newTokens) scheduleRefresh(newTokens.expiresAt);
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

  const loginLocal = useCallback(async (email: string, password: string): Promise<void> => {
    setIsLoading(true);
    setAuthError(null);
    try {
      const response = await apiPublicFetchResponse("/v1/auth/login", {
        method: "POST",
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
      const response = await apiPublicFetchResponse("/v1/auth/register", {
        method: "POST",
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
      const response = await apiPublicFetchResponse(`/v1/auth/callback?${params.toString()}`, {
        method: "GET",
      });
      if (!response.ok) throw new Error(await parseErrorResponse(response, "OAuth callback failed"));
      const data = await response.json();
      const tokens = { accessToken: data.access_token, refreshToken: data.refresh_token, expiresAt: new Date(data.expires_at).getTime() };
      const claims = parseJwt(data.access_token);
      const userData: User = { id: (claims?.sub as string) || "", email: (claims?.email as string) || "", name: claims?.name as string, roles: extractRoles(claims), foundry_tier: (claims?.foundry_tier as User["foundry_tier"]) || null };
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
      const userData: User = { id: (claims?.sub as string) || "", email: (claims?.email as string) || "", name: claims?.name as string, roles: extractRoles(claims), foundry_tier: (claims?.foundry_tier as User["foundry_tier"]) || null };
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
        const response = await apiFetchResponse("/v1/auth/logout", {
          method: "POST",
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
    login: loginLocal,
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

export function AuthProvider({ children }: { children: ReactNode }) {
  if (AUTH_MODE === "oidc") {
    return <OIDCAuthProvider>{children}</OIDCAuthProvider>;
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
