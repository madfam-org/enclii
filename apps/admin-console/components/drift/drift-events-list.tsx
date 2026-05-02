'use client'

import { useEffect, useState } from 'react'
import { driftApi } from '@/lib/admin-api'
import type { DriftEvent } from '@/types/admin'
import { Badge } from '@enclii/ui-components/badge'
import { Button } from '@enclii/ui-components/button'
import { AlertTriangle, CheckCircle2, Wifi, Clock, Filter } from 'lucide-react'

const severityColors: Record<string, string> = {
  critical: 'bg-red-500/20 text-red-400 border-red-500/30',
  high: 'bg-orange-500/20 text-orange-400 border-orange-500/30',
  medium: 'bg-amber-500/20 text-amber-400 border-amber-500/30',
  low: 'bg-blue-500/20 text-blue-400 border-blue-500/30',
}

const sourceIcons: Record<string, React.ElementType> = {
  argocd: Wifi,
  crossplane: Wifi,
  manual: AlertTriangle,
}

function relativeTime(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

export function DriftEventsList() {
  const [events, setEvents] = useState<DriftEvent[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<'all' | 'unresolved' | 'resolved'>('unresolved')
  const [resolvingId, setResolvingId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const fetchEvents = async () => {
    setLoading(true)
    setError(null)
    try {
      const resolved = filter === 'all' ? undefined : filter === 'resolved'
      const data = await driftApi.list(resolved)
      setEvents(data.events || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load drift events')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchEvents()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter])

  const handleResolve = async (id: string) => {
    setResolvingId(id)
    try {
      await driftApi.resolve(id)
      await fetchEvents()
    } catch {
      // error handled by adminFetch
    } finally {
      setResolvingId(null)
    }
  }

  const counts = {
    critical: events.filter((e) => e.severity === 'critical' && !e.resolved).length,
    high: events.filter((e) => e.severity === 'high' && !e.resolved).length,
    unresolved: events.filter((e) => !e.resolved).length,
  }

  return (
    <div className="space-y-6">
      {/* Summary bar */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        {[
          { label: 'Unresolved', value: counts.unresolved, color: 'text-foreground' },
          { label: 'Critical', value: counts.critical, color: 'text-red-400' },
          { label: 'High', value: counts.high, color: 'text-orange-400' },
          { label: 'Total', value: events.length, color: 'text-muted-foreground' },
        ].map((s) => (
          <div key={s.label} className="rounded-lg border border-border bg-card/50 p-3 text-center">
            <p className={`text-2xl font-bold font-mono ${s.color}`}>{s.value}</p>
            <p className="text-xs text-muted-foreground mt-1">{s.label}</p>
          </div>
        ))}
      </div>

      {/* Filter tabs */}
      <div className="flex items-center gap-1 border-b border-border pb-3">
        <Filter className="size-4 text-muted-foreground mr-2" />
        {(['unresolved', 'all', 'resolved'] as const).map((f) => (
          <button
            key={f}
            onClick={() => setFilter(f)}
            className={`px-3 py-1 rounded-md text-sm font-medium capitalize transition-colors ${
              filter === f
                ? 'bg-primary/10 text-primary'
                : 'text-muted-foreground hover:text-foreground hover:bg-muted/50'
            }`}
          >
            {f}
          </button>
        ))}
      </div>

      {/* Event list */}
      {loading ? (
        <div className="flex justify-center py-12">
          <div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" />
        </div>
      ) : error ? (
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
          {error}
        </div>
      ) : events.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <CheckCircle2 className="size-10 text-green-400 mb-3" />
          <p className="font-medium">No drift events</p>
          <p className="text-sm text-muted-foreground mt-1">
            {filter === 'unresolved' ? 'All resources are in sync.' : 'No events found for this filter.'}
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {events.map((event) => {
            const SourceIcon = sourceIcons[event.source] ?? AlertTriangle
            return (
              <div
                key={event.id}
                className={`rounded-lg border bg-card/50 p-4 flex items-start gap-4 ${
                  event.resolved ? 'opacity-60' : ''
                }`}
              >
                <div className="mt-0.5">
                  <SourceIcon className="size-4 text-muted-foreground" />
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex flex-wrap items-center gap-2 mb-1">
                    <span className="font-medium text-sm">{event.resource_name}</span>
                    <span className="text-xs text-muted-foreground font-mono">{event.resource_type}</span>
                    <Badge className={`text-xs border ${severityColors[event.severity]}`}>
                      {event.severity}
                    </Badge>
                    {event.resolved && (
                      <Badge className="text-xs bg-green-500/20 text-green-400 border-green-500/30">
                        resolved
                      </Badge>
                    )}
                  </div>
                  <div className="flex items-center gap-3 text-xs text-muted-foreground">
                    <span className="capitalize">{event.source}</span>
                    {event.cluster_id && <span>cluster: {event.cluster_id.slice(0, 8)}…</span>}
                    <span className="flex items-center gap-1">
                      <Clock className="size-3" />
                      {relativeTime(event.detected_at)}
                    </span>
                  </div>
                </div>
                {!event.resolved && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => handleResolve(event.id)}
                    disabled={resolvingId === event.id}
                    className="shrink-0"
                  >
                    {resolvingId === event.id ? 'Resolving…' : 'Resolve'}
                  </Button>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
