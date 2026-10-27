/**
 * OAuth `state` binding for the browser PKCE sign-in.
 *
 * Every sign-in attempt gets a fresh random `state` (256 bits from
 * crypto.getRandomValues). It is stored in this tab's sessionStorage next to the
 * PKCE code_verifier and sent on the authorize request. The callback exchanges
 * the authorization code only when the returned `state` equals the stored value.
 * The stored value is single-use (removed on the first read) and short-lived
 * (OAUTH_STATE_TTL_MS).
 */

export const OAUTH_STATE_KEY = "enclii_oauth_state";
export const OAUTH_STATE_TTL_MS = 10 * 60 * 1000;

interface StoredState {
  v: string;
  t: number;
}

function base64UrlEncode(bytes: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

/** A fresh, URL-safe random `state` value (32 random bytes). */
export function generateOAuthState(): string {
  const bytes = new Uint8Array(32);
  crypto.getRandomValues(bytes);
  return base64UrlEncode(bytes);
}

/** Store `state` for this tab's pending sign-in, replacing any earlier attempt. */
export function storeOAuthState(state: string, now: number = Date.now()): void {
  const record: StoredState = { v: state, t: now };
  sessionStorage.setItem(OAUTH_STATE_KEY, JSON.stringify(record));
}

/** Drop any pending `state` for this tab. */
export function clearOAuthState(): void {
  sessionStorage.removeItem(OAUTH_STATE_KEY);
}

/** Compare two strings without short-circuiting on the first differing character. */
export function timingSafeEqual(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return diff === 0;
}

/**
 * Read and remove the stored `state`, then check it against the value returned
 * on the callback. Returns false when either side is missing, the stored value
 * is older than OAUTH_STATE_TTL_MS, or the values differ. The stored value is
 * removed in every case, so a `state` can be accepted at most once.
 */
export function consumeOAuthState(returned: string | null | undefined, now: number = Date.now()): boolean {
  const raw = sessionStorage.getItem(OAUTH_STATE_KEY);
  sessionStorage.removeItem(OAUTH_STATE_KEY);
  if (!raw || !returned) return false;

  let record: StoredState;
  try {
    record = JSON.parse(raw);
  } catch {
    return false;
  }
  if (!record || typeof record.v !== "string" || typeof record.t !== "number") return false;
  if (!record.v) return false;

  const age = now - record.t;
  if (age < 0 || age > OAUTH_STATE_TTL_MS) return false;

  return timingSafeEqual(record.v, returned);
}

/**
 * Accept a post-login return path only when it is a same-origin relative path
 * (`/projects?x=1`). Anything else — absolute or protocol-relative URLs,
 * backslashes, control characters, or the callback route itself — falls back
 * to `/`.
 */
export function safeReturnPath(raw: string | null | undefined, origin: string): string {
  if (!raw || typeof raw !== "string") return "/";
  if (!raw.startsWith("/") || raw.startsWith("//")) return "/";
  if (/[\\\u0000-\u001f\u007f]/.test(raw)) return "/";

  let url: URL;
  try {
    url = new URL(raw, origin);
  } catch {
    return "/";
  }
  if (url.origin !== origin) return "/";
  if (url.pathname.startsWith("/auth/callback")) return "/";
  return `${url.pathname}${url.search}${url.hash}`;
}
