'use client'

import { cn } from '@/lib/utils'
import type { DayStatus, ServiceStatus } from '@/lib/types'
import { formatDate, formatUptime } from '@/lib/utils'
import { STATUS_COLORS } from '@/lib/status-config'

interface UptimeBarProps {
  history: DayStatus[]
  showTooltip?: boolean
}

export function UptimeBar({ history, showTooltip = true }: UptimeBarProps) {
  // Ensure we have exactly 90 days
  const days = history.slice(-90)

  return (
    <div className="w-full">
      <div className="flex gap-[2px] h-8">
        {days.map((day, index) => (
          <div
            key={day.date}
            className="relative flex-1 group"
            style={{ minWidth: '2px' }}
          >
            <div
              className={cn(
                'h-full rounded-sm transition-all duration-150',
                'hover:scale-y-110 hover:brightness-110',
                STATUS_COLORS[day.status]
              )}
            />
            {showTooltip && (
              <div className={cn(
                'absolute z-20 bottom-full mb-2 opacity-0 group-hover:opacity-100 transition-opacity',
                'pointer-events-none',
                index < 45 ? 'left-0' : 'right-0'
              )}>
                <div className="bg-card border border-border rounded-md px-2 py-1 text-xs shadow-lg whitespace-nowrap">
                  <div className="font-medium">{formatDate(day.date)}</div>
                  <div className="text-muted-foreground">
                    {formatUptime(day.uptimePercent)} uptime
                  </div>
                </div>
              </div>
            )}
          </div>
        ))}
      </div>
      <div className="flex justify-between mt-2 text-xs text-muted-foreground">
        <span>90 days ago</span>
        <span>Today</span>
      </div>
    </div>
  )
}

interface UptimeStatsProps {
  uptime24h: number | null
  uptime7d: number | null
  uptime30d: number | null
  uptime90d: number | null
}

export function UptimeStats({ uptime24h, uptime7d, uptime30d, uptime90d }: UptimeStatsProps) {
  const stats = [
    { label: '24h', value: uptime24h },
    { label: '7d', value: uptime7d },
    { label: '30d', value: uptime30d },
    { label: '90d', value: uptime90d },
  ]

  return (
    <div className="flex gap-4">
      {stats.map(({ label, value }) => (
        <div key={label} className="text-center">
          <div className={cn(
            'text-lg font-semibold font-mono',
            value === null
              ? 'text-muted-foreground'
              : value >= 99.9
                ? 'text-status-operational'
                : value >= 99
                  ? 'text-status-degraded'
                  : 'text-status-outage'
          )}>
            {formatUptime(value)}
          </div>
          <div className="text-xs text-muted-foreground">{label}</div>
        </div>
      ))}
    </div>
  )
}

interface UptimeLegendProps {
  className?: string
}

export function UptimeLegend({ className }: UptimeLegendProps) {
  const items = [
    { status: 'operational', label: 'Operational' },
    { status: 'degraded', label: 'Degraded' },
    { status: 'outage', label: 'Outage' },
    { status: 'maintenance', label: 'Maintenance' },
  ] as const

  return (
    <div className={cn('flex flex-wrap gap-4 text-xs', className)}>
      {items.map(({ status, label }) => (
        <div key={status} className="flex items-center gap-1.5">
          <div className={cn('size-3 rounded-sm', STATUS_COLORS[status])} />
          <span className="text-muted-foreground">{label}</span>
        </div>
      ))}
    </div>
  )
}
