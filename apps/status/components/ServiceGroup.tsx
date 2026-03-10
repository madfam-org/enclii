'use client'

import { cn } from '@/lib/utils'
import { ServiceCard, ServiceCardCompact } from './ServiceCard'
import { StatusBadge } from './StatusBadge'
import type { HealthCheckResult, UptimeData, ServiceStatus } from '@/lib/types'
import { calculateOverallStatus } from '@/lib/types'
import { ChevronDown } from 'lucide-react'
import { useState } from 'react'

interface ServiceGroupProps {
  name: string
  services: HealthCheckResult[]
  uptimeData?: Record<string, UptimeData>
  defaultExpanded?: boolean
  variant?: 'card' | 'compact'
}

export function ServiceGroup({
  name,
  services,
  uptimeData,
  defaultExpanded = true,
  variant = 'card',
}: ServiceGroupProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded)
  const groupStatus = calculateOverallStatus(services)
  const operationalCount = services.filter(s => s.status === 'operational').length
  const operationalPct = Math.round((operationalCount / services.length) * 100)

  return (
    <div className="border border-border rounded-lg bg-card/50 overflow-hidden">
      {/* Group Header */}
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className={cn(
          'w-full flex items-center justify-between p-4',
          'hover:bg-muted/50 transition-colors',
          'text-left'
        )}
      >
        <div className="flex items-center gap-3">
          <StatusBadge status={groupStatus} showLabel={false} size="sm" />
          <h2 className="text-lg font-semibold">{name}</h2>
          <span className="text-sm text-muted-foreground">
            ({services.length} {services.length === 1 ? 'service' : 'services'})
          </span>
          <span className={cn(
            'text-xs font-mono',
            operationalPct === 100
              ? 'text-status-operational'
              : operationalPct >= 75
                ? 'text-status-degraded'
                : 'text-status-outage',
          )}>
            {operationalPct}% operational
          </span>
        </div>
        <ChevronDown
          className={cn(
            'size-5 text-muted-foreground transition-transform duration-200',
            isExpanded && 'rotate-180'
          )}
        />
      </button>

      {/* Services */}
      {isExpanded && (
        <div className={cn(
          'border-t border-border',
          variant === 'card' ? 'p-4 grid gap-4' : 'px-4'
        )}>
          {services.map((service) => (
            variant === 'card' ? (
              <ServiceCard
                key={service.url}
                service={service}
                uptimeData={uptimeData?.[service.service]}
                showUptime={!!uptimeData}
              />
            ) : (
              <ServiceCardCompact
                key={service.url}
                service={service}
              />
            )
          ))}
        </div>
      )}
    </div>
  )
}

interface ServiceListProps {
  services: HealthCheckResult[]
  uptimeData?: Record<string, UptimeData>
  groupBy?: 'group' | 'status' | 'none'
  variant?: 'card' | 'compact'
}

export function ServiceList({
  services,
  uptimeData,
  groupBy = 'group',
  variant = 'card',
}: ServiceListProps) {
  if (groupBy === 'none') {
    return (
      <div className={cn(
        variant === 'card'
          ? 'grid gap-4 md:grid-cols-2'
          : 'border border-border rounded-lg bg-card divide-y divide-border'
      )}>
        {services.map((service) => (
          variant === 'card' ? (
            <ServiceCard
              key={service.url}
              service={service}
              uptimeData={uptimeData?.[service.service]}
              showUptime={!!uptimeData}
            />
          ) : (
            <ServiceCardCompact
              key={service.url}
              service={service}
            />
          )
        ))}
      </div>
    )
  }

  // Group services
  const groups = services.reduce((acc, service) => {
    const key = groupBy === 'status' ? service.status : service.group
    if (!acc[key]) acc[key] = []
    acc[key].push(service)
    return acc
  }, {} as Record<string, HealthCheckResult[]>)

  // Sort groups (operational groups first if grouping by status)
  const sortedGroups = Object.entries(groups).sort((a, b) => {
    if (groupBy === 'status') {
      const statusOrder: Record<ServiceStatus, number> = {
        outage: 0,
        degraded: 1,
        maintenance: 2,
        operational: 3,
        unknown: 4,
      }
      return (statusOrder[a[0] as ServiceStatus] ?? 5) - (statusOrder[b[0] as ServiceStatus] ?? 5)
    }
    return a[0].localeCompare(b[0])
  })

  return (
    <div className="space-y-6">
      {sortedGroups.map(([groupName, groupServices]) => (
        <ServiceGroup
          key={groupName}
          name={groupName}
          services={groupServices}
          uptimeData={uptimeData}
          variant={variant}
        />
      ))}
    </div>
  )
}
