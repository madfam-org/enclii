'use client'

import { cn } from '@/lib/utils'
import { ServiceCard, ServiceCardCompact } from './ServiceCard'
import { StatusBadge } from './StatusBadge'
import type { HealthCheckResult, UptimeData, ServiceStatus } from '@/lib/types'
import { calculateOverallStatus } from '@/lib/types'
import { STATUS_PRIORITY } from '@/lib/status-config'
import { ChevronDown, ChevronsUpDown } from 'lucide-react'
import { useState, useEffect, useCallback } from 'react'

const STORAGE_KEY = 'status-groups-expanded'

function loadExpandedGroups(): Set<string> | null {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored) return new Set(JSON.parse(stored))
  } catch { /* SSR or parse error */ }
  return null
}

function saveExpandedGroups(groups: Set<string>) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify([...groups]))
  } catch { /* SSR safety */ }
}

interface ServiceGroupProps {
  name: string
  services: HealthCheckResult[]
  uptimeData?: Record<string, UptimeData>
  isExpanded: boolean
  onToggle: () => void
  variant?: 'card' | 'compact'
}

export function ServiceGroup({
  name,
  services,
  uptimeData,
  isExpanded,
  onToggle,
  variant = 'card',
}: ServiceGroupProps) {
  const groupStatus = calculateOverallStatus(services)
  const operationalCount = services.filter(s => s.status === 'operational').length
  const operationalPct = Math.round((operationalCount / services.length) * 100)

  return (
    <div className="border border-border rounded-lg bg-card/50 overflow-hidden">
      {/* Group Header */}
      <button
        onClick={onToggle}
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

  // Sort groups
  const sortedGroups = Object.entries(groups).sort((a, b) => {
    if (groupBy === 'status') {
      return (STATUS_PRIORITY[a[0] as ServiceStatus] ?? 5) - (STATUS_PRIORITY[b[0] as ServiceStatus] ?? 5)
    }
    return a[0].localeCompare(b[0])
  })

  const groupNames = sortedGroups.map(([name]) => name)

  // State: which groups are expanded
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(
    () => new Set(groupNames) // default all expanded
  )
  const [initialized, setInitialized] = useState(false)

  // Load persisted state on mount
  useEffect(() => {
    const stored = loadExpandedGroups()
    if (stored) {
      // Only keep groups that still exist
      const valid = new Set([...stored].filter(g => groupNames.includes(g)))
      setExpandedGroups(valid)
    }
    setInitialized(true)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const toggleGroup = useCallback((name: string) => {
    setExpandedGroups(prev => {
      const next = new Set(prev)
      if (next.has(name)) {
        next.delete(name)
      } else {
        next.add(name)
      }
      saveExpandedGroups(next)
      return next
    })
  }, [])

  const allExpanded = expandedGroups.size === groupNames.length
  const toggleAll = useCallback(() => {
    const next = allExpanded ? new Set<string>() : new Set(groupNames)
    setExpandedGroups(next)
    saveExpandedGroups(next)
  }, [allExpanded, groupNames])

  // Before localStorage is loaded, default all expanded (SSR-safe)
  const isExpanded = (name: string) => !initialized || expandedGroups.has(name)

  return (
    <div className="space-y-6">
      {sortedGroups.length > 1 && (
        <div className="flex justify-end">
          <button
            onClick={toggleAll}
            className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            <ChevronsUpDown className="size-3.5" />
            {allExpanded ? 'Collapse All' : 'Expand All'}
          </button>
        </div>
      )}
      {sortedGroups.map(([groupName, groupServices]) => (
        <ServiceGroup
          key={groupName}
          name={groupName}
          services={groupServices}
          uptimeData={uptimeData}
          variant={variant}
          isExpanded={isExpanded(groupName)}
          onToggle={() => toggleGroup(groupName)}
        />
      ))}
    </div>
  )
}
