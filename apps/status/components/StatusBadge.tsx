'use client'

import { cn } from '@/lib/utils'
import type { ServiceStatus } from '@/lib/types'

interface StatusBadgeProps {
  status: ServiceStatus
  showLabel?: boolean
  size?: 'sm' | 'md' | 'lg'
  pulse?: boolean
}

const statusConfig: Record<ServiceStatus, {
  label: string
  dotClass: string
  bgClass: string
  textClass: string
}> = {
  operational: {
    label: 'Operational',
    dotClass: 'bg-status-operational',
    bgClass: 'bg-status-operational-muted',
    textClass: 'text-status-operational',
  },
  degraded: {
    label: 'Degraded',
    dotClass: 'bg-status-degraded',
    bgClass: 'bg-status-degraded-muted',
    textClass: 'text-status-degraded',
  },
  outage: {
    label: 'Major Outage',
    dotClass: 'bg-status-outage',
    bgClass: 'bg-status-outage-muted',
    textClass: 'text-status-outage',
  },
  maintenance: {
    label: 'Maintenance',
    dotClass: 'bg-status-maintenance',
    bgClass: 'bg-status-maintenance-muted',
    textClass: 'text-status-maintenance',
  },
  unknown: {
    label: 'Unknown',
    dotClass: 'bg-muted-foreground',
    bgClass: 'bg-muted',
    textClass: 'text-muted-foreground',
  },
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
  const config = statusConfig[status]
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

export function OverallStatusBadge({ status }: { status: ServiceStatus }) {
  const config = statusConfig[status]

  const label = status === 'operational' ? 'All Systems Operational' : config.label

  return (
    <div
      role="status"
      aria-label={`Overall status: ${label}`}
      className={cn(
        'inline-flex items-center gap-3 rounded-lg px-4 py-3',
        config.bgClass
      )}
    >
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
  )
}
