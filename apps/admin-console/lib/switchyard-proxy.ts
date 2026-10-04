/**
 * Server-side proxy for Switchyard provider and ops contract endpoints.
 */

import { callerProxy } from './caller-proxy'

export async function switchyardProxy(
  prefix: 'providers' | 'ops',
  path: string,
  options?: RequestInit & { userToken?: string }
) {
  return callerProxy(`/${prefix}${path}`, options)
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
