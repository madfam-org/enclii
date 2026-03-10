'use client'

import { cn } from '@/lib/utils'
import { formatResponseTime } from '@/lib/utils'
import { getResponseTimeStatus, type ResponseTimeThresholds } from '@/lib/types'
import {
  RESPONSE_TIME_COLORS,
  RESPONSE_TIME_LABELS,
  RESPONSE_TIME_BAR_COLORS,
} from '@/lib/status-config'

interface ResponseTimeProps {
  ms: number | null
  showLabel?: boolean
  size?: 'sm' | 'md'
  thresholds?: ResponseTimeThresholds
}

export function ResponseTime({ ms, showLabel = false, size = 'md', thresholds }: ResponseTimeProps) {
  const status = getResponseTimeStatus(ms, thresholds)
  const formatted = formatResponseTime(ms)

  return (
    <div
      className={cn(
        'inline-flex items-center gap-1.5 font-mono',
        size === 'sm' ? 'text-xs' : 'text-sm',
        RESPONSE_TIME_COLORS[status]
      )}
    >
      <span>{formatted}</span>
      {showLabel && ms !== null && (
        <span className="text-muted-foreground">({RESPONSE_TIME_LABELS[status]})</span>
      )}
    </div>
  )
}

interface ResponseTimeBarProps {
  ms: number | null
  maxMs?: number
  thresholds?: ResponseTimeThresholds
}

export function ResponseTimeBar({ ms, maxMs = 5000, thresholds }: ResponseTimeBarProps) {
  const status = getResponseTimeStatus(ms, thresholds)
  const percentage = ms ? Math.min((ms / maxMs) * 100, 100) : 0

  return (
    <div className="flex items-center gap-2 w-full">
      <div className="flex-1 h-1.5 bg-muted rounded-full overflow-hidden">
        <div
          className={cn('h-full rounded-full transition-all duration-300', RESPONSE_TIME_BAR_COLORS[status])}
          style={{ width: `${percentage}%` }}
        />
      </div>
      <span className={cn('text-xs font-mono min-w-[60px] text-right', RESPONSE_TIME_COLORS[status])}>
        {formatResponseTime(ms)}
      </span>
    </div>
  )
}
