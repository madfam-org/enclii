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
import type { AuthMode, AuthContextType, User, RedirectTokens, LoginWithOIDCOptions } from "./auth-types";
import { getStoredTokens, setStoredTokens, clearStorage, captureSession, sameSession, withSessionLock, canRestoreCookieSession, SESSION_CHANGED, TOKEN_STORAGE_KEY, type StoredTokens } from "@/lib/auth-session";
import { JANUA_BASE_URL, OAUTH_CLIENT_ID } from "@/lib/constants";
import { apiFetchResponse, apiPublicFetchResponse, attemptTokenRefresh } from "@/lib/api";

// =============================================================================
// CONFIGURATION
// =============================================================================

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:4200";
const AUTH_MODE = (process.env.NEXT_PUBLIC_AUTH_MODE || "local") as AuthMode;

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
  const sessionRef = useRef<StoredTokens | null>(getStoredTokens());

  const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const checkVersion = useRef(0);

  const checkAuth = useCallback(async () => {
    const version = ++checkVersion.current;
    try {
      let tokens = await captureSession();
      if (!tokens && canRestoreCookieSession()) {
        const cookieToken = document.cookie.split('; ').find((r) => r.startsWith('enclii_auth='))?.slice('enclii_auth='.length);
        // Decode exp only for scheduling. Identity is always verified below.
        const exp = cookieToken ? parseJwt(cookieToken)?.exp : null;
        if (cookieToken && typeof exp === 'number' && Number.isFinite(exp)) {
          tokens = { accessToken: cookieToken, expiresAt: exp * 1000 };
          tokens = await setStoredTokens(tokens);
        }
      }
      if (!tokens) { tokenRef.current = null; setUser(null); return; }
      const checkedSession = tokens;
      if (tokens.expiresAt <= Date.now()) {
        const refreshed = await attemptTokenRefresh(checkedSession);
        if (version !== checkVersion.current || !sameSession(checkedSession, getStoredTokens())) return;
        if (!refreshed) {
          tokenRef.current = null;
          setUser(null);
          setAuthError('Session expired. Please sign in again.');
          return;
        }
        tokens = getStoredTokens();
      }
      if (!tokens || !sameSession(checkedSession, tokens) || version !== checkVersion.current) return;
      let checkedToken = tokens.accessToken;
      const verify = (token: string) => fetch(`${JANUA_BASE_URL}/api/v1/auth/me`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      let response = await verify(checkedToken);
      const refreshedAfterRejection = response.status === 401 && await attemptTokenRefresh(checkedSession);
      if (refreshedAfterRejection) {
        const refreshed = getStoredTokens();
        if (!refreshed || !sameSession(checkedSession, refreshed) || version !== checkVersion.current) return;
        checkedToken = refreshed.accessToken;
        response = await verify(checkedToken);
      }
      if (version !== checkVersion.current || !sameSession(checkedSession, getStoredTokens()) || getStoredTokens()?.accessToken !== checkedToken) return;
      if (response.ok) {
        const userData = await response.json();
        if (version !== checkVersion.current || !sameSession(checkedSession, getStoredTokens()) || getStoredTokens()?.accessToken !== checkedToken) return;
        sessionRef.current = getStoredTokens();
        tokenRef.current = checkedToken;
        setUser({
          id: userData.id || '', email: userData.email || '',
          name: userData.name || userData.display_name,
          roles: extractRoles(userData),
          foundry_tier: (userData.user_metadata?.foundry_tier as User['foundry_tier']) || null,
        });
        setAuthError(getStoredTokens()?.refreshAttempted ? 'Session refresh could not be confirmed. Please sign in again.' : null);
      } else if (response.status === 401 || response.status === 403) {
        tokenRef.current = null;
        setUser(null);
        // A rejected access token plus an unavailable issuer is recoverable.
        // Retain its refresh credential, but expose no authenticated user.
        if (refreshedAfterRejection || !getStoredTokens()?.refreshToken) await clearStorage(checkedSession);
        setAuthError('Session expired. Please sign in again.');
      } else {
        setAuthError('Authentication service unavailable. Please retry.');
      }
    } catch {
      if (version === checkVersion.current) {
        tokenRef.current = null;
        setUser(null);
        setAuthError('Authentication check failed. Please retry.');
      }
    } finally { if (version === checkVersion.current) setIsLoading(false); }
  }, []);

  const refreshTokens = useCallback(async (owned: StoredTokens | null = getStoredTokens()) => {
    if (!owned || !sameSession(owned, getStoredTokens())) return false;
    if (owned.expiresAt <= Date.now()) {
      tokenRef.current = null;
      setUser(null);
    }
    const refreshed = await attemptTokenRefresh(owned);
    if (!sameSession(owned, getStoredTokens())) return false;
    if (refreshed) await checkAuth();
    else if (getStoredTokens()?.refreshAttempted) setAuthError('Session refresh could not be confirmed. Please sign in again.');
    if (!refreshed && (getStoredTokens()?.expiresAt ?? 0) <= Date.now()) {
      tokenRef.current = null;
      setUser(null);
      setAuthError('Session expired. Please sign in again.');
    }
    return refreshed;
  }, [checkAuth]);

  useEffect(() => {
    let active = true;
    const schedule = () => {
      if (refreshTimerRef.current) clearTimeout(refreshTimerRef.current);
      const tokens = getStoredTokens();
      const changed = !sameSession(sessionRef.current, tokens);
      if (changed) { ++checkVersion.current; sessionRef.current = tokens; setUser(null); }
      tokenRef.current = tokens && tokens.expiresAt > Date.now() ? tokens.accessToken : null;
      if (!tokens) {
        ++checkVersion.current; setUser(null); setIsLoading(false);
        if (changed) setAuthError('Session unavailable. Please sign in again.');
        return;
      }
      if (tokens.expiresAt <= Date.now()) {
        setUser(null);
        setAuthError('Session expired. Please sign in again.');
      }
      // Retry transient failures at a bounded cadence, then fail closed at expiry.
      const delay = tokens.refreshToken && !tokens.refreshAttempted ? Math.max(1000, tokens.expiresAt - Date.now() - 60_000)
        : Math.max(0, tokens.expiresAt - Date.now());
      refreshTimerRef.current = setTimeout(async () => {
        const refreshed = await refreshTokens(tokens);
        if (active && !refreshed && sameSession(tokens, getStoredTokens())) {
          const latest = getStoredTokens();
          if (latest && latest.expiresAt > Date.now()) {
            refreshTimerRef.current = setTimeout(schedule, Math.min(30_000, latest.expiresAt - Date.now()));
          }
        }
      }, Math.min(delay, 2_147_483_647));
      return changed;
    };
    const onStorage = (event: StorageEvent) => {
      if (event.key === TOKEN_STORAGE_KEY || event.key === null) { schedule(); void checkAuth(); }
    };
    const onSessionChange = () => { if (schedule()) void checkAuth(); };
    window.addEventListener(SESSION_CHANGED, onSessionChange);
    window.addEventListener('storage', onStorage);
    void checkAuth().then(() => { if (active) schedule(); });
    return () => {
      active = false;
      ++checkVersion.current;
      if (refreshTimerRef.current) clearTimeout(refreshTimerRef.current);
      window.removeEventListener(SESSION_CHANGED, onSessionChange);
      window.removeEventListener('storage', onStorage);
    };
  }, [checkAuth, refreshTokens]);

  const login = useCallback(async (options?: LoginWithOIDCOptions) => {
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

    // Optional OIDC `prompt` (account switching). Appended only when supplied,
    // so the default sign-in keeps sending NO prompt (silent session reuse).
    // Janua honors `login` (force re-auth) and `select_account` (chooser); see
    // janua #623. Mirrors the enclii admin-console (DISPATCH) switching model.
    if (options?.prompt) {
      params.set("prompt", options.prompt);
    }

    window.location.href = `${JANUA_BASE_URL}/api/v1/oauth/authorize?${params.toString()}`;
  }, []);

  const logout = useCallback(async () => {
    const owned = getStoredTokens();
    const accessToken = owned?.accessToken || tokenRef.current;
    ++checkVersion.current;
    tokenRef.current = null;
    setUser(null);
    // Local denial is immediate; storage and network cleanup settle independently.
    const cleanup = clearStorage(owned).catch(() => false);
    const revoke = accessToken ? fetch(`${JANUA_BASE_URL}/api/v1/auth/logout`, {
      method: "POST", headers: { Authorization: `Bearer ${accessToken}` },
    }).catch(() => {}) : Promise.resolve();
    await Promise.allSettled([cleanup, revoke]);
    await withSessionLock(async () => {
      // A newer login wins over this logout's delayed cleanup/redirect.
      const replacement = getStoredTokens();
      if (replacement && !sameSession(owned, replacement)) return;
      await fetch("/api/auth/session", { method: "DELETE" }).catch(() => {});
      window.location.href = "/login";
    }).catch(() => {});
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
    refreshTokens,
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

  const refreshTokens = useCallback(async (stored: StoredTokens | null = getStoredTokens()): Promise<boolean> => {
    if (isRefreshingRef.current) return false;
    if (!sameSession(stored, getStoredTokens())) return false;
    if (!stored?.refreshToken) return false;
    isRefreshingRef.current = true;
    try {
      const ok = await attemptTokenRefresh(stored);
      if (!sameSession(stored, getStoredTokens())) return false;
      if (!ok) throw new Error("Token refresh failed");
      const newTokens = getStoredTokens();
      if (newTokens) scheduleRefresh(newTokens.expiresAt);
      return true;
    } catch {
      if (sameSession(stored, getStoredTokens())) setAuthError("Session expired. Please log in again.");
      return false;
    } finally {
      isRefreshingRef.current = false;
    }
  }, []);

  const scheduleRefresh = useCallback((expiresAt: number) => {
    if (refreshTimerRef.current) clearTimeout(refreshTimerRef.current);
    const refreshIn = expiresAt - Date.now() - 5 * 60 * 1000;
    if (refreshIn > 0) {
      const owned = getStoredTokens();
      refreshTimerRef.current = setTimeout(() => { refreshTokens(owned); }, refreshIn);
    }
  }, [refreshTokens]);

  useEffect(() => {
    const init = async () => {
      try {
        if (typeof window !== "undefined" && window.location.pathname.startsWith("/auth/callback")) return;
        const storedTokens = await captureSession();
        const storedUser = getStoredUser();
        if (storedTokens && storedUser) {
          if (Date.now() < storedTokens.expiresAt) {
            setUser(storedUser);
            scheduleRefresh(storedTokens.expiresAt);
          } else if (storedTokens.refreshToken) {
            const refreshed = await refreshTokens();
            if (refreshed && sameSession(storedTokens, getStoredTokens())) setUser(storedUser);
            else await clearStorage(storedTokens);
          } else {
            await clearStorage(storedTokens);
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
      const published = await setStoredTokens(tokens);
      if (!sameSession(published, getStoredTokens())) return;
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
      const published = await setStoredTokens(tokens);
      if (!sameSession(published, getStoredTokens())) return;
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
      const published = await setStoredTokens(tokens);
      if (!sameSession(published, getStoredTokens())) return;
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
      const published = await setStoredTokens(tokens);
      if (!sameSession(published, getStoredTokens())) return;
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
    setUser(null);
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
      if (!sameSession(stored, getStoredTokens())) return;
      setUser(null);
      await clearStorage(stored);
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
    // Local/bootstrap mode has no OIDC prompt concept (no estate SSO session to
    // switch). The `LoginWithOIDCOptions` arg from the interface is simply not
    // read here; local login has no chooser.
    loginWithOIDC: async () => {
      if (typeof window !== "undefined") localStorage.setItem("auth_return_url", window.location.pathname);
      await clearStorage();
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
