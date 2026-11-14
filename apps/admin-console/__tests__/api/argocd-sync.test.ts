/**
 * Tests for app/api/argocd/applications/[name]/sync/route.ts
 *
 * Verifies the superadmin gate, name validation, and that the sync is sent
 * with the @kubernetes/client-node 1.x object-parameter signature as a JSON
 * merge patch (the client's default patch type is JSON patch, which the API
 * server would reject for this body).
 */

jest.mock('next/server', () => {
  class MockNextResponse {
    body: unknown
    status: number
    constructor(body: string, init?: { status?: number }) {
      this.body = JSON.parse(body)
      this.status = init?.status ?? 200
    }
    async json() {
      return this.body
    }
    static json(data: unknown, init?: { status?: number }) {
      return new MockNextResponse(JSON.stringify(data), init)
    }
  }
  return { NextResponse: MockNextResponse }
})

const mockVerifyAuth = jest.fn()
jest.mock('@/lib/api-auth', () => ({
  verifyAuth: (...args: unknown[]) => mockVerifyAuth(...args),
  hasRole: (claims: { roles: string[] } | null, ...required: string[]) =>
    !!claims && claims.roles.some((r) => required.includes(r)),
}))

const mockPatchCustomObject = jest.fn()
const mergePatchMarker = { middleware: ['merge-patch'] }
const mockSetHeaderOptions = jest.fn((..._args: unknown[]) => mergePatchMarker)
jest.mock('@kubernetes/client-node', () => {
  class FakeKubeConfig {
    loadFromCluster() {}
    loadFromDefault() {}
    makeApiClient(api: unknown) {
      const name = (api as { name?: string }).name
      if (name === 'CustomObjectsApi') {
        return { patchNamespacedCustomObject: mockPatchCustomObject }
      }
      return {}
    }
  }
  class CoreV1Api {}
  class AppsV1Api {}
  class NetworkingV1Api {}
  class CustomObjectsApi {}
  return {
    KubeConfig: FakeKubeConfig,
    CoreV1Api,
    AppsV1Api,
    NetworkingV1Api,
    CustomObjectsApi,
    PatchStrategy: { MergePatch: 'application/merge-patch+json' },
    setHeaderOptions: (...args: unknown[]) => mockSetHeaderOptions(...args),
  }
})

import type { NextRequest } from 'next/server'
import { POST } from '@/app/api/argocd/applications/[name]/sync/route'

const request = {} as NextRequest
const params = (name: string) => ({ params: Promise.resolve({ name }) })

beforeEach(() => {
  jest.clearAllMocks()
})

describe('POST /api/argocd/applications/[name]/sync', () => {
  it('rejects callers without the superadmin role', async () => {
    mockVerifyAuth.mockResolvedValueOnce({ email: null, roles: ['operator'] })

    const response = await POST(request, params('enclii-api'))

    expect((response as unknown as { status: number }).status).toBe(403)
    expect(mockPatchCustomObject).not.toHaveBeenCalled()
  })

  it('rejects invalid application names', async () => {
    mockVerifyAuth.mockResolvedValueOnce({ email: null, roles: ['superadmin'] })

    const response = await POST(request, params('bad/name'))

    expect((response as unknown as { status: number }).status).toBe(400)
    expect(mockPatchCustomObject).not.toHaveBeenCalled()
  })

  it('patches operation.sync as a merge patch with the object signature', async () => {
    mockVerifyAuth.mockResolvedValueOnce({ email: null, roles: ['superadmin'] })
    mockPatchCustomObject.mockResolvedValueOnce({})

    const response = await POST(request, params('enclii-api'))
    const body = (await response.json()) as { status: string; name: string }

    expect((response as unknown as { status: number }).status).toBe(200)
    expect(body).toMatchObject({ status: 'sync_triggered', name: 'enclii-api' })
    expect(mockSetHeaderOptions).toHaveBeenCalledWith('Content-Type', 'application/merge-patch+json')
    expect(mockPatchCustomObject).toHaveBeenCalledWith(
      {
        group: 'argoproj.io',
        version: 'v1alpha1',
        namespace: 'argocd',
        plural: 'applications',
        name: 'enclii-api',
        body: { operation: { sync: {} } },
      },
      mergePatchMarker
    )
  })

  it('returns 500 when the patch fails', async () => {
    mockVerifyAuth.mockResolvedValueOnce({ email: null, roles: ['superadmin'] })
    mockPatchCustomObject.mockRejectedValueOnce(new Error('applications.argoproj.io "x" not found'))

    const response = await POST(request, params('enclii-api'))
    const body = (await response.json()) as { error: string }

    expect((response as unknown as { status: number }).status).toBe(500)
    expect(body.error).toMatch(/not found/)
  })
})
