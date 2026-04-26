/**
 * GET /api/topology
 *
 * Returns the live cluster topology by querying the K8s API server via the
 * in-cluster ServiceAccount. Cached for 30s in-memory.
 *
 * Auth: enforced by middleware (admin/operator role + allowed domain).
 */
import { NextResponse } from 'next/server'
import {
  appsApi,
  cached,
  coreApi,
  formatBytes,
  formatCpu,
  parseCpu,
  parseMemory,
} from '@/lib/k8s-client'

export const dynamic = 'force-dynamic'

interface TopologyNode {
  name: string
  role: 'control-plane' | 'worker' | 'builder' | 'unknown'
  status: 'Ready' | 'NotReady' | 'Unknown'
  conditions: { type: string; status: string; reason?: string }[]
  kubelet_version?: string
  os_image?: string
  capacity: { cpu_millicores: number; memory_bytes: number; pods: number }
  allocatable: { cpu_millicores: number; memory_bytes: number; pods: number }
  used: { cpu_millicores: number; memory_bytes: number; pod_count: number }
  taints: { key: string; value?: string; effect: string }[]
  labels?: Record<string, string>
}

interface TopologyPod {
  name: string
  namespace: string
  node: string
  phase: string
  ready: string
  restart_count: number
  containers: { name: string; image: string; ready: boolean }[]
  age_seconds: number
}

interface TopologyNamespace {
  name: string
  pod_count: number
  deployment_count: number
  service_count: number
  pod_phases: Record<string, number>
}

interface TopologyService {
  name: string
  namespace: string
  type: string
  cluster_ip: string | null
  ports: { port: number; target_port: number | string; protocol: string; name?: string }[]
  selector: Record<string, string> | null
}

export interface TopologyResponse {
  nodes: TopologyNode[]
  namespaces: TopologyNamespace[]
  pods: TopologyPod[]
  services: TopologyService[]
  synced_at: string
  totals: {
    nodes: number
    namespaces: number
    pods: number
    services: number
    cpu_capacity_millicores: number
    cpu_used_millicores: number
    memory_capacity_bytes: number
    memory_used_bytes: number
  }
  display: {
    cpu_capacity: string
    cpu_used: string
    memory_capacity: string
    memory_used: string
  }
}

function inferRole(labels: Record<string, string> | undefined): TopologyNode['role'] {
  if (!labels) return 'unknown'
  if (labels['node-role.kubernetes.io/control-plane'] !== undefined) return 'control-plane'
  if (labels['node-role.kubernetes.io/master'] !== undefined) return 'control-plane'
  if (labels['enclii.dev/role'] === 'builder') return 'builder'
  if (labels['node-role.kubernetes.io/worker'] !== undefined) return 'worker'
  return 'worker'
}

async function fetchTopology(): Promise<TopologyResponse> {
  const core = coreApi()
  const apps = appsApi()

  const [nodesRes, namespacesRes, podsRes, servicesRes, deploymentsRes] = await Promise.all([
    core.listNode(),
    core.listNamespace(),
    core.listPodForAllNamespaces(),
    core.listServiceForAllNamespaces(),
    apps.listDeploymentForAllNamespaces(),
  ])

  const nowMs = Date.now()
  const nodeMap = new Map<string, TopologyNode>()

  for (const n of nodesRes.items) {
    const labels = n.metadata?.labels ?? {}
    const status = n.status
    const readyCondition = status?.conditions?.find((c) => c.type === 'Ready')
    nodeMap.set(n.metadata?.name ?? '', {
      name: n.metadata?.name ?? '',
      role: inferRole(labels),
      status:
        readyCondition?.status === 'True'
          ? 'Ready'
          : readyCondition?.status === 'False'
            ? 'NotReady'
            : 'Unknown',
      conditions:
        status?.conditions?.map((c) => ({
          type: c.type,
          status: c.status,
          reason: c.reason ?? undefined,
        })) ?? [],
      kubelet_version: status?.nodeInfo?.kubeletVersion,
      os_image: status?.nodeInfo?.osImage,
      capacity: {
        cpu_millicores: parseCpu(status?.capacity?.cpu),
        memory_bytes: parseMemory(status?.capacity?.memory),
        pods: parseInt(status?.capacity?.pods ?? '0', 10) || 0,
      },
      allocatable: {
        cpu_millicores: parseCpu(status?.allocatable?.cpu),
        memory_bytes: parseMemory(status?.allocatable?.memory),
        pods: parseInt(status?.allocatable?.pods ?? '0', 10) || 0,
      },
      used: { cpu_millicores: 0, memory_bytes: 0, pod_count: 0 },
      taints: (n.spec?.taints ?? []).map((t) => ({
        key: t.key,
        value: t.value ?? undefined,
        effect: t.effect,
      })),
      labels,
    })
  }

  const pods: TopologyPod[] = []
  for (const p of podsRes.items) {
    const nodeName = p.spec?.nodeName ?? ''
    const containerStatuses = p.status?.containerStatuses ?? []
    const readyCount = containerStatuses.filter((c) => c.ready).length
    const totalContainers = containerStatuses.length || (p.spec?.containers?.length ?? 0)
    const restartCount = containerStatuses.reduce((sum, c) => sum + (c.restartCount ?? 0), 0)
    const startMs = p.status?.startTime ? new Date(p.status.startTime).getTime() : nowMs

    pods.push({
      name: p.metadata?.name ?? '',
      namespace: p.metadata?.namespace ?? '',
      node: nodeName,
      phase: p.status?.phase ?? 'Unknown',
      ready: `${readyCount}/${totalContainers}`,
      restart_count: restartCount,
      containers: (p.spec?.containers ?? []).map((c) => ({
        name: c.name,
        image: c.image ?? '',
        ready: containerStatuses.find((s) => s.name === c.name)?.ready ?? false,
      })),
      age_seconds: Math.max(0, Math.floor((nowMs - startMs) / 1000)),
    })

    const node = nodeMap.get(nodeName)
    if (node && p.status?.phase === 'Running') {
      node.used.pod_count += 1
      for (const c of p.spec?.containers ?? []) {
        node.used.cpu_millicores += parseCpu(c.resources?.requests?.cpu)
        node.used.memory_bytes += parseMemory(c.resources?.requests?.memory)
      }
    }
  }

  const podsByNamespace = new Map<string, TopologyPod[]>()
  for (const p of pods) {
    const list = podsByNamespace.get(p.namespace) ?? []
    list.push(p)
    podsByNamespace.set(p.namespace, list)
  }
  const deploymentsByNamespace = new Map<string, number>()
  for (const d of deploymentsRes.items) {
    const ns = d.metadata?.namespace ?? ''
    deploymentsByNamespace.set(ns, (deploymentsByNamespace.get(ns) ?? 0) + 1)
  }
  const servicesByNamespace = new Map<string, number>()
  const services: TopologyService[] = []
  for (const s of servicesRes.items) {
    const ns = s.metadata?.namespace ?? ''
    servicesByNamespace.set(ns, (servicesByNamespace.get(ns) ?? 0) + 1)
    services.push({
      name: s.metadata?.name ?? '',
      namespace: ns,
      type: s.spec?.type ?? 'ClusterIP',
      cluster_ip: s.spec?.clusterIP ?? null,
      ports: (s.spec?.ports ?? []).map((p) => ({
        port: p.port,
        target_port: (p.targetPort as number | string) ?? p.port,
        protocol: p.protocol ?? 'TCP',
        name: p.name ?? undefined,
      })),
      selector: s.spec?.selector ?? null,
    })
  }

  const namespaces: TopologyNamespace[] = namespacesRes.items.map((n) => {
    const name = n.metadata?.name ?? ''
    const nsPods = podsByNamespace.get(name) ?? []
    const phases: Record<string, number> = {}
    for (const p of nsPods) phases[p.phase] = (phases[p.phase] ?? 0) + 1
    return {
      name,
      pod_count: nsPods.length,
      deployment_count: deploymentsByNamespace.get(name) ?? 0,
      service_count: servicesByNamespace.get(name) ?? 0,
      pod_phases: phases,
    }
  })

  const nodeList = Array.from(nodeMap.values())
  const totals = {
    nodes: nodeList.length,
    namespaces: namespaces.length,
    pods: pods.length,
    services: services.length,
    cpu_capacity_millicores: nodeList.reduce((s, n) => s + n.capacity.cpu_millicores, 0),
    cpu_used_millicores: nodeList.reduce((s, n) => s + n.used.cpu_millicores, 0),
    memory_capacity_bytes: nodeList.reduce((s, n) => s + n.capacity.memory_bytes, 0),
    memory_used_bytes: nodeList.reduce((s, n) => s + n.used.memory_bytes, 0),
  }

  return {
    nodes: nodeList.sort((a, b) => a.name.localeCompare(b.name)),
    namespaces: namespaces.sort((a, b) => a.name.localeCompare(b.name)),
    pods,
    services,
    synced_at: new Date().toISOString(),
    totals,
    display: {
      cpu_capacity: formatCpu(totals.cpu_capacity_millicores),
      cpu_used: formatCpu(totals.cpu_used_millicores),
      memory_capacity: formatBytes(totals.memory_capacity_bytes),
      memory_used: formatBytes(totals.memory_used_bytes),
    },
  }
}

export async function GET() {
  try {
    const data = await cached('topology:v1', 30_000, fetchTopology)
    return NextResponse.json(data)
  } catch (error) {
    console.error('[Dispatch /api/topology] error:', error)
    return NextResponse.json(
      { error: error instanceof Error ? error.message : 'Failed to fetch topology' },
      { status: 500 }
    )
  }
}
