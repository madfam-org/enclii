'use client'

import { useEffect, useRef, useState } from 'react'
import { cn } from '@/lib/utils'
import { formatResponseTime } from '@/lib/utils'
import type { ServiceStatus, TimelineResponse, ServiceTimeline, TimelineSlot } from '@/lib/types'
import { STATUS_COLORS, STATUS_LABELS, groupByKey } from '@/lib/status-config'
import { Clock, Activity } from 'lucide-react'
import { useTimelineBreakpoint } from '@/hooks/useTimelineBreakpoint'

function formatSlotTime(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

interface TimelineBarProps {
  timeline: ServiceTimeline
  gapClass: string
}

function TimelineBar({ timeline, gapClass }: TimelineBarProps) {
  const { service, slots, uptime24h } = timeline

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between">
        <a
          href={timeline.href || timeline.url}
          target="_blank"
          rel="noopener noreferrer"
          className="text-sm font-medium truncate hover:underline hover:text-primary transition-colors"
        >
          {service}
        </a>
        <span
          className={cn(
            'text-xs font-mono',
            uptime24h >= 99.9
              ? 'text-status-operational'
              : uptime24h >= 99
                ? 'text-status-degraded'
                : 'text-status-outage',
          )}
        >
          {uptime24h.toFixed(2)}%
        </span>
      </div>
      <div className={cn('flex h-6', gapClass)}>
        {slots.map((slot, index) => (
          <SlotCell key={slot.start} slot={slot} index={index} total={slots.length} />
        ))}
      </div>
    </div>
  )
}

interface SlotCellProps {
  slot: TimelineSlot
  index: number
  total: number
}

function SlotCell({ slot, index, total }: SlotCellProps) {
  const timeRange = `${formatSlotTime(slot.start)} – ${formatSlotTime(slot.end)}`
  const ariaLabel = `${timeRange}: ${STATUS_LABELS[slot.status]}${slot.checks > 0 ? `, ${slot.checks} check${slot.checks !== 1 ? 's' : ''}` : ''}`

  return (
    <div
      className="relative flex-1 group"
      style={{ minWidth: '1px' }}
      role="img"
      aria-label={ariaLabel}
    >
      <div
        className={cn(
          'h-full rounded-[1px] transition-all duration-100',
          'group-hover:brightness-125 group-hover:scale-y-125',
          STATUS_COLORS[slot.status],
        )}
      />
      {/* Tooltip */}
      <div
        className={cn(
          'absolute z-30 bottom-full mb-2 opacity-0 group-hover:opacity-100 transition-opacity',
          'pointer-events-none',
          index < total / 2 ? 'left-0' : 'right-0',
        )}
      >
        <div className="bg-card border border-border rounded-md px-2.5 py-1.5 text-xs shadow-lg whitespace-nowrap">
          <div className="font-medium">
            {formatSlotTime(slot.start)} &ndash; {formatSlotTime(slot.end)}
          </div>
          <div className="flex items-center gap-1.5 mt-0.5">
            <div className={cn('size-2 rounded-full', STATUS_COLORS[slot.status])} />
            <span className="text-muted-foreground">{STATUS_LABELS[slot.status]}</span>
          </div>
          {slot.checks > 0 && (
            <div className="text-muted-foreground mt-0.5">
              {slot.checks} check{slot.checks !== 1 ? 's' : ''}
              {slot.avgResponseMs !== null && ` \u00B7 ${formatResponseTime(slot.avgResponseMs)}`}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

interface TimelineTimeLabelsProps {
  from: string
  to: string
}

function TimelineTimeLabels({ from, to }: TimelineTimeLabelsProps) {
  // Generate labels at 6h intervals
  const fromDate = new Date(from)
  const toDate = new Date(to)
  const labels: string[] = []

  // Start from the first 6h-aligned time after "from"
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

export function Timeline() {
  const [data, setData] = useState<TimelineResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const isInitialLoad = useRef(true)
  const breakpoint = useTimelineBreakpoint()

  useEffect(() => {
    let cancelled = false

    async function fetchTimeline() {
      try {
        const res = await fetch(`/api/status/timeline?hours=24&window=${breakpoint.windowMinutes}`)
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const json: TimelineResponse = await res.json()
        if (!cancelled) {
          setData(json)
          setError(null)
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load timeline')
        }
      } finally {
        if (!cancelled) {
          if (isInitialLoad.current) {
            isInitialLoad.current = false
            setLoading(false)
          }
        }
      }
    }

    fetchTimeline()

    // Refresh every 60 seconds
    const interval = setInterval(fetchTimeline, 60_000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [breakpoint.windowMinutes])

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

  // Group services by their group
  const groups = groupByKey(data.services, (s) => s.group)

  return (
    <div
      className="space-y-6"
      role="region"
      aria-label="24-Hour Status Timeline"
      aria-busy={loading}
    >
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Activity className="size-5 text-muted-foreground" />
          <h2 className="text-xl font-semibold">24-Hour Timeline</h2>
        </div>
        <TimelineLegend />
      </div>

      {(() => {
        // Check if data is sparse (>50% unknown slots across all services)
        const allSlots = data.services.flatMap(s => s.slots)
        const unknownCount = allSlots.filter(s => s.status === 'unknown').length
        const sparseData = allSlots.length > 0 && unknownCount / allSlots.length > 0.5

        return (
          <>
            {sparseData && (
              <p className="text-xs text-muted-foreground flex items-center gap-1.5">
                <Clock className="size-3.5" />
                History is still accumulating — the timeline will fill in over the next 24 hours.
              </p>
            )}
            {Array.from(groups).map(([group, timelines]) => (
              <div key={group} className="space-y-3">
                <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wide">
                  {group}
                </h3>
                <div className="border rounded-lg bg-card p-4 space-y-4">
                  {timelines.map((tl) => (
                    <TimelineBar key={tl.service} timeline={tl} gapClass={breakpoint.gapClass} />
                  ))}
                  <TimelineTimeLabels from={data.from} to={data.to} />
                </div>
              </div>
            ))}
          </>
        )
      })()}
    </div>
  )
}
