/**
 * Server-side proxy for admin API calls to Switchyard.
 *
 * Auth strategy (resilient fallback):
 *   1. Try user's JWT (from dispatch_auth cookie) — works for logged-in users
 *   2. If JWT gets 401 (expired/invalid), retry with SWITCHYARD_API_KEY (service token)
 *   3. If neither is available, request goes unauthenticated (Switchyard will 401)
 */
export async function adminProxy(
  path: string,
  options?: RequestInit & { userToken?: string }
) {
  const { userToken, ...fetchOptions } = options || {}
  const apiKey = process.env.SWITCHYARD_API_KEY
  const apiBase = process.env.NEXT_PUBLIC_API_URL || 'https://api.enclii.dev'
  const url = `${apiBase}/v1/admin${path}`

  const doFetch = (token?: string) =>
    fetch(url, {
      ...fetchOptions,
      headers: {
        'Content-Type': 'application/json',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        ...fetchOptions?.headers,
      },
    })

  // Try user JWT first
  if (userToken) {
    const res = await doFetch(userToken)
    // If user JWT succeeded or there's no service key to fall back to, return as-is
    if (res.status !== 401 || !apiKey) return res
    // User JWT was rejected — fall back to service key
    return doFetch(apiKey)
  }

  // No user JWT — use service key (or unauthenticated)
  return doFetch(apiKey)
}
