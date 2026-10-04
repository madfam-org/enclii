/** Forward console requests as the authenticated caller, never as a service principal. */
export async function callerProxy(
  path: string,
  options?: RequestInit & { userToken?: string }
): Promise<Response> {
  const { userToken, ...fetchOptions } = options || {}
  if (!userToken) {
    return new Response(JSON.stringify({ error: 'Unauthorized' }), {
      status: 401,
      headers: { 'Content-Type': 'application/json' },
    })
  }

  const headers = new Headers(fetchOptions.headers)
  headers.set('Content-Type', 'application/json')
  // Set last so supplied headers cannot replace the caller's identity.
  headers.set('Authorization', `Bearer ${userToken}`)
  const apiBase = process.env.NEXT_PUBLIC_API_URL || 'https://api.enclii.dev'
  return fetch(`${apiBase}/v1${path}`, { ...fetchOptions, headers, cache: 'no-store' })
}
