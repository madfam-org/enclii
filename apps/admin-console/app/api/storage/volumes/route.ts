import { inventoryResponse } from '@/lib/inventory-proxy'

export const dynamic = 'force-dynamic'

export type LonghornState = 'attached' | 'detached' | 'attaching' | 'detaching' | 'deleting' | 'unknown'
export type LonghornRobustness = 'healthy' | 'degraded' | 'faulted' | 'unknown'

export interface VolumeReplica {
  name: string
  node: string
  running: boolean
  mode: string
}

export interface LonghornVolumeSummary {
  name: string
  namespace: string | null
  pvc_name: string | null
  state: LonghornState
  robustness: LonghornRobustness
  size_bytes: number
  size_display: string
  replica_count_target: number
  replicas: VolumeReplica[]
  attached_to_node: string | null
  attached_to_pod: string | null
  created_at: string | null
  data_engine: string | null
}

export async function GET() {
  return inventoryResponse('volumes', process.env.LONGHORN_NAMESPACE || 'longhorn-system')
}
