/** Browser session transport; identity and roles still come from Janua /auth/me. */
export const SESSION_CHANGED = 'enclii-session-changed';
export const TOKEN_STORAGE_KEY = 'enclii_tokens';

export interface StoredTokens {
  accessToken: string;
  refreshToken?: string;
  expiresAt: number;
  idpToken?: string;
}

export function getStoredTokens(): StoredTokens | null {
  if (typeof window === 'undefined') return null;
  try {
    const value = JSON.parse(localStorage.getItem(TOKEN_STORAGE_KEY) || 'null');
    if (!value || typeof value.accessToken !== 'string' || !value.accessToken ||
        typeof value.expiresAt !== 'number' || !Number.isFinite(value.expiresAt)) return null;
    return value;
  } catch { return null; }
}

function cookieAttributes(): string {
  // Match /api/auth/session, including its production parent-domain cookie.
  return `path=/; samesite=lax${process.env.NODE_ENV === 'production' ? '; secure; domain=.enclii.dev' : ''}`;
}

export function setStoredTokens(tokens: StoredTokens): void {
  localStorage.setItem(TOKEN_STORAGE_KEY, JSON.stringify(tokens));
  document.cookie = `enclii_auth=${tokens.accessToken}; ${cookieAttributes()}; max-age=${Math.max(0, Math.floor((tokens.expiresAt - Date.now()) / 1000))}`;
  window.dispatchEvent(new Event(SESSION_CHANGED));
}

export function clearStorage(): void {
  localStorage.removeItem(TOKEN_STORAGE_KEY);
  localStorage.removeItem('enclii_user');
  for (const name of ['enclii_auth', 'enclii_user_email']) {
    document.cookie = `${name}=; ${cookieAttributes()}; max-age=0`;
    // Remove legacy host-only cookies too.
    document.cookie = `${name}=; path=/; samesite=lax; max-age=0`;
  }
  window.dispatchEvent(new Event(SESSION_CHANGED));
}
