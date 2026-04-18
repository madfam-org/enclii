'use client'

import { cn } from '@/lib/utils'
import { ServiceCard, ServiceCardCompact } from './ServiceCard'
import { StatusBadge } from './StatusBadge'
import type { HealthCheckResult, UptimeData, ServiceStatus, ResponseTimeThresholds } from '@/lib/types'
import { calculateOverallStatus } from '@/lib/types'
import { STATUS_PRIORITY } from '@/lib/status-config'
import { ChevronDown, ChevronsUpDown } from 'lucide-react'
import { useState, useEffect, useCallback } from 'react'

const STORAGE_KEY = 'status-groups-expanded'
const FAMILY_STORAGE_KEY = 'status-families-expanded'

const DEFAULT_FAMILY = 'Other'

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

function loadExpandedFamilies(): Set<string> | null {
  try {
    const stored = localStorage.getItem(FAMILY_STORAGE_KEY)
    if (stored) return new Set(JSON.parse(stored))
  } catch { /* SSR or parse error */ }
  return null
}

function saveExpandedFamilies(families: Set<string>) {
  try {
    localStorage.setItem(FAMILY_STORAGE_KEY, JSON.stringify([...families]))
  } catch { /* SSR safety */ }
}

interface ServiceGroupProps {
  name: string
  services: HealthCheckResult[]
  uptimeData?: Record<string, UptimeData>
  isExpanded: boolean
  onToggle: () => void
  variant?: 'card' | 'compact'
  thresholds?: ResponseTimeThresholds
  maxResponseTime?: number
}

export function ServiceGroup({
  name,
  services,
  uptimeData,
  isExpanded,
  onToggle,
  variant = 'card',
  thresholds,
  maxResponseTime,
}: ServiceGroupProps) {
  const groupStatus = calculateOverallStatus(services)
  const operationalCount = services.filter(s => s.status === 'operational').length
  const operationalPct = Math.round((operationalCount / services.length) * 100)

  return (
    <div className={cn(
      'border border-border rounded-lg bg-card/50 overflow-hidden',
      !isExpanded && groupStatus === 'outage' && 'border-l-2 border-l-status-outage',
      !isExpanded && groupStatus === 'degraded' && 'border-l-2 border-l-status-degraded',
      !isExpanded && groupStatus === 'maintenance' && 'border-l-2 border-l-status-maintenance',
    )}>
      {/* Group Header */}
      <button
        onClick={onToggle}
        className={cn(
          'w-full flex items-center justify-between p-4',
          'hover:bg-muted/50 transition-colors',
          'text-left'
        )}
      >
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
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
                thresholds={thresholds}
                maxResponseTime={maxResponseTime}
              />
            ) : (
              <ServiceCardCompact
                key={service.url}
                service={service}
                thresholds={thresholds}
              />
            )
          ))}
        </div>
      )}
    </div>
  )
}

interface ServiceFamilyProps {
  name: string
  groupedServices: Array<[string, HealthCheckResult[]]>
  uptimeData?: Record<string, UptimeData>
  isExpanded: boolean
  onToggle: () => void
  variant?: 'card' | 'compact'
  thresholds?: ResponseTimeThresholds
  maxResponseTime?: number
  expandedGroups: Set<string>
  initialized: boolean
  onToggleGroup: (name: string) => void
}

/**
 * Family-level wrapper that groups multiple product `group`s under one
 * collapsible product family header. Reuses ServiceGroup for inner rendering —
 * does NOT change the service-level row layout.
 */
export function ServiceFamily({
  name,
  groupedServices,
  uptimeData,
  isExpanded,
  onToggle,
  variant = 'card',
  thresholds,
  maxResponseTime,
  expandedGroups,
  initialized,
  onToggleGroup,
}: ServiceFamilyProps) {
  const allServices = groupedServices.flatMap(([, s]) => s)
  const familyStatus = calculateOverallStatus(allServices)
  const operationalCount = allServices.filter(s => s.status === 'operational').length
  const operationalPct = allServices.length === 0
    ? 100
    : Math.round((operationalCount / allServices.length) * 100)

  return (
    <div className={cn(
      'border border-border rounded-lg bg-card/30 overflow-hidden',
      !isExpanded && familyStatus === 'outage' && 'border-l-4 border-l-status-outage',
      !isExpanded && familyStatus === 'degraded' && 'border-l-4 border-l-status-degraded',
      !isExpanded && familyStatus === 'maintenance' && 'border-l-4 border-l-status-maintenance',
    )}>
      <button
        onClick={onToggle}
        className={cn(
          'w-full flex items-center justify-between p-4',
          'hover:bg-muted/40 transition-colors text-left',
        )}
        aria-expanded={isExpanded}
      >
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <StatusBadge status={familyStatus} showLabel={false} size="sm" />
          <h2 className="text-xl font-semibold">{name}</h2>
          <span className="text-sm text-muted-foreground">
            ({allServices.length} {allServices.length === 1 ? 'service' : 'services'},{' '}
            {groupedServices.length} {groupedServices.length === 1 ? 'group' : 'groups'})
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
            isExpanded && 'rotate-180',
          )}
        />
      </button>

      {isExpanded && (
        <div className="border-t border-border p-3 space-y-3">
          {groupedServices.map(([groupName, groupServices]) => (
            <ServiceGroup
              key={groupName}
              name={groupName}
              services={groupServices}
              uptimeData={uptimeData}
              variant={variant}
              isExpanded={!initialized || expandedGroups.has(groupName)}
              onToggle={() => onToggleGroup(groupName)}
              thresholds={thresholds}
              maxResponseTime={maxResponseTime}
            />
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
  thresholds?: ResponseTimeThresholds
}

export function ServiceList({
  services,
  uptimeData,
  groupBy = 'group',
  variant = 'card',
  thresholds,
}: ServiceListProps) {
  const maxResponseTime = Math.max(...services.map(s => s.responseTime ?? 0), 1) * 1.2
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
              thresholds={thresholds}
              maxResponseTime={maxResponseTime}
            />
          ) : (
            <ServiceCardCompact
              key={service.url}
              service={service}
              thresholds={thresholds}
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

  // --- Family-level grouping (S1) ---
  // When any service has a `family`, wrap groups in family accordions. Otherwise
  // fall back to the original single-level list so there's zero change for
  // deployments that haven't opted in yet.
  const hasFamilies = groupBy === 'group'
    && services.some(s => s.family && s.family.length > 0)

  // Build family -> group -> services mapping.
  const familyMap = new Map<string, Map<string, HealthCheckResult[]>>()
  if (hasFamilies) {
    for (const [groupName, groupServices] of sortedGroups) {
      // Family for this group = family of first member (fall back to "Other")
      const family = groupServices[0]?.family ?? DEFAULT_FAMILY
      if (!familyMap.has(family)) familyMap.set(family, new Map())
      familyMap.get(family)!.set(groupName, groupServices)
    }
  }
  const familyNames = [...familyMap.keys()].sort((a, b) => {
    // Push "Other" to the end
    if (a === DEFAULT_FAMILY) return 1
    if (b === DEFAULT_FAMILY) return -1
    return a.localeCompare(b)
  })

  const [expandedFamilies, setExpandedFamilies] = useState<Set<string>>(
    () => new Set(familyNames),
  )

  useEffect(() => {
    if (!hasFamilies) return
    const stored = loadExpandedFamilies()
    if (stored) {
      const valid = new Set([...stored].filter(f => familyNames.includes(f)))
      setExpandedFamilies(valid)
    }
  }, [hasFamilies]) // eslint-disable-line react-hooks/exhaustive-deps

  const toggleFamily = useCallback((name: string) => {
    setExpandedFamilies(prev => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name); else next.add(name)
      saveExpandedFamilies(next)
      return next
    })
  }, [])

  const isFamilyExpanded = (name: string) => !initialized || expandedFamilies.has(name)

  if (hasFamilies) {
    return (
      <div className="space-y-6">
        <div className="flex justify-end">
          <button
            onClick={toggleAll}
            className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            <ChevronsUpDown className="size-3.5" />
            {allExpanded ? 'Collapse All Groups' : 'Expand All Groups'}
          </button>
        </div>
        {familyNames.map(familyName => {
          const groupsInFamily = [...familyMap.get(familyName)!.entries()]
            .sort((a, b) => a[0].localeCompare(b[0]))
          return (
            <ServiceFamily
              key={familyName}
              name={familyName}
              groupedServices={groupsInFamily}
              uptimeData={uptimeData}
              variant={variant}
              isExpanded={isFamilyExpanded(familyName)}
              onToggle={() => toggleFamily(familyName)}
              thresholds={thresholds}
              maxResponseTime={maxResponseTime}
              expandedGroups={expandedGroups}
              initialized={initialized}
              onToggleGroup={toggleGroup}
            />
          )
        })}
      </div>
    )
  }

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
          thresholds={thresholds}
          maxResponseTime={maxResponseTime}
        />
      ))}
    </div>
  )
}
