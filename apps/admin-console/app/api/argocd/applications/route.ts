import { inventoryResponse } from '@/lib/inventory-proxy'

export const dynamic = 'force-dynamic'

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

export async function GET() {
  return inventoryResponse('applications', process.env.ARGOCD_NAMESPACE || 'argocd')
}
