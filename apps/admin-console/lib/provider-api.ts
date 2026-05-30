import type { OperatorResponse, ProviderCatalogResponse } from '@/types/providers'

async function providerFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`/api/providers${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error(body.error || body.summary || `Provider API error: ${res.status}`)
  }
  return body as T
}

async function opsFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`/api/ops${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error(body.error || body.summary || `Ops API error: ${res.status}`)
  }
  return body as T
}

async function adminFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`/api/admin${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error(body.error || `Admin API error: ${res.status}`)
  }
  return body as T
}

export const providerApi = {
  catalog: () => adminFetch<ProviderCatalogResponse>('/providers/catalog'),

  operation: (provider: string, action: string, body: Record<string, unknown>) =>
    providerFetch<OperatorResponse>(`/${provider}/${action}`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  resendDomains: (tenant?: string) =>
    providerFetch<OperatorResponse>('/resend/domains', {
      method: 'POST',
      body: JSON.stringify({
        dry_run: true,
        scope: tenant ? { tenant } : undefined,
      }),
    }),

  cloudflareZones: () =>
    providerFetch<OperatorResponse>('/cloudflare/zones', {
      method: 'POST',
      body: JSON.stringify({ dry_run: true }),
    }),
}

export const opsApi = {
  operation: (domain: string, action: string, body: Record<string, unknown>) =>
    opsFetch<OperatorResponse>(`/${domain}/${action}`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  secretsExternal: (namespace = 'enclii') =>
    opsFetch<OperatorResponse>('/secrets/external', {
      method: 'POST',
      body: JSON.stringify({ dry_run: true, scope: { namespace } }),
    }),

  secretsVault: () =>
    opsFetch<OperatorResponse>('/secrets/vault', {
      method: 'POST',
      body: JSON.stringify({ dry_run: true }),
    }),

  secretsSyncSweep: (reason: string, namespace = 'enclii') =>
    opsFetch<OperatorResponse>('/secrets/sync-sweep', {
      method: 'POST',
      body: JSON.stringify({
        dry_run: false,
        reason,
        scope: { namespace },
      }),
    }),

  githubRuns: (repo: string) =>
    providerFetch<OperatorResponse>('/github/runs', {
      method: 'POST',
      body: JSON.stringify({ dry_run: true, args: { target: repo } }),
    }),

  porkbunDomains: () =>
    providerFetch<OperatorResponse>('/porkbun/domains', {
      method: 'POST',
      body: JSON.stringify({ dry_run: true }),
    }),
}
