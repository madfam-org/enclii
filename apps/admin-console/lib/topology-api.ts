/**
 * Client-side fetcher for the live cluster topology served by /api/topology.
 *
 * Separate from `admin-api.ts`, which proxies to the legacy
 * `/api/admin/topology` Switchyard endpoint that returns stub-shaped data.
 */
import type { TopologyResponse } from '@/lib/topology-types'

export class TopologyFetchError extends Error {
  constructor(message: string, readonly status: number) {
    super(message)
    this.name = 'TopologyFetchError'
  }
}

export async function fetchTopology(): Promise<TopologyResponse> {
  const res = await fetch('/api/topology', { cache: 'no-store' })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new TopologyFetchError(body.error || `Topology fetch failed: ${res.status}`, res.status)
  }
  return (await res.json()) as TopologyResponse
}

export type { TopologyResponse }
