'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import { ChevronDown, ChevronRight, RefreshCw, Shield } from 'lucide-react'
import {
  fetchNetworkPolicies,
  type NetworkPoliciesResponse,
  type NetworkPolicySummary,
} from '@/lib/network-policies-api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/empty-state'

const POLL_INTERVAL_MS = 5 * 60_000

function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const m = Math.floor(diff / 60_000)
  if (m < 1) return 'just now'
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

export function PoliciesTree() {
  const [data, setData] = useState<NetworkPoliciesResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState('')
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true)
    try {
      const fresh = await fetchNetworkPolicies()
      setData(fresh)
      setError(null)
      // Auto-expand if filter is active
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load network policies')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
    const id = setInterval(() => load(true), POLL_INTERVAL_MS)
    return () => clearInterval(id)
  }, [load])

  const filteredGroups = useMemo(() => {
    if (!data) return []
    const f = filter.trim().toLowerCase()
    if (!f) return data.groups
    return data.groups
      .map((g) => ({
        namespace: g.namespace,
        policies: g.policies.filter(
          (p) =>
            p.name.toLowerCase().includes(f) ||
            p.namespace.toLowerCase().includes(f) ||
            p.pod_selector_summary.toLowerCase().includes(f)
        ),
      }))
      .filter((g) => g.namespace.toLowerCase().includes(f) || g.policies.length > 0)
  }, [data, filter])

  if (loading && !data) {
    return (
      <div className="flex justify-center py-12">
        <div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  if (error && !data) {
    return <EmptyState icon={Shield} title="Network policies unavailable" description={error} />
  }

  if (!data || data.groups.length === 0) {
    return (
      <EmptyState
        icon={Shield}
        title="No NetworkPolicies"
        description="No NetworkPolicies are currently defined in the cluster."
      />
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3 rounded-md border border-border bg-card/30 p-3">
        <span className="text-sm">
          <span className="font-semibold">{data.total_policies}</span>{' '}
          <span className="text-muted-foreground">policies across</span>{' '}
          <span className="font-semibold">{data.total_namespaces}</span>{' '}
          <span className="text-muted-foreground">namespaces</span>
        </span>
        <input
          type="text"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter by namespace, name, or selector..."
          className="ml-auto rounded-md border border-border bg-background px-3 py-1.5 text-sm w-64 focus:outline-none focus:ring-2 focus:ring-ring"
          aria-label="Filter network policies"
        />
        <Badge variant="secondary" className="font-mono text-xs">
          synced {relativeTime(data.synced_at)}
        </Badge>
        <Button size="sm" variant="outline" onClick={() => load()}>
          <RefreshCw className="size-3.5 mr-1" />
          Refresh
        </Button>
      </div>

      {error && (
        <div className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-400">
          Last refresh failed: {error}. Showing cached data.
        </div>
      )}

      <div className="space-y-3">
        {filteredGroups.map((group) => {
          const isExpanded = expanded[group.namespace] ?? !!filter
          return (
            <div key={group.namespace} className="rounded-lg border border-border bg-card/40">
              <button
                type="button"
                onClick={() =>
                  setExpanded((prev) => ({
                    ...prev,
                    [group.namespace]: !isExpanded,
                  }))
                }
                className="w-full flex items-center gap-2 p-3 text-left hover:bg-muted/30"
                aria-expanded={isExpanded}
              >
                {isExpanded ? (
                  <ChevronDown className="size-4" />
                ) : (
                  <ChevronRight className="size-4" />
                )}
                <span className="font-mono text-sm font-semibold">{group.namespace}</span>
                <Badge variant="outline" className="text-xs ml-auto">
                  {group.policies.length} {group.policies.length === 1 ? 'policy' : 'policies'}
                </Badge>
              </button>
              {isExpanded && (
                <div className="border-t border-border divide-y divide-border">
                  {group.policies.map((p) => (
                    <PolicyCard key={`${p.namespace}/${p.name}`} policy={p} />
                  ))}
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function PolicyCard({ policy }: { policy: NetworkPolicySummary }) {
  return (
    <div className="p-3 text-sm">
      <div className="flex items-center gap-2 mb-2">
        <span className="font-mono font-semibold">{policy.name}</span>
        {policy.policy_types.map((t) => (
          <Badge key={t} variant="outline" className="text-[10px]">
            {t}
          </Badge>
        ))}
        <span className="ml-auto text-xs text-muted-foreground font-mono">
          podSelector: {policy.pod_selector_summary}
        </span>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
        <div>
          <div className="text-muted-foreground mb-1 flex items-center gap-1">
            Ingress <Badge variant="outline" className="text-[10px]">{policy.ingress_rules}</Badge>
          </div>
          {policy.ingress_summary.length === 0 ? (
            <div className="text-muted-foreground/70 italic">
              {policy.policy_types.includes('Ingress') ? 'deny all' : 'not enforced'}
            </div>
          ) : (
            <ul className="space-y-1 font-mono">
              {policy.ingress_summary.map((r, i) => (
                <li key={i} className="rounded border border-border bg-background/50 px-2 py-1 break-all">
                  {r}
                </li>
              ))}
            </ul>
          )}
        </div>
        <div>
          <div className="text-muted-foreground mb-1 flex items-center gap-1">
            Egress <Badge variant="outline" className="text-[10px]">{policy.egress_rules}</Badge>
          </div>
          {policy.egress_summary.length === 0 ? (
            <div className="text-muted-foreground/70 italic">
              {policy.policy_types.includes('Egress') ? 'deny all' : 'not enforced'}
            </div>
          ) : (
            <ul className="space-y-1 font-mono">
              {policy.egress_summary.map((r, i) => (
                <li key={i} className="rounded border border-border bg-background/50 px-2 py-1 break-all">
                  {r}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  )
}
