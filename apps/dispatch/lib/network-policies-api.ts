/**
 * Client-side fetcher for NetworkPolicy listing.
 */
import type { NetworkPolicySummary } from '@/app/api/network-policies/route'

export interface NetworkPoliciesResponse {
  groups: { namespace: string; policies: NetworkPolicySummary[] }[]
  total_policies: number
  total_namespaces: number
  synced_at: string
}

export async function fetchNetworkPolicies(): Promise<NetworkPoliciesResponse> {
  const res = await fetch('/api/network-policies', { cache: 'no-store' })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || `NetworkPolicy fetch failed: ${res.status}`)
  }
  return (await res.json()) as NetworkPoliciesResponse
}

export type { NetworkPolicySummary }
