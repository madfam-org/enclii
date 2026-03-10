'use client'

import { cn } from '@/lib/utils'
import type { ServiceStatus } from '@/lib/types'
import { STATUS_CONFIG } from '@/lib/status-config'

interface StatusBadgeProps {
  status: ServiceStatus
  showLabel?: boolean
  size?: 'sm' | 'md' | 'lg'
  pulse?: boolean
}

const sizeConfig = {
  sm: {
    dot: 'size-2',
    text: 'text-xs',
    padding: 'px-2 py-0.5',
  },
  md: {
    dot: 'size-2.5',
    text: 'text-sm',
    padding: 'px-2.5 py-1',
  },
  lg: {
    dot: 'size-3',
    text: 'text-base',
    padding: 'px-3 py-1.5',
  },
}

export function StatusBadge({
  status,
  showLabel = true,
  size = 'md',
  pulse = false,
}: StatusBadgeProps) {
  const config = STATUS_CONFIG[status]
  const sizes = sizeConfig[size]

  return (
    <div
      role="status"
      aria-label={`Status: ${config.label}`}
      className={cn(
        'inline-flex items-center gap-2 rounded-full font-medium',
        showLabel && sizes.padding,
        showLabel && config.bgClass,
        showLabel && sizes.text
      )}
    >
      <span
        aria-hidden="true"
        className={cn(
          'rounded-full',
          sizes.dot,
          config.dotClass,
          pulse && status === 'operational' && 'animate-pulse-slow'
        )}
        style={{
          boxShadow: status === 'operational' ? '0 0 8px hsl(var(--status-operational))' : undefined,
        }}
      />
      {showLabel && (
        <span className={config.textClass}>{config.label}</span>
      )}
    </div>
  )
}

interface OverallStatusBadgeProps {
  status: ServiceStatus
  totalServices?: number
  affectedServices?: number
}

function getOverallLabel(status: ServiceStatus, total?: number, affected?: number): string {
  if (status === 'operational') return 'All Systems Operational'
  if (status === 'maintenance') return 'Scheduled Maintenance in Progress'

  const hasContext = total !== undefined && affected !== undefined && total > 0
  const affectedPct = hasContext ? affected! / total! : 1

  if (status === 'outage') {
    if (hasContext && affectedPct < 0.5) return 'Partial System Outage'
    return 'Major System Outage'
  }

  if (status === 'degraded') {
    if (hasContext && affectedPct < 0.5) return 'Some Systems Experiencing Issues'
    return 'Major Performance Issues'
  }

  return 'Unknown'
}

export function OverallStatusBadge({ status, totalServices, affectedServices }: OverallStatusBadgeProps) {
  const config = STATUS_CONFIG[status]
  const label = getOverallLabel(status, totalServices, affectedServices)
  const hasContext = totalServices !== undefined && affectedServices !== undefined && affectedServices > 0 && status !== 'operational'

  return (
    <div
      role="status"
      aria-label={`Overall status: ${label}`}
      className={cn(
        'inline-flex flex-col items-center gap-1 rounded-lg px-4 py-3',
        config.bgClass
      )}
    >
      <div className="inline-flex items-center gap-3">
        <span
          aria-hidden="true"
          className={cn(
            'size-4 rounded-full',
            config.dotClass,
            status === 'operational' && 'animate-pulse-slow'
          )}
          style={{
            boxShadow: `0 0 12px hsl(var(--status-${status}))`,
          }}
        />
        <span className={cn('text-lg font-semibold', config.textClass)}>
          {label}
        </span>
      </div>
      {hasContext && (
        <span className="text-xs text-muted-foreground">
          ({affectedServices} of {totalServices} affected)
        </span>
      )}
    </div>
  )
}
