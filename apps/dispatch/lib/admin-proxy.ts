/**
 * Server-side proxy for admin API calls to Switchyard.
 * Authenticates with a service API key instead of user tokens.
 */
export async function adminProxy(path: string, options?: RequestInit) {
  const apiKey = process.env.SWITCHYARD_API_KEY
  const apiBase = process.env.NEXT_PUBLIC_API_URL || 'https://api.enclii.dev'

  const res = await fetch(`${apiBase}/v1/admin${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...(apiKey ? { Authorization: `Bearer ${apiKey}` } : {}),
      ...options?.headers,
    },
  })

  return res
}
