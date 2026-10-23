jest.mock('next/server', () => ({
  NextResponse: {
    json: (body: unknown, init?: { status?: number; headers?: Record<string, string> }) => ({
      status: init?.status ?? 200,
      headers: new Map(Object.entries(init?.headers ?? {})),
      json: async () => body,
    }),
  },
}))
jest.mock('next/headers', () => ({ cookies: jest.fn() }))
jest.mock('@/lib/caller-proxy', () => ({ callerProxy: jest.fn() }))

import { GET } from '@/app/api/topology/route'
import { callerProxy } from '@/lib/caller-proxy'
import { cookies } from 'next/headers'

const mockProxy = jest.mocked(callerProxy)
const mockCookies = jest.mocked(cookies)
const snapshot = {
  nodes: [{ name: 'node-a', role: 'builder', used: { cpu_millicores: 500, memory_bytes: 128, pod_count: 1 } }],
  namespaces: [{ name: 'tenant-a', pod_count: 2, data_access_label: 'runtime' }],
  pods: [{ name: 'pending-a', node: '', phase: 'Pending' }],
  services: [{ name: 'service-a', backend_observation: { status: 'observed' } }],
  totals: { nodes: 1, pods: 2 }, display: {}, synced_at: '2026-10-22T12:00:00Z',
}
function upstream(status: number, body: unknown) {
  return { ok: status >= 200 && status < 300, status, json: async () => body } as Response
}
beforeAll(() => {
  Object.defineProperty(AbortSignal, 'timeout', { configurable: true, value: () => new AbortController().signal })
})
beforeEach(() => {
  jest.clearAllMocks()
  mockCookies.mockResolvedValue({ get: (key: string) => key === 'dispatch_auth' ? { value: 'caller-token' } : undefined } as never)
})

it('forwards the caller to the read-only inventory and preserves builder roles and all evidence', async () => {
  mockProxy.mockResolvedValue(upstream(200, { status: 'succeeded', data: snapshot }))
  const response = await GET()
  expect(response.status).toBe(200)
  expect(await response.json()).toEqual(snapshot)
  expect(response.headers.get('Cache-Control')).toBe('no-store')
  expect(mockProxy).toHaveBeenCalledWith('/ops/inventory/topology', expect.objectContaining({
    method: 'POST', body: JSON.stringify({ dry_run: true }), userToken: 'caller-token',
  }))
})
it('supports the established legacy auth cookie', async () => {
  mockCookies.mockResolvedValue({ get: (key: string) => key === 'admin_auth' ? { value: 'legacy-caller' } : undefined } as never)
  mockProxy.mockResolvedValue(upstream(200, { status: 'succeeded', data: snapshot }))
  await GET()
  expect(mockProxy).toHaveBeenCalledWith('/ops/inventory/topology', expect.objectContaining({ userToken: 'legacy-caller' }))
})
it.each([401, 403, 503])('preserves upstream refusal %s without private details', async (status) => {
  mockProxy.mockResolvedValue(upstream(status, { error: 'private transport address' }))
  const response = await GET()
  expect(response.status).toBe(status)
  expect(await response.json()).toEqual({ error: status === 401 ? 'Authentication required' : status === 403 ? 'Platform operator access required' : 'Cluster inventory is unavailable' })
})
it.each([
  [{ status: 'adapter_unconfigured' }, 503],
  [{ status: 'failed' }, 502],
  [{ status: 'succeeded', data: {} }, 502],
  [{ status: 'succeeded', data: { ...snapshot, nodes: null } }, 502],
])('does not disguise unavailable or incomplete data as empty inventory', async (body, status) => {
  mockProxy.mockResolvedValue(upstream(200, body))
  const response = await GET()
  expect(response.status).toBe(status)
  expect(await response.json()).toEqual({ error: 'Cluster inventory is unavailable' })
})
it('sanitizes network failures', async () => {
  mockProxy.mockRejectedValue(new Error('private transport address'))
  const response = await GET()
  expect(response.status).toBe(502)
  expect(await response.json()).toEqual({ error: 'Switchyard inventory is unreachable' })
})
