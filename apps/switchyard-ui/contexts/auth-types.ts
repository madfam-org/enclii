/**
 * Auth Types
 * Type definitions for the authentication context
 */

// =============================================================================
// USER TYPES
// =============================================================================

export interface User {
  id: string;
  email: string;
  name?: string;
  roles?: string[];
  avatarUrl?: string;
  /** Foundry tier from Janua JWT claims (after Dhanam purchase) */
  foundry_tier?: "community" | "essentials" | "pro" | "madfam" | "sovereign" | "ecosystem" | null;
}

// =============================================================================
// TOKEN TYPES
// =============================================================================

export interface RedirectTokens {
  accessToken: string;
  refreshToken: string;
  expiresAt: Date;
  tokenType: string;
  idpToken?: string;
  idpTokenExpiresAt?: Date;
}

// =============================================================================
// CONTEXT TYPES
// =============================================================================

export type AuthMode = "local" | "oidc";

export interface AuthContextType {
  // State
  user: User | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  authMode: AuthMode;
  authError: string | null;
  clearAuthError: () => void;

  // Local auth methods
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string, name: string) => Promise<void>;

  // OIDC methods
  loginWithOIDC: () => void;
  handleOAuthCallback: (code: string, state?: string) => Promise<void>;
  storeTokensFromRedirect: (tokens: RedirectTokens) => Promise<void>;

  // Common methods
  logout: (options?: { skipServerRevocation?: boolean }) => Promise<void>;
  refreshTokens: () => Promise<boolean>;

  // Token access
  getAccessToken: () => string | null;
  getIDPToken: () => string | null;
}
