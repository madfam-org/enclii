/**
 * Client-side fetcher for the ArgoCD applications endpoint.
 */
import type { ArgoApplicationSummary } from '@/app/api/argocd/applications/route'

export interface ArgoApplicationsResponse {
  applications: ArgoApplicationSummary[]
  synced_at: string
}

export async function fetchArgoApplications(): Promise<ArgoApplicationsResponse> {
  const res = await fetch('/api/argocd/applications', { cache: 'no-store' })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || `ArgoCD fetch failed: ${res.status}`)
  }
  return (await res.json()) as ArgoApplicationsResponse
}

export async function syncArgoApplication(name: string): Promise<void> {
  const res = await fetch(`/api/argocd/applications/${encodeURIComponent(name)}/sync`, {
    method: 'POST',
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || `Sync failed: ${res.status}`)
  }
}

export type { ArgoApplicationSummary }
