import { inventoryResponse } from '@/lib/inventory-proxy'

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

export async function GET() {
  return inventoryResponse('topology')
}
