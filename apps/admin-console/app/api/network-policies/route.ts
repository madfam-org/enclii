/**
 * GET /api/network-policies
 *
 * Lists all NetworkPolicies cluster-wide via the in-cluster ServiceAccount.
 * Read-only — NetworkPolicies are managed by switchyard-api per the
 * platform's zero-touch onboarding policy.
 *
 * Cached 5min. Auth: middleware enforces admin/operator + allowed domain.
 */
import { NextResponse } from 'next/server'
import { cached, networkingApi } from '@/lib/k8s-client'

export const dynamic = 'force-dynamic'

export interface NetworkPolicySummary {
  name: string
  namespace: string
  pod_selector: Record<string, string> | null
  pod_selector_summary: string
  policy_types: string[]
  ingress_rules: number
  egress_rules: number
  ingress_summary: string[]
  egress_summary: string[]
  created_at: string | null
}

interface PeerLike {
  podSelector?: { matchLabels?: Record<string, string> }
  namespaceSelector?: { matchLabels?: Record<string, string> }
  ipBlock?: { cidr?: string; except?: string[] }
}

interface PortLike {
  port?: number | string
  protocol?: string
}

function summarizeSelector(sel: Record<string, string> | null | undefined): string {
  if (!sel || Object.keys(sel).length === 0) return 'all pods'
  return Object.entries(sel)
    .map(([k, v]) => `${k}=${v}`)
    .join(',')
}

function summarizePeer(peer: PeerLike): string {
  if (peer.ipBlock) {
    return peer.ipBlock.except?.length
      ? `${peer.ipBlock.cidr ?? '?'} (except ${peer.ipBlock.except.join(',')})`
      : (peer.ipBlock.cidr ?? '?')
  }
  const parts: string[] = []
  if (peer.namespaceSelector) {
    parts.push(`ns:${summarizeSelector(peer.namespaceSelector.matchLabels)}`)
  }
  if (peer.podSelector) {
    parts.push(`pod:${summarizeSelector(peer.podSelector.matchLabels)}`)
  }
  return parts.join(' ') || 'unrestricted'
}

function summarizePorts(ports: PortLike[] | undefined): string {
  if (!ports || ports.length === 0) return 'any-port'
  return ports
    .map((p) => `${p.protocol ?? 'TCP'}/${p.port ?? 'any'}`)
    .join(',')
}

interface RawNetworkPolicy {
  metadata?: { name?: string; namespace?: string; creationTimestamp?: string | Date }
  spec?: {
    podSelector?: { matchLabels?: Record<string, string> }
    policyTypes?: string[]
    ingress?: { from?: PeerLike[]; ports?: PortLike[] }[]
    egress?: { to?: PeerLike[]; ports?: PortLike[] }[]
  }
}

async function fetchPolicies(): Promise<NetworkPolicySummary[]> {
  const api = networkingApi()
  const result = (await api.listNetworkPolicyForAllNamespaces()) as
    | { body?: { items?: RawNetworkPolicy[] }; items?: RawNetworkPolicy[] }
  const items: RawNetworkPolicy[] = result.body?.items ?? result.items ?? []
  return items.map((np) => {
    const ingress = np.spec?.ingress ?? []
    const egress = np.spec?.egress ?? []
    const ingressSummary = ingress.map((rule) => {
      const peers = (rule.from ?? []).map((p) => summarizePeer(p as PeerLike)).join(' OR ') || 'all sources'
      const ports = summarizePorts(rule.ports as PortLike[] | undefined)
      return `${peers} -> ${ports}`
    })
    const egressSummary = egress.map((rule) => {
      const peers = (rule.to ?? []).map((p) => summarizePeer(p as PeerLike)).join(' OR ') || 'all destinations'
      const ports = summarizePorts(rule.ports as PortLike[] | undefined)
      return `${ports} -> ${peers}`
    })
    const podSelectorMatch = np.spec?.podSelector?.matchLabels ?? null
    return {
      name: np.metadata?.name ?? '',
      namespace: np.metadata?.namespace ?? '',
      pod_selector: podSelectorMatch && Object.keys(podSelectorMatch).length > 0 ? podSelectorMatch : null,
      pod_selector_summary: summarizeSelector(podSelectorMatch),
      policy_types: np.spec?.policyTypes ?? [],
      ingress_rules: ingress.length,
      egress_rules: egress.length,
      ingress_summary: ingressSummary,
      egress_summary: egressSummary,
      created_at: np.metadata?.creationTimestamp
        ? new Date(np.metadata.creationTimestamp).toISOString()
        : null,
    }
  })
}

export async function GET() {
  try {
    const policies = await cached('network-policies:v1', 300_000, fetchPolicies)
    // Group by namespace
    const byNamespace = new Map<string, NetworkPolicySummary[]>()
    for (const p of policies) {
      const list = byNamespace.get(p.namespace) ?? []
      list.push(p)
      byNamespace.set(p.namespace, list)
    }
    const grouped = Array.from(byNamespace.entries())
      .map(([namespace, items]) => ({
        namespace,
        policies: items.sort((a, b) => a.name.localeCompare(b.name)),
      }))
      .sort((a, b) => a.namespace.localeCompare(b.namespace))

    return NextResponse.json({
      groups: grouped,
      total_policies: policies.length,
      total_namespaces: grouped.length,
      synced_at: new Date().toISOString(),
    })
  } catch (error) {
    console.error('[Dispatch /api/network-policies] error:', error)
    return NextResponse.json(
      { error: error instanceof Error ? error.message : 'Failed to fetch network policies' },
      { status: 500 }
    )
  }
}
