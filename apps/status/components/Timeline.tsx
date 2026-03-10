'use client'

import { useMemo, useState, useCallback } from 'react'
import { cn } from '@/lib/utils'
import type { TimelineSlot } from '@/lib/types'
import { STATUS_COLORS, groupByKey } from '@/lib/status-config'
import { Clock, Activity } from 'lucide-react'
import { useTimelineBreakpoint } from '@/hooks/useTimelineBreakpoint'
import { useTimelineData } from '@/hooks/useTimelineData'
import { CanvasTimelineBar } from './CanvasTimelineBar'
import { SharedTooltip } from './SharedTooltip'
import type { TooltipAnchor } from './SharedTooltip'

function formatSlotTime(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

// ---- Time Labels (lightweight DOM — no change) ----

interface TimelineTimeLabelsProps {
  from: string
  to: string
}

function TimelineTimeLabels({ from, to }: TimelineTimeLabelsProps) {
  const fromDate = new Date(from)
  const toDate = new Date(to)
  const labels: string[] = []

  const first = new Date(fromDate)
  first.setMinutes(0, 0, 0)
  first.setHours(Math.ceil(first.getHours() / 6) * 6)

  for (let t = first.getTime(); t <= toDate.getTime(); t += 6 * 60 * 60 * 1000) {
    labels.push(formatSlotTime(new Date(t).toISOString()))
  }

  return (
    <div className="flex justify-between text-xs text-muted-foreground mt-1.5 px-0.5">
      <span>{formatSlotTime(from)}</span>
      {labels.map((label, i) => (
        <span key={i} className="hidden sm:inline">{label}</span>
      ))}
      <span>Now</span>
    </div>
  )
}

// ---- Legend (lightweight DOM — no change) ----

interface TimelineLegendProps {
  className?: string
}

function TimelineLegend({ className }: TimelineLegendProps) {
  const items = [
    { status: 'operational' as const, label: 'Operational' },
    { status: 'degraded' as const, label: 'Degraded' },
    { status: 'outage' as const, label: 'Outage' },
    { status: 'unknown' as const, label: 'No Data' },
  ]

  return (
    <div className={cn('flex flex-wrap gap-3 text-xs', className)}>
      {items.map(({ status, label }) => (
        <div key={status} className="flex items-center gap-1.5">
          <div className={cn('size-2.5 rounded-sm', STATUS_COLORS[status])} />
          <span className="text-muted-foreground">{label}</span>
        </div>
      ))}
    </div>
  )
}

// ---- Main Timeline Component ----

export function Timeline() {
  const breakpoint = useTimelineBreakpoint()
  const { data, loading, error } = useTimelineData(breakpoint.windowMinutes)

  // Shared tooltip state
  const [tooltipVisible, setTooltipVisible] = useState(false)
  const [tooltipSlot, setTooltipSlot] = useState<TimelineSlot | null>(null)
  const [tooltipAnchor, setTooltipAnchor] = useState<TooltipAnchor | null>(null)

  const handleSlotHover = useCallback((slot: TimelineSlot, anchor: TooltipAnchor) => {
    setTooltipSlot(slot)
    setTooltipAnchor(anchor)
    setTooltipVisible(true)
  }, [])

  const handleSlotLeave = useCallback(() => {
    setTooltipVisible(false)
  }, [])

  // Sparse data check — memoized to avoid recomputing every render
  const sparseData = useMemo(() => {
    if (!data || data.services.length === 0) return false
    let total = 0
    let unknownCount = 0
    for (const svc of data.services) {
      for (const slot of svc.slots) {
        total++
        if (slot.status === 'unknown') unknownCount++
      }
    }
    return total > 0 && unknownCount / total > 0.5
  }, [data])

  // Groups — memoized
  const groups = useMemo(() => {
    if (!data) return null
    return groupByKey(data.services, (s) => s.group)
  }, [data])

  if (loading) {
    return (
      <div className="space-y-4 animate-pulse">
        <div className="h-5 bg-muted rounded w-48" />
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="space-y-2">
            <div className="h-4 bg-muted rounded w-32" />
            <div className="h-6 bg-muted rounded" />
          </div>
        ))}
      </div>
    )
  }

  if (error || !data) {
    return (
      <div className="text-sm text-muted-foreground flex items-center gap-2">
        <Clock className="size-4" />
        <span>Timeline data not yet available. History recording starts after first cron run.</span>
      </div>
    )
  }

  if (data.services.length === 0) {
    return (
      <div className="text-sm text-muted-foreground flex items-center gap-2">
        <Clock className="size-4" />
        <span>No history recorded yet. Timeline will populate within a few minutes.</span>
      </div>
    )
  }

  return (
    <div
      className="w-screen relative left-1/2 -ml-[50vw] px-4 sm:px-6 lg:px-10 xl:px-16"
      role="region"
      aria-label="24-Hour Status Timeline"
    >
      <div className="max-w-[2400px] mx-auto space-y-6">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Activity className="size-5 text-muted-foreground" />
            <h2 className="text-xl font-semibold">24-Hour Timeline</h2>
          </div>
          <TimelineLegend />
        </div>

        {sparseData && (
          <p className="text-xs text-muted-foreground flex items-center gap-1.5">
            <Clock className="size-3.5" />
            History is still accumulating — the timeline will fill in over the next 24 hours.
          </p>
        )}

        {groups && Array.from(groups).map(([group, timelines]) => (
          <div key={group} className="space-y-3">
            <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wide">
              {group}
            </h3>
            <div className="border rounded-lg bg-card p-4 space-y-4">
              {timelines.map((tl) => (
                <CanvasTimelineBar
                  key={tl.service}
                  timeline={tl}
                  onSlotHover={handleSlotHover}
                  onSlotLeave={handleSlotLeave}
                />
              ))}
              <TimelineTimeLabels from={data.from} to={data.to} />
            </div>
          </div>
        ))}

        <SharedTooltip
          visible={tooltipVisible}
          slot={tooltipSlot}
          anchor={tooltipAnchor}
        />
      </div>
    </div>
  )
}
