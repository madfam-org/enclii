/** @jest-environment node */
import { GET as topology } from '@/app/api/topology/route'
import { GET as applications } from '@/app/api/argocd/applications/route'
import { GET as volumes } from '@/app/api/storage/volumes/route'
import { GET as policies } from '@/app/api/network-policies/route'
import { switchyardOpsCall } from '@/lib/switchyard-proxy'
import { cookies } from 'next/headers'

jest.mock('next/headers', () => ({ cookies: jest.fn() }))
jest.mock('@/lib/switchyard-proxy', () => ({ switchyardOpsCall: jest.fn() }))
const mockOps = jest.mocked(switchyardOpsCall)
const mockCookies = jest.mocked(cookies)
const fixtures = [
  { action: 'topology', get: topology, namespace: undefined, data: { nodes: [{ name: 'node-a' }], namespaces: [], pods: [], services: [], totals: { nodes: 1 }, display: {}, synced_at: 'observed' } },
  { action: 'applications', get: applications, namespace: 'argocd', data: { applications: [{ name: 'app-a', source_repo: 'https://example.org/repo' }], synced_at: 'observed' } },
  { action: 'volumes', get: volumes, namespace: 'longhorn-system', data: { volumes: [{ name: 'vol-a', replicas: [{ name: 'rep-a' }] }], summary: { total: 1 }, synced_at: 'observed' } },
  { action: 'network-policies', get: policies, namespace: undefined, data: { groups: [{ namespace: 'tenant-a', policies: [] }], total_policies: 0, total_namespaces: 1, synced_at: 'observed' } },
]
beforeEach(() => {
  jest.clearAllMocks()
  mockCookies.mockResolvedValue({ get: (name: string) => name === 'dispatch_auth' ? { value: 'caller-token' } : undefined } as never)
})

describe.each(fixtures)('$action inventory', ({ action, get, namespace, data }) => {
  it('passes the authenticated caller and preserves the complete observed schema', async () => {
    mockOps.mockResolvedValue({ ok: true, status: 200, data: { status: 'succeeded', data } })
    const response = await get()
    expect(response.status).toBe(200)
    expect(await response.json()).toEqual(data)
    expect(mockOps).toHaveBeenCalledWith('inventory', action, {
      dry_run: true, ...(namespace ? { scope: { namespace } } : {}),
    }, 'caller-token')
  })
  it.each([401, 403, 503])('preserves upstream refusal %s', async (status) => {
    mockOps.mockResolvedValue({ ok: false, status, data: { error: 'private upstream address' } })
    const response = await get()
    expect(response.status).toBe(status)
    expect(await response.json()).toEqual({ error: status === 401 ? 'Authentication required' : status === 403 ? 'Platform operator access required' : 'Cluster inventory is unavailable' })
  })
  it.each([['adapter_unconfigured', 503], ['failed', 502]])('does not disguise %s as empty inventory', async (state, status) => {
    mockOps.mockResolvedValue({ ok: true, status: 200, data: { status: state, summary: 'private upstream address' } })
    const response = await get()
    expect(response.status).toBe(status)
    expect(await response.json()).toEqual({ error: 'Cluster inventory is unavailable' })
  })
})

it('returns a gateway error on network failure without leaking internals', async () => {
  mockOps.mockRejectedValue(new Error('private upstream details'))
  const response = await topology()
  expect(response.status).toBe(502)
  expect(JSON.stringify(await response.json())).not.toContain('private upstream')
})
