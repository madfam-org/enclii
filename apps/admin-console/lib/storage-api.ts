/**
 * Client-side fetcher for Longhorn volume health.
 */
import type { LonghornVolumeSummary } from '@/app/api/storage/volumes/route'

export interface StorageVolumesResponse {
  volumes: LonghornVolumeSummary[]
  summary: {
    total: number
    healthy: number
    degraded: number
    faulted: number
    total_bytes: number
    total_size_display: string
  }
  synced_at: string
}

export async function fetchStorageVolumes(): Promise<StorageVolumesResponse> {
  const res = await fetch('/api/storage/volumes', { cache: 'no-store' })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || `Storage fetch failed: ${res.status}`)
  }
  return (await res.json()) as StorageVolumesResponse
}

export type { LonghornVolumeSummary }
