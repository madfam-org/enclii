/**
 * GET /api/argocd/applications
 *
 * Lists all ArgoCD Applications via the K8s API server (custom resource).
 * Cached 30s. Auth: middleware enforces admin/operator + allowed domain.
 */
import { NextResponse } from 'next/server'
import { cached, customObjectsApi } from '@/lib/k8s-client'

export const dynamic = 'force-dynamic'

const ARGO_GROUP = 'argoproj.io'
const ARGO_VERSION = 'v1alpha1'
const ARGO_NAMESPACE = process.env.ARGOCD_NAMESPACE || 'argocd'
const APPLICATIONS_PLURAL = 'applications'

export interface ArgoApplicationSummary {
  name: string
  namespace: string
  sync_status: 'Synced' | 'OutOfSync' | 'Unknown'
  health_status: 'Healthy' | 'Degraded' | 'Progressing' | 'Suspended' | 'Missing' | 'Unknown'
  last_sync_at: string | null
  target_revision: string | null
  current_revision: string | null
  source_repo: string | null
  source_path: string | null
  destination_namespace: string | null
  destination_server: string | null
  conditions: { type: string; message: string }[]
  message: string | null
}

interface ArgoApplicationItem {
  metadata?: { name?: string; namespace?: string }
  spec?: {
    source?: { repoURL?: string; path?: string; targetRevision?: string }
    destination?: { namespace?: string; server?: string }
  }
  status?: {
    sync?: { status?: string; revision?: string }
    health?: { status?: string; message?: string }
    operationState?: { finishedAt?: string; startedAt?: string }
    conditions?: { type?: string; message?: string }[]
  }
}

async function fetchApplications(): Promise<ArgoApplicationSummary[]> {
  const api = customObjectsApi()
  const list = (await api.listNamespacedCustomObject({
    group: ARGO_GROUP,
    version: ARGO_VERSION,
    namespace: ARGO_NAMESPACE,
    plural: APPLICATIONS_PLURAL,
  })) as { items?: ArgoApplicationItem[] }

  return (list.items ?? []).map((item) => {
    const status = item.status ?? {}
    const sync = status.sync ?? {}
    const health = status.health ?? {}
    const op = status.operationState ?? {}

    return {
      name: item.metadata?.name ?? '',
      namespace: item.metadata?.namespace ?? ARGO_NAMESPACE,
      sync_status: (sync.status as ArgoApplicationSummary['sync_status']) ?? 'Unknown',
      health_status: (health.status as ArgoApplicationSummary['health_status']) ?? 'Unknown',
      last_sync_at: op.finishedAt ?? op.startedAt ?? null,
      target_revision: item.spec?.source?.targetRevision ?? null,
      current_revision: sync.revision ?? null,
      source_repo: item.spec?.source?.repoURL ?? null,
      source_path: item.spec?.source?.path ?? null,
      destination_namespace: item.spec?.destination?.namespace ?? null,
      destination_server: item.spec?.destination?.server ?? null,
      conditions:
        status.conditions
          ?.filter((c) => c.type)
          .map((c) => ({ type: c.type ?? '', message: c.message ?? '' })) ?? [],
      message: health.message ?? null,
    }
  })
}

export async function GET() {
  try {
    const applications = await cached('argocd:applications:v1', 30_000, fetchApplications)
    return NextResponse.json({
      applications: applications.sort((a, b) => a.name.localeCompare(b.name)),
      synced_at: new Date().toISOString(),
    })
  } catch (error) {
    console.error('[Dispatch /api/argocd/applications] error:', error)
    return NextResponse.json(
      { error: error instanceof Error ? error.message : 'Failed to fetch applications' },
      { status: 500 }
    )
  }
}
