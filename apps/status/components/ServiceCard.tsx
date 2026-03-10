'use client'

import { cn } from '@/lib/utils'
import { StatusBadge } from './StatusBadge'
import { ResponseTime, ResponseTimeBar } from './ResponseTime'
import { UptimeBar, UptimeStats } from './UptimeBar'
import type { HealthCheckResult, UptimeData } from '@/lib/types'
import { formatRelativeTime, formatResponseTime } from '@/lib/utils'
import { ExternalLink, Clock, AlertCircle } from 'lucide-react'

interface ServiceCardProps {
  service: HealthCheckResult
  uptimeData?: UptimeData
  showDetails?: boolean
  showUptime?: boolean
}

export function ServiceCard({
  service,
  uptimeData,
  showDetails = true,
  showUptime = true,
}: ServiceCardProps) {
  const hasError = service.status === 'outage' || service.status === 'degraded'

  return (
    <div
      className={cn(
        'border rounded-lg bg-card p-4 transition-all duration-200',
        'hover:border-primary/20',
        hasError && 'border-status-outage/30'
      )}
    >
      {/* Header */}
      <div className="flex items-start justify-between gap-2 sm:gap-4">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <a
              href={service.href || service.url}
              target="_blank"
              rel="noopener noreferrer"
              className="font-medium truncate hover:underline hover:text-primary transition-colors"
            >
              {service.service}
            </a>
            <a
              href={service.href || service.url}
              target="_blank"
              rel="noopener noreferrer"
              className="text-muted-foreground hover:text-foreground transition-colors flex-shrink-0"
            >
              <ExternalLink className="size-3.5" />
            </a>
          </div>
          {service.description && (
            <p className="text-sm text-muted-foreground mt-0.5 truncate">
              {service.description}
            </p>
          )}
          <p className="text-xs text-muted-foreground/60 mt-0.5 truncate font-mono max-w-[200px] sm:max-w-none">
            {new URL(service.href || service.url).hostname}
          </p>
        </div>
        <div className="flex items-center gap-2 flex-shrink-0">
          {service.responseTime !== null && (
            <span className="text-xs font-mono text-muted-foreground hidden sm:inline">
              {formatResponseTime(service.responseTime)}
            </span>
          )}
          <StatusBadge status={service.status} size="sm" />
        </div>
      </div>

      {/* Response Time */}
      {showDetails && (
        <div className="mt-4">
          <ResponseTimeBar ms={service.responseTime} />
        </div>
      )}

      {/* Error Message */}
      {hasError && service.error && (
        <div className="mt-3 flex items-start gap-2 text-sm text-status-outage">
          <AlertCircle className="size-4 flex-shrink-0 mt-0.5" />
          <span>{service.error}</span>
        </div>
      )}

      {/* Uptime Data */}
      {showUptime && uptimeData && (
        <div className="mt-4 pt-4 border-t border-border">
          <UptimeBar history={uptimeData.dailyHistory} />
          <div className="mt-3">
            <UptimeStats
              uptime24h={uptimeData.uptime24h}
              uptime7d={uptimeData.uptime7d}
              uptime30d={uptimeData.uptime30d}
              uptime90d={uptimeData.uptime90d}
            />
          </div>
        </div>
      )}

      {/* Last Checked */}
      {showDetails && (
        <div className="mt-3 flex items-center gap-1.5 text-xs text-muted-foreground">
          <Clock className="size-3" />
          <span>Checked {formatRelativeTime(service.lastChecked)}</span>
        </div>
      )}
    </div>
  )
}

interface ServiceCardCompactProps {
  service: HealthCheckResult
}

export function ServiceCardCompact({ service }: ServiceCardCompactProps) {
  return (
    <div className="flex items-center justify-between gap-2 py-3 border-b border-border last:border-0">
      <div className="flex items-center gap-3 min-w-0">
        <StatusBadge status={service.status} showLabel={false} />
        <div className="min-w-0">
          <a
            href={service.href || service.url}
            target="_blank"
            rel="noopener noreferrer"
            className="font-medium truncate block hover:underline hover:text-primary transition-colors"
          >
            {service.service}
          </a>
          {service.description && (
            <span className="text-muted-foreground text-sm truncate block">— {service.description}</span>
          )}
        </div>
      </div>
      <ResponseTime ms={service.responseTime} size="sm" />
    </div>
  )
}
