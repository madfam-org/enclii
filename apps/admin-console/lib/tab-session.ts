/**
 * Per-tab estate session (two accounts live in two tabs at once).
 *
 * ## The problem this solves
 *
 * The console's `dispatch_auth` cookie and Janua's `janua_sso` cookie are both
 * per-BROWSER, not per-tab, so two tabs cannot naturally hold two identities:
 * switching one re-fronts the other. `sessionStorage` IS per-tab, so a tab that
 * wants to be a specific held account remembers that account's Janua session id
 * (`sid`) here and asserts it to Janua via the `X-Janua-Session` header.
 *
 * ## Where the sid comes from
 *
 * Janua sids are HttpOnly (unreadable by script) everywhere except one place:
 * `POST /switch-session` with `return_sid=true` hands the chosen sid back to the
 * landing page as a URL FRAGMENT (`#janua_sid=<sid>`). A fragment is never sent
 * to a server, so the sid leaks to no host log; and it is only ever a REFERENCE
 * that Janua re-checks against the browser's signed `janua_sessions` held-set and
 * a live `sessions` row (see the Janua authorize resolver), so it is not a bearer
 * and escalates nothing. This module reads that fragment, stores the sid per-tab,
 * and strips the fragment from the address bar.
 *
 * ## What the header buys
 *
 * On a Janua authorize/verify round trip, a tab carrying `X-Janua-Session`
 * resolves as ITS account rather than whatever `janua_sso` currently fronts — so
 * two tabs keep their own identities across re-auth. The header is additive: with
 * no per-tab sid, everything behaves exactly as before.
 */

/** The per-tab key. `sessionStorage`, so it is scoped to this tab and this origin. */
export const TAB_SID_KEY = 'janua_tab_sid'

/** The header Janua's authorize resolver reads (must match the Janua constant). */
export const TAB_SESSION_HEADER = 'X-Janua-Session'

/** The URL fragment key Janua's `switch-session?return_sid` lands on. */
export const TAB_SID_FRAGMENT = 'janua_sid'

function safeSessionStorage(): Storage | null {
  try {
    if (typeof window === 'undefined') return null
    return window.sessionStorage
  } catch {
    // Private mode / disabled storage: degrade to no per-tab sid.
    return null
  }
}

/** The sid this tab is pinned to, or null when it follows the shared cookie. */
export function getTabSid(): string | null {
  const store = safeSessionStorage()
  if (store === null) return null
  try {
    const value = store.getItem(TAB_SID_KEY)
    return value && value.trim() !== '' ? value : null
  } catch {
    return null
  }
}

/** Pin this tab to `sid` (or clear it when `sid` is null/empty). */
export function setTabSid(sid: string | null): void {
  const store = safeSessionStorage()
  if (store === null) return
  try {
    if (sid && sid.trim() !== '') {
      store.setItem(TAB_SID_KEY, sid.trim())
    } else {
      store.removeItem(TAB_SID_KEY)
    }
  } catch {
    // Best-effort; a tab that cannot persist a sid simply follows the cookie.
  }
}

/**
 * Extract the sid from a `#…janua_sid=<sid>…` fragment, or null.
 *
 * Accepts the fragment with or without a leading `#`, and tolerates other
 * fragment params around it. Only the opaque sid is returned; nothing else in
 * the fragment is trusted or forwarded.
 */
export function readSidFromFragment(hash: string | null | undefined): string | null {
  if (!hash) return null
  const raw = hash.startsWith('#') ? hash.slice(1) : hash
  if (raw === '') return null
  const params = new URLSearchParams(raw)
  const sid = params.get(TAB_SID_FRAGMENT)
  return sid && sid.trim() !== '' ? sid.trim() : null
}

/**
 * If the current URL carries `#janua_sid=<sid>`, pin this tab to it and strip the
 * fragment from the address bar (so a copied URL never carries the sid, and a
 * reload does not re-apply a stale one). Returns the sid it adopted, or null.
 *
 * Safe to call unconditionally on a landing page; a no-op when no fragment is
 * present or storage is unavailable.
 */
export function adoptSidFromLocation(): string | null {
  if (typeof window === 'undefined') return null
  let sid: string | null = null
  try {
    sid = readSidFromFragment(window.location.hash)
  } catch {
    return null
  }
  if (sid === null) return null
  setTabSid(sid)
  try {
    // Remove only the fragment; leave path + query intact.
    const { pathname, search } = window.location
    window.history.replaceState(null, '', `${pathname}${search}`)
  } catch {
    // If we cannot rewrite history, the sid is still stored; leaving the
    // fragment in the bar is cosmetic, not a leak (it was never sent to a server).
  }
  return sid
}

/**
 * Merge `X-Janua-Session` into a fetch `headers` init when this tab holds a sid.
 *
 * Additive and header-shaped, so it only ever rides same-origin/CORS fetch — a
 * custom header a cross-site form or navigation cannot set, which is a CSRF gain
 * over the ambient cookie. With no per-tab sid the headers are returned
 * unchanged, so callers behave exactly as before.
 */
export function withTabSessionHeader(
  headers: Record<string, string> = {}
): Record<string, string> {
  const sid = getTabSid()
  if (sid === null) return headers
  return { ...headers, [TAB_SESSION_HEADER]: sid }
}
