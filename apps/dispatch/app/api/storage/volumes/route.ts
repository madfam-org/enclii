/**
 * GET /api/storage/volumes
 *
 * Lists all Longhorn volumes via the K8s API server (custom resource).
 * Cached 5min. Auth: middleware enforces admin/operator + allowed domain.
 */
import { NextResponse } from 'next/server'
import { cached, customObjectsApi, formatBytes, parseMemory } from '@/lib/k8s-client'

export const dynamic = 'force-dynamic'

const LONGHORN_GROUP = 'longhorn.io'
const LONGHORN_VERSION = 'v1beta2'
const LONGHORN_NAMESPACE = process.env.LONGHORN_NAMESPACE || 'longhorn-system'
const VOLUMES_PLURAL = 'volumes'

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

interface LonghornVolumeItem {
  metadata?: {
    name?: string
    namespace?: string
    creationTimestamp?: string
  }
  spec?: {
    size?: string
    numberOfReplicas?: number
    dataEngine?: string
  }
  status?: {
    state?: string
    robustness?: string
    currentNodeID?: string
    kubernetesStatus?: {
      pvcName?: string
      namespace?: string
      workloadsStatus?: { podName?: string; podStatus?: string; workloadName?: string }[]
    }
  }
}

interface LonghornReplicaItem {
  metadata?: { name?: string }
  spec?: { volumeName?: string; nodeID?: string }
  status?: { currentState?: string; mode?: string }
}

const ROBUSTNESS_RANK: Record<LonghornRobustness, number> = {
  faulted: 0,
  degraded: 1,
  healthy: 2,
  unknown: 3,
}

async function fetchVolumes(): Promise<LonghornVolumeSummary[]> {
  const api = customObjectsApi()

  const [volRes, repRes] = await Promise.all([
    api.listNamespacedCustomObject({
      group: LONGHORN_GROUP,
      version: LONGHORN_VERSION,
      namespace: LONGHORN_NAMESPACE,
      plural: VOLUMES_PLURAL,
    }) as Promise<{ items?: LonghornVolumeItem[] }>,
    api.listNamespacedCustomObject({
      group: LONGHORN_GROUP,
      version: LONGHORN_VERSION,
      namespace: LONGHORN_NAMESPACE,
      plural: 'replicas',
    }) as Promise<{ items?: LonghornReplicaItem[] }>,
  ])

  const replicasByVolume = new Map<string, VolumeReplica[]>()
  for (const r of repRes.items ?? []) {
    const volName = r.spec?.volumeName ?? ''
    const list = replicasByVolume.get(volName) ?? []
    list.push({
      name: r.metadata?.name ?? '',
      node: r.spec?.nodeID ?? '',
      running: r.status?.currentState === 'running',
      mode: r.status?.mode ?? '',
    })
    replicasByVolume.set(volName, list)
  }

  return (volRes.items ?? []).map((v) => {
    const name = v.metadata?.name ?? ''
    const sizeBytes = parseMemory(v.spec?.size)
    const k8sStatus = v.status?.kubernetesStatus ?? {}
    const firstWorkload = k8sStatus.workloadsStatus?.[0]
    return {
      name,
      namespace: k8sStatus.namespace ?? null,
      pvc_name: k8sStatus.pvcName ?? null,
      state: ((v.status?.state ?? 'unknown') as string).toLowerCase() as LonghornState,
      robustness: ((v.status?.robustness ?? 'unknown') as string).toLowerCase() as LonghornRobustness,
      size_bytes: sizeBytes,
      size_display: formatBytes(sizeBytes),
      replica_count_target: v.spec?.numberOfReplicas ?? 0,
      replicas: replicasByVolume.get(name) ?? [],
      attached_to_node: v.status?.currentNodeID ?? null,
      attached_to_pod: firstWorkload?.podName ?? null,
      created_at: v.metadata?.creationTimestamp ?? null,
      data_engine: v.spec?.dataEngine ?? null,
    }
  })
}

export async function GET() {
  try {
    const volumes = await cached('storage:volumes:v1', 300_000, fetchVolumes)
    // Sort: faulted first, then degraded, then healthy
    const sorted = [...volumes].sort((a, b) => {
      const rankA = ROBUSTNESS_RANK[a.robustness] ?? 99
      const rankB = ROBUSTNESS_RANK[b.robustness] ?? 99
      if (rankA !== rankB) return rankA - rankB
      return a.name.localeCompare(b.name)
    })

    const summary = {
      total: volumes.length,
      healthy: volumes.filter((v) => v.robustness === 'healthy').length,
      degraded: volumes.filter((v) => v.robustness === 'degraded').length,
      faulted: volumes.filter((v) => v.robustness === 'faulted').length,
      total_bytes: volumes.reduce((s, v) => s + v.size_bytes, 0),
    }

    return NextResponse.json({
      volumes: sorted,
      summary: {
        ...summary,
        total_size_display: formatBytes(summary.total_bytes),
      },
      synced_at: new Date().toISOString(),
    })
  } catch (error) {
    console.error('[Dispatch /api/storage/volumes] error:', error)
    return NextResponse.json(
      { error: error instanceof Error ? error.message : 'Failed to fetch volumes' },
      { status: 500 }
    )
  }
}
