/** Browser session transport; identity and roles still come from Janua /auth/me. */
export const SESSION_CHANGED = 'enclii-session-changed';
export const TOKEN_STORAGE_KEY = 'enclii_tokens';
const SESSION_LOCK = 'enclii-token-refresh';
const quarantined = new Set<string>();
let cookieRestoreBlocked = false;

export interface StoredTokens {
  accessToken: string;
  refreshToken?: string;
  expiresAt: number;
  idpToken?: string;
  /** Browser login generation, not an identity or authorization claim. */
  sessionId?: string;
  /** Persisted before spending a grant; only a published replacement resets it. */
  refreshAttempted?: boolean;
}

function readTokens(): StoredTokens | null {
  if (typeof window === 'undefined') return null;
  try {
    const value = JSON.parse(localStorage.getItem(TOKEN_STORAGE_KEY) || 'null');
    if (!value || typeof value.accessToken !== 'string' || !value.accessToken ||
        typeof value.expiresAt !== 'number' || !Number.isFinite(value.expiresAt)) return null;
    return value;
  } catch { return null; }
}

function key(tokens: StoredTokens): string {
  return tokens.sessionId || JSON.stringify(tokens);
}

export function getStoredTokens(): StoredTokens | null {
  const tokens = readTokens();
  return tokens && !quarantined.has(key(tokens)) ? tokens : null;
}

export function canRestoreCookieSession(): boolean { return !cookieRestoreBlocked && quarantined.size === 0; }

export function sameSession(a: StoredTokens | null, b: StoredTokens | null): boolean {
  if (!a || !b) return a === b;
  if (a.sessionId || b.sessionId) return !!a.sessionId && a.sessionId === b.sessionId;
  // Legacy credentials have no durable generation; only an exact match is safe.
  return key(a) === key(b);
}

export async function withSessionLock<T>(run: () => T | Promise<T>): Promise<T> {
  return typeof navigator !== 'undefined' && navigator.locks
    ? await navigator.locks.request(SESSION_LOCK, async () => await run())
    : await run();
}

function cookieAttributes(): string {
  return `path=/; samesite=lax${process.env.NODE_ENV === 'production' ? '; secure; domain=.enclii.dev' : ''}`;
}

/** Block this generation immediately, even when persistent storage is unavailable. */
export function invalidateSession(tokens: StoredTokens | null): void {
  cookieRestoreBlocked = true;
  if (tokens) quarantined.add(key(tokens));
  window.dispatchEvent(new Event(SESSION_CHANGED));
}

/** Caller holds SESSION_LOCK. Cleanup steps must not prevent one another. */
export function clearStoredSession(expected: StoredTokens | null): boolean {
  const current = readTokens();
  if (current && !sameSession(expected, current)) return false;
  if (expected) quarantined.add(key(expected));
  if (current) quarantined.add(key(current));
  // If all browser persistence fails, only this realm can be fenced. A durable
  // denial across reloads requires storage or cookie mutation to succeed.
  for (const name of [TOKEN_STORAGE_KEY, 'enclii_user']) {
    try { localStorage.removeItem(name); } catch { /* try a tombstone below */ }
    try {
      if (localStorage.getItem(name) !== null) localStorage.setItem(name, 'null');
    } catch { /* memory quarantine still denies access if all mutations fail */ }
  }
  for (const name of ['enclii_auth', 'enclii_user_email']) {
    for (const attributes of [cookieAttributes(), 'path=/; samesite=lax']) {
      try { document.cookie = `${name}=; ${attributes}; max-age=0`; } catch { /* server cleanup is independent */ }
    }
  }
  window.dispatchEvent(new Event(SESSION_CHANGED));
  return true;
}

export function clearStorage(expected: StoredTokens | null = getStoredTokens()): Promise<boolean> {
  invalidateSession(expected);
  return withSessionLock(() => clearStoredSession(expected));
}

/** Caller holds SESSION_LOCK. Never reuse a refresh credential after lost publication. */
export function persistTokens(tokens: StoredTokens, previous: StoredTokens | null): void {
  const value = JSON.stringify(tokens);
  try {
    localStorage.setItem(TOKEN_STORAGE_KEY, value);
    if (localStorage.getItem(TOKEN_STORAGE_KEY) !== value) throw new Error('Session storage unavailable');
    document.cookie = `enclii_auth=${tokens.accessToken}; ${cookieAttributes()}; max-age=${Math.max(0, Math.floor((tokens.expiresAt - Date.now()) / 1000))}`;
  } catch {
    if (previous) quarantined.add(key(previous));
    quarantined.add(key(tokens));
    const current = readTokens();
    if (sameSession(current, previous) || sameSession(current, tokens)) clearStoredSession(current);
    window.dispatchEvent(new Event(SESSION_CHANGED));
    throw new Error('Session storage unavailable. Please sign in again.');
  }
  window.dispatchEvent(new Event(SESSION_CHANGED));
}

/** New sign-ins always create a new generation, even for the same account. */
export function setStoredTokens(tokens: StoredTokens, establishCookie?: () => Promise<void>): Promise<StoredTokens> {
  return withSessionLock(async () => {
    const next = { ...tokens, sessionId: Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) => byte.toString(16).padStart(2, '0')).join('') };
    if (establishCookie) await establishCookie();
    persistTokens(next, getStoredTokens());
    return next;
  });
}

/** Migrate a legacy session once, before requests can depend on rotation identity. */
export function captureSession(): Promise<StoredTokens | null> {
  const initial = getStoredTokens();
  return withSessionLock(() => {
    const current = getStoredTokens();
    if (!sameSession(initial, current)) throw new Error('Session changed. Please retry.');
    if (!current || current.sessionId) return current;
    const next = { ...current, sessionId: Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) => byte.toString(16).padStart(2, '0')).join('') };
    persistTokens(next, current);
    return next;
  });
}
