/**
 * Tests for app/api/network-policies/route.ts
 *
 * Verifies the per-namespace grouping and the ingress/egress summarization
 * logic (the human-readable peer/port strings used in the UI).
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

const mockListNetworkPolicy = jest.fn()

jest.mock('@kubernetes/client-node', () => {
  class FakeKubeConfig {
    loadFromCluster() {}
    loadFromDefault() {}
    makeApiClient(api: unknown) {
      const name = (api as { name?: string }).name
      if (name === 'NetworkingV1Api') {
        return { listNetworkPolicyForAllNamespaces: mockListNetworkPolicy }
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

import { GET } from '@/app/api/network-policies/route'
import { invalidateCache } from '@/lib/k8s-client'

beforeEach(() => {
  jest.clearAllMocks()
  invalidateCache()
})

describe('GET /api/network-policies', () => {
  it('groups policies by namespace and summarizes peers + ports', async () => {
    mockListNetworkPolicy.mockResolvedValueOnce({
      items: [
        {
          metadata: { name: 'enclii-default-deny', namespace: 'enclii' },
          spec: {
            podSelector: {},
            policyTypes: ['Ingress', 'Egress'],
            ingress: [],
            egress: [],
          },
        },
        {
          metadata: { name: 'enclii-allow-api', namespace: 'enclii' },
          spec: {
            podSelector: { matchLabels: { app: 'switchyard-api' } },
            policyTypes: ['Ingress'],
            ingress: [
              {
                from: [{ podSelector: { matchLabels: { app: 'switchyard-ui' } } }],
                ports: [{ port: 4200, protocol: 'TCP' }],
              },
            ],
          },
        },
        {
          metadata: { name: 'argocd-allow-controller', namespace: 'argocd' },
          spec: {
            podSelector: { matchLabels: { 'app.kubernetes.io/name': 'argocd-application-controller' } },
            policyTypes: ['Egress'],
            egress: [
              {
                to: [{ ipBlock: { cidr: '0.0.0.0/0', except: ['169.254.0.0/16'] } }],
                ports: [{ port: 443, protocol: 'TCP' }],
              },
            ],
          },
        },
      ],
    })

    const response = await GET()
    const body = (await response.json()) as {
      groups: { namespace: string; policies: unknown[] }[]
      total_policies: number
      total_namespaces: number
    }

    expect(body.total_policies).toBe(3)
    expect(body.total_namespaces).toBe(2)
    expect(body.groups.map((g) => g.namespace)).toEqual(['argocd', 'enclii'])

    const enclii = body.groups.find((g) => g.namespace === 'enclii')!
    const allowApi = enclii.policies.find(
      (p) => (p as { name: string }).name === 'enclii-allow-api'
    ) as { ingress_summary: string[]; pod_selector_summary: string }
    expect(allowApi.ingress_summary[0]).toContain('app=switchyard-api'.replace('switchyard-api', 'switchyard-ui'))
    expect(allowApi.ingress_summary[0]).toContain('TCP/4200')
    expect(allowApi.pod_selector_summary).toBe('app=switchyard-api')

    const argocd = body.groups.find((g) => g.namespace === 'argocd')!
    const policy = argocd.policies[0] as { egress_summary: string[] }
    expect(policy.egress_summary[0]).toContain('0.0.0.0/0')
    expect(policy.egress_summary[0]).toContain('169.254.0.0/16')
    expect(policy.egress_summary[0]).toContain('TCP/443')
  })

  it('renders deny-all when policy types are listed but no rules are defined', async () => {
    mockListNetworkPolicy.mockResolvedValueOnce({
      items: [
        {
          metadata: { name: 'deny-all', namespace: 'demo' },
          spec: { podSelector: {}, policyTypes: ['Ingress', 'Egress'], ingress: [], egress: [] },
        },
      ],
    })

    const response = await GET()
    const body = (await response.json()) as {
      groups: { policies: { ingress_summary: string[]; egress_summary: string[]; policy_types: string[] }[] }[]
    }
    const policy = body.groups[0].policies[0]
    expect(policy.ingress_summary).toEqual([])
    expect(policy.egress_summary).toEqual([])
    expect(policy.policy_types).toEqual(['Ingress', 'Egress'])
  })

  it('returns 500 on K8s API failure', async () => {
    mockListNetworkPolicy.mockRejectedValueOnce(new Error('connection refused'))

    const response = await GET()
    const body = (await response.json()) as { error: string }
    expect((response as unknown as { status: number }).status).toBe(500)
    expect(body.error).toMatch(/connection refused/)
  })
})
