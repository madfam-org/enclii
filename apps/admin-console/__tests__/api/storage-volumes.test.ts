/**
 * Tests for app/api/storage/volumes/route.ts
 *
 * Verifies sort order (faulted first), summary aggregation, and replica
 * mapping logic.
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

const mockListCustomObject = jest.fn()

jest.mock('@kubernetes/client-node', () => {
  class FakeKubeConfig {
    loadFromCluster() {}
    loadFromDefault() {}
    makeApiClient(api: unknown) {
      const name = (api as { name?: string }).name
      if (name === 'CustomObjectsApi') {
        return { listNamespacedCustomObject: mockListCustomObject }
      }
      return {}
    }
  }
  class CoreV1Api {}
  class AppsV1Api {}
  class NetworkingV1Api {}
  class CustomObjectsApi {}
  return { KubeConfig: FakeKubeConfig, CoreV1Api, AppsV1Api, NetworkingV1Api, CustomObjectsApi }
})

import { GET } from '@/app/api/storage/volumes/route'
import { invalidateCache } from '@/lib/k8s-client'

beforeEach(() => {
  jest.clearAllMocks()
  invalidateCache()
})

function makeVolume(name: string, robustness: string, state: string, size = '10Gi') {
  return {
    metadata: { name, namespace: 'longhorn-system', creationTimestamp: '2026-04-01T00:00:00Z' },
    spec: { size, numberOfReplicas: 2, dataEngine: 'v1' },
    status: {
      state,
      robustness,
      currentNodeID: state === 'attached' ? 'foundry-cp' : null,
      kubernetesStatus: { pvcName: `${name}-pvc`, namespace: 'enclii' },
    },
  }
}

describe('GET /api/storage/volumes', () => {
  it('sorts faulted -> degraded -> healthy and computes summary', async () => {
    mockListCustomObject
      .mockResolvedValueOnce({
        body: {
          items: [
            makeVolume('vol-healthy', 'healthy', 'attached'),
            makeVolume('vol-faulted', 'faulted', 'detached'),
            makeVolume('vol-degraded', 'degraded', 'attached'),
          ],
        },
      })
      .mockResolvedValueOnce({ body: { items: [] } }) // replicas

    const response = await GET()
    const body = (await response.json()) as {
      volumes: { name: string; robustness: string }[]
      summary: { total: number; healthy: number; degraded: number; faulted: number }
    }

    expect(body.volumes.map((v) => v.name)).toEqual(['vol-faulted', 'vol-degraded', 'vol-healthy'])
    expect(body.summary).toMatchObject({ total: 3, healthy: 1, degraded: 1, faulted: 1 })
  })

  it('attaches replicas to their owning volume', async () => {
    mockListCustomObject
      .mockResolvedValueOnce({ body: { items: [makeVolume('vol-1', 'healthy', 'attached')] } })
      .mockResolvedValueOnce({
        body: {
          items: [
            {
              metadata: { name: 'vol-1-r-aaaa' },
              spec: { volumeName: 'vol-1', nodeID: 'foundry-cp' },
              status: { currentState: 'running', mode: 'RW' },
            },
            {
              metadata: { name: 'vol-1-r-bbbb' },
              spec: { volumeName: 'vol-1', nodeID: 'foundry-worker-01' },
              status: { currentState: 'running', mode: 'RW' },
            },
          ],
        },
      })

    const response = await GET()
    const body = (await response.json()) as {
      volumes: { name: string; replicas: { node: string; running: boolean }[] }[]
    }
    expect(body.volumes[0].replicas).toHaveLength(2)
    expect(body.volumes[0].replicas.map((r) => r.node).sort()).toEqual([
      'foundry-cp',
      'foundry-worker-01',
    ])
    expect(body.volumes[0].replicas.every((r) => r.running)).toBe(true)
  })

  it('returns 500 on Longhorn CR list failure', async () => {
    // Both calls in Promise.all get a mock — Promise.all rejects on the first failure.
    mockListCustomObject.mockRejectedValueOnce(new Error('CRD not found'))
    mockListCustomObject.mockResolvedValueOnce({ body: { items: [] } })

    const response = await GET()
    const body = (await response.json()) as { error: string }
    expect((response as unknown as { status: number }).status).toBe(500)
    expect(body.error).toMatch(/CRD not found/)
  })
})
