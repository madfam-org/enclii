/**
 * Server-side proxy for Switchyard provider and ops contract endpoints.
 */

export async function switchyardProxy(
  prefix: 'providers' | 'ops',
  path: string,
  options?: RequestInit & { userToken?: string }
) {
  const { userToken, ...fetchOptions } = options || {}
  const apiKey = process.env.SWITCHYARD_API_KEY
  const apiBase = process.env.NEXT_PUBLIC_API_URL || 'https://api.enclii.dev'
  const url = `${apiBase}/v1/${prefix}${path}`

  const doFetch = (token?: string) =>
    fetch(url, {
      ...fetchOptions,
      headers: {
        'Content-Type': 'application/json',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        ...fetchOptions?.headers,
      },
    })

  if (userToken) {
    const res = await doFetch(userToken)
    if (res.status !== 401 || !apiKey) return res
    return doFetch(apiKey)
  }

  return doFetch(apiKey)
}

export type OperatorRequest = {
  operation?: string
  dry_run?: boolean
  reason?: string
  scope?: Record<string, string>
  args?: Record<string, string>
}

export async function switchyardProviderCall(
  provider: string,
  action: string,
  body: OperatorRequest,
  userToken?: string
) {
  const res = await switchyardProxy('providers', `/${provider}/${action}`, {
    method: 'POST',
    body: JSON.stringify({ dry_run: true, ...body }),
    userToken,
  })
  const data = await res.json().catch(() => ({}))
  return { ok: res.ok, status: res.status, data }
}

export async function switchyardOpsCall(
  domain: string,
  action: string,
  body: OperatorRequest,
  userToken?: string
) {
  const res = await switchyardProxy('ops', `/${domain}/${action}`, {
    method: 'POST',
    body: JSON.stringify({ dry_run: true, ...body }),
    userToken,
  })
  const data = await res.json().catch(() => ({}))
  return { ok: res.ok, status: res.status, data }
}
