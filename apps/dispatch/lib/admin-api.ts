import type {
  BareMetalHost,
  Cluster,
  ManagedResource,
  VirtualCluster,
  PropagationPolicy,
  DriftEvent,
  CostAllocation,
  TopologyNode,
  TopologyEdge,
} from '@/types/admin'

async function adminFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`/api/admin${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || `API error: ${res.status}`)
  }
  return res.json()
}

// Fleet
export const fleetApi = {
  list: () => adminFetch<{ hosts: BareMetalHost[] }>('/fleet'),
  get: (id: string) => adminFetch<BareMetalHost>(`/fleet/${id}`),
  register: (host: Partial<BareMetalHost>) => adminFetch<BareMetalHost>('/fleet', { method: 'POST', body: JSON.stringify(host) }),
  power: (id: string, action: string) => adminFetch<{ status: string }>(`/fleet/${id}/power`, { method: 'PUT', body: JSON.stringify({ action }) }),
  wipe: (id: string) => adminFetch<{ status: string }>(`/fleet/${id}/wipe`, { method: 'POST' }),
  update: (id: string, host: Partial<BareMetalHost>) => adminFetch<BareMetalHost>(`/fleet/${id}`, { method: 'PUT', body: JSON.stringify(host) }),
  firmware: (id: string, settings: Record<string, string>) => adminFetch<{ status: string }>(`/fleet/${id}/firmware`, { method: 'PUT', body: JSON.stringify({ settings }) }),
}

// Clusters
export const clusterApi = {
  list: () => adminFetch<{ clusters: Cluster[] }>('/clusters'),
  get: (id: string) => adminFetch<Cluster>(`/clusters/${id}`),
  register: (cluster: Partial<Cluster>) => adminFetch<Cluster>('/clusters', { method: 'POST', body: JSON.stringify(cluster) }),
  update: (id: string, cluster: Partial<Cluster>) => adminFetch<Cluster>(`/clusters/${id}`, { method: 'PUT', body: JSON.stringify(cluster) }),
  deregister: (id: string) => adminFetch<void>(`/clusters/${id}`, { method: 'DELETE' }),
}

// Resources
export const resourceApi = {
  list: (params?: { provider?: string; kind?: string; status?: string }) => {
    const qs = new URLSearchParams(params as Record<string, string>).toString()
    return adminFetch<{ resources: ManagedResource[] }>(`/resources${qs ? '?' + qs : ''}`)
  },
  get: (id: string) => adminFetch<ManagedResource>(`/resources/${id}`),
  create: (resource: Partial<ManagedResource>) => adminFetch<ManagedResource>('/resources', { method: 'POST', body: JSON.stringify(resource) }),
  updatePolicy: (id: string, policy: string) => adminFetch<{ status: string }>(`/resources/${id}/policy`, { method: 'PUT', body: JSON.stringify({ management_policy: policy }) }),
  delete: (id: string) => adminFetch<void>(`/resources/${id}`, { method: 'DELETE' }),
}

// Virtual Clusters
export const vclusterApi = {
  list: () => adminFetch<{ vclusters: VirtualCluster[] }>('/vclusters'),
  get: (id: string) => adminFetch<VirtualCluster>(`/vclusters/${id}`),
  provision: (vc: Partial<VirtualCluster>) => adminFetch<VirtualCluster>('/vclusters', { method: 'POST', body: JSON.stringify(vc) }),
  teardown: (id: string) => adminFetch<void>(`/vclusters/${id}`, { method: 'DELETE' }),
  kubeconfig: (id: string) => adminFetch<{ kubeconfig: string }>(`/vclusters/${id}/kubeconfig`),
}

// Propagation
export const propagationApi = {
  list: () => adminFetch<{ policies: PropagationPolicy[] }>('/propagation'),
  get: (id: string) => adminFetch<PropagationPolicy>(`/propagation/${id}`),
  create: (policy: Partial<PropagationPolicy>) => adminFetch<PropagationPolicy>('/propagation', { method: 'POST', body: JSON.stringify(policy) }),
  delete: (id: string) => adminFetch<void>(`/propagation/${id}`, { method: 'DELETE' }),
}

// Drift
export const driftApi = {
  list: (resolved?: boolean) => {
    const qs = resolved !== undefined ? `?resolved=${resolved}` : ''
    return adminFetch<{ events: DriftEvent[] }>(`/drift${qs}`)
  },
  get: (id: string) => adminFetch<DriftEvent>(`/drift/${id}`),
  resolve: (id: string) => adminFetch<{ status: string }>(`/drift/${id}/resolve`, { method: 'POST' }),
}

// Costs
export const costApi = {
  list: (params?: { tenant_id?: string; start?: string; end?: string }) => {
    const qs = new URLSearchParams(params as Record<string, string>).toString()
    return adminFetch<{ allocations: CostAllocation[] }>(`/costs${qs ? '?' + qs : ''}`)
  },
  summary: (params?: { start?: string; end?: string }) => {
    const qs = new URLSearchParams(params as Record<string, string>).toString()
    return adminFetch<{ summary: CostAllocation[] }>(`/costs/summary${qs ? '?' + qs : ''}`)
  },
  allocate: (allocation: Partial<CostAllocation>) => adminFetch<CostAllocation>('/costs/allocate', { method: 'POST', body: JSON.stringify(allocation) }),
}

// Topology
export const topologyApi = {
  get: () => adminFetch<{ nodes: TopologyNode[]; edges: TopologyEdge[] }>('/topology'),
}
