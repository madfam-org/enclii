/**
 * Tests for app/api/topology/route.ts
 *
 * The route delegates to k8s-client helpers. We mock @kubernetes/client-node
 * so we can verify aggregation logic (CPU/memory totals, namespace pod
 * counts, role inference, error handling) without real cluster access.
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

const mockListNode = jest.fn()
const mockListNamespace = jest.fn()
const mockListPod = jest.fn()
const mockListService = jest.fn()
const mockListDeployment = jest.fn()

jest.mock('@kubernetes/client-node', () => {
  class FakeKubeConfig {
    loadFromCluster() {}
    loadFromDefault() {}
    makeApiClient(api: unknown) {
      // Return per-class fake instances based on the constructor name
      const name = (api as { name?: string }).name
      if (name === 'CoreV1Api') {
        return {
          listNode: mockListNode,
          listNamespace: mockListNamespace,
          listPodForAllNamespaces: mockListPod,
          listServiceForAllNamespaces: mockListService,
        }
      }
      if (name === 'AppsV1Api') {
        return { listDeploymentForAllNamespaces: mockListDeployment }
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

import { GET } from '@/app/api/topology/route'
import { invalidateCache } from '@/lib/k8s-client'

beforeEach(() => {
  jest.clearAllMocks()
  invalidateCache()
})

function fakeNode(name: string, role: 'control-plane' | 'worker', cpu = '4', mem = '8Gi') {
  const labels: Record<string, string> = {}
  if (role === 'control-plane') labels['node-role.kubernetes.io/control-plane'] = ''
  return {
    metadata: { name, labels },
    spec: { taints: [] },
    status: {
      conditions: [{ type: 'Ready', status: 'True' }],
      nodeInfo: { kubeletVersion: 'v1.33.7+k3s3', osImage: 'Linux' },
      capacity: { cpu, memory: mem, pods: '110' },
      allocatable: { cpu, memory: mem, pods: '110' },
    },
  }
}

function fakePod(
  name: string,
  namespace: string,
  node: string,
  phase = 'Running',
  cpuReq = '100m',
  memReq = '128Mi'
) {
  return {
    metadata: { name, namespace },
    spec: {
      nodeName: node,
      containers: [
        {
          name: 'main',
          image: 'ghcr.io/example:1',
          resources: { requests: { cpu: cpuReq, memory: memReq } },
        },
      ],
    },
    status: {
      phase,
      containerStatuses: [{ name: 'main', ready: phase === 'Running', restartCount: 0 }],
      startTime: new Date().toISOString(),
    },
  }
}

describe('GET /api/topology', () => {
  it('aggregates nodes, pods, services, deployments correctly', async () => {
    mockListNode.mockResolvedValueOnce({ body: {
      items: [fakeNode('foundry-cp', 'control-plane'), fakeNode('foundry-worker-01', 'worker')],
    })
    mockListNamespace.mockResolvedValueOnce({
      items: [
        { metadata: { name: 'enclii' } },
        { metadata: { name: 'argocd' } },
      ],
    })
    mockListPod.mockResolvedValueOnce({
      items: [
        fakePod('api-1', 'enclii', 'foundry-cp'),
        fakePod('ui-1', 'enclii', 'foundry-cp'),
        fakePod('app-controller-1', 'argocd', 'foundry-worker-01'),
      ],
    })
    mockListService.mockResolvedValueOnce({
      items: [
        {
          metadata: { name: 'switchyard-api', namespace: 'enclii' },
          spec: {
            type: 'ClusterIP',
            clusterIP: '10.0.0.1',
            ports: [{ port: 80, targetPort: 4200, protocol: 'TCP' }],
            selector: { app: 'switchyard-api' },
          },
        },
      ],
    })
    mockListDeployment.mockResolvedValueOnce({
      items: [
        { metadata: { namespace: 'enclii' } },
        { metadata: { namespace: 'enclii' } },
        { metadata: { namespace: 'argocd' } },
      ],
    })

    const response = await GET()
    const body = (await response.json()) as Record<string, unknown>

    expect(body.totals).toMatchObject({
      nodes: 2,
      pods: 3,
      services: 1,
      namespaces: 2,
    })
    const nodes = body.nodes as { name: string; role: string }[]
    expect(nodes.find((n) => n.name === 'foundry-cp')?.role).toBe('control-plane')
    expect(nodes.find((n) => n.name === 'foundry-worker-01')?.role).toBe('worker')

    const namespaces = body.namespaces as {
      name: string
      pod_count: number
      deployment_count: number
    }[]
    const enclii = namespaces.find((ns) => ns.name === 'enclii')
    expect(enclii?.pod_count).toBe(2)
    expect(enclii?.deployment_count).toBe(2)
  })

  it('charges pod CPU/memory requests to their host node', async () => {
    mockListNode.mockResolvedValueOnce({ items: [fakeNode('foundry-cp', 'control-plane')] } })
    mockListNamespace.mockResolvedValueOnce({ body: { items: [{ metadata: { name: 'enclii' } }] } })
    mockListPod.mockResolvedValueOnce({ body: {
      items: [
        fakePod('api-1', 'enclii', 'foundry-cp', 'Running', '500m', '256Mi'),
        fakePod('api-2', 'enclii', 'foundry-cp', 'Running', '500m', '256Mi'),
        // Pending pods should NOT be counted toward used capacity
        fakePod('pending-1', 'enclii', 'foundry-cp', 'Pending', '1000m', '1Gi'),
      ],
    })
    mockListService.mockResolvedValueOnce({ items: [] } })
    mockListDeployment.mockResolvedValueOnce({ body: { items: [] } })

    const response = await GET()
    const body = (await response.json()) as Record<string, unknown>
    const node = (body.nodes as { name: string; used: { cpu_millicores: number; memory_bytes: number; pod_count: number } }[])[0]
    expect(node.used.pod_count).toBe(2)
    expect(node.used.cpu_millicores).toBe(1000) // 500m + 500m
    expect(node.used.memory_bytes).toBe(2 * 256 * 1024 * 1024) // 256Mi + 256Mi
  })

  it('returns 500 with descriptive error when K8s API fails', async () => {
    mockListNode.mockRejectedValueOnce(new Error('Forbidden: cluster role missing'))
    mockListNamespace.mockResolvedValueOnce({ body: { items: [] } })
    mockListPod.mockResolvedValueOnce({ body: { items: [] } })
    mockListService.mockResolvedValueOnce({ body: { items: [] } })
    mockListDeployment.mockResolvedValueOnce({ body: { items: [] } })

    const response = await GET()
    const body = (await response.json()) as { error: string }
    expect((response as unknown as { status: number }).status).toBe(500)
    expect(body.error).toMatch(/Forbidden/)
  })
})
