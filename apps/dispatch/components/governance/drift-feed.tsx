'use client'

import { useEffect, useState } from 'react'
import { driftApi } from '@/lib/admin-api'
import type { DriftEvent } from '@/types/admin'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { AlertTriangle, CheckCircle, GitBranch, Cloud, ChevronDown, ChevronRight } from 'lucide-react'

const severityColors: Record<string, string> = {
  low: 'bg-blue-500/20 text-blue-400',
  medium: 'bg-amber-500/20 text-amber-400',
  high: 'bg-orange-500/20 text-orange-400',
  critical: 'bg-red-500/20 text-red-400',
}

function relativeTime(dateStr: string | undefined): string {
  if (!dateStr) return ''
  const diff = Date.now() - new Date(dateStr).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

export function DriftFeed() {
  const [events, setEvents] = useState<DriftEvent[]>([])
  const [loading, setLoading] = useState(true)
  const [expandedId, setExpandedId] = useState<string | null>(null)

  const fetchEvents = () => {
    setLoading(true)
    driftApi.list().then((d) => setEvents(d.events || [])).finally(() => setLoading(false))
  }

  useEffect(() => { fetchEvents() }, [])

  const handleResolve = async (id: string) => {
    await driftApi.resolve(id)
    fetchEvents()
  }

  return (
    <div>
      <h3 className="text-lg font-semibold mb-3 flex items-center gap-2"><AlertTriangle className="size-5" /> Drift Events</h3>
      {loading ? (
        <div className="flex justify-center py-6"><div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" /></div>
      ) : events.length === 0 ? (
        <div className="rounded-lg border border-border bg-card/50 p-6 text-center">
          <CheckCircle className="size-8 mx-auto mb-2 text-green-400" />
          <p className="text-muted-foreground">No drift events detected.</p>
        </div>
      ) : (
        <div className="space-y-2">
          {events.map((e) => {
            const hasDetails = e.drift_details && Object.keys(e.drift_details).length > 0
            const isExpanded = expandedId === e.id
            return (
              <div key={e.id} className="rounded-lg border border-border bg-card/50">
                <div className="p-4 flex items-center justify-between">
                  <div className="flex items-center gap-3">
                    {e.source === 'argocd' ? <GitBranch className="size-5 text-blue-400" /> : <Cloud className="size-5 text-purple-400" />}
                    <div>
                      <p className="font-mono text-sm">{e.resource_name}</p>
                      <p className="text-xs text-muted-foreground">
                        {e.resource_type} • {e.source}
                        {e.detected_at && <> • detected {relativeTime(e.detected_at)}</>}
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge className={severityColors[e.severity]}>{e.severity}</Badge>
                    {hasDetails && (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-7 w-7 p-0"
                        onClick={() => setExpandedId(isExpanded ? null : e.id)}
                      >
                        {isExpanded ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
                      </Button>
                    )}
                    {!e.resolved && (
                      <Button variant="outline" size="sm" onClick={() => handleResolve(e.id)}>Resolve</Button>
                    )}
                    {e.resolved && <Badge variant="outline" className="text-green-400">Resolved</Badge>}
                  </div>
                </div>
                {isExpanded && hasDetails && (
                  <div className="px-4 pb-4">
                    <pre className="text-xs bg-muted/30 rounded p-2 overflow-auto max-h-40">
                      {JSON.stringify(e.drift_details, null, 2)}
                    </pre>
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
