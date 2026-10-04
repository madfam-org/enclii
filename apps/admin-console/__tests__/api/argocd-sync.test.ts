/** @jest-environment node */
import { NextRequest } from 'next/server'
import { POST } from '@/app/api/argocd/applications/[name]/sync/route'
import { verifyAuth } from '@/lib/api-auth'
import { switchyardOpsCall } from '@/lib/switchyard-proxy'

jest.mock('@/lib/api-auth', () => ({
  verifyAuth: jest.fn(),
  hasRole: (claims: { roles: string[] } | null, role: string) => claims?.roles.includes(role) ?? false,
}))
jest.mock('@/lib/switchyard-proxy', () => ({ switchyardOpsCall: jest.fn() }))

const mockVerify = jest.mocked(verifyAuth)
const mockOps = jest.mocked(switchyardOpsCall)
const params = (name = 'fixture-app') => ({ params: Promise.resolve({ name }) })
const request = (cookie = 'dispatch_auth=operator-jwt') => new NextRequest('https://console.example.org/api/argocd/applications/fixture-app/sync', { method: 'POST', headers: { Cookie: cookie } })

beforeEach(() => {
  jest.clearAllMocks()
  mockVerify.mockResolvedValue({ email: 'operator@example.org', roles: ['superadmin'] })
})

it('requests an audited non-pruning sync as the caller', async () => {
  mockOps.mockResolvedValue({ ok: true, status: 200, data: { status: 'succeeded' } })
  const result = await POST(request(), params())
  expect(result.status).toBe(200)
  expect(mockOps).toHaveBeenCalledWith('apps', 'sync', {
    dry_run: false,
    reason: 'Operator requested ArgoCD sync for fixture-app from Dispatch',
    scope: { namespace: 'argocd' },
    args: { target: 'fixture-app', prune: 'false' },
  }, 'operator-jwt')
})

it.each([401, 403, 409, 503])('preserves Switchyard refusal %s', async (status) => {
  mockOps.mockResolvedValue({ ok: false, status, data: { message: 'Request refused' } })
  const result = await POST(request(), params())
  expect(result.status).toBe(status)
  expect(await result.json()).toEqual({ error: 'Request refused' })
})

it('does not call the adapter for an unauthorized role', async () => {
  mockVerify.mockResolvedValue({ email: 'tenant@example.org', roles: ['admin'] })
  expect((await POST(request(), params())).status).toBe(403)
  expect(mockOps).not.toHaveBeenCalled()
})

it('does not call the adapter for an invalid application name', async () => {
  expect((await POST(request(), params('../other'))).status).toBe(400)
  expect(mockOps).not.toHaveBeenCalled()
})

it('forwards an accepted legacy session as that same caller', async () => {
  mockOps.mockResolvedValue({ ok: true, status: 200, data: {} })
  await POST(request('admin_auth=legacy-operator'), params())
  expect(mockOps).toHaveBeenCalledWith('apps', 'sync', expect.any(Object), 'legacy-operator')
})
