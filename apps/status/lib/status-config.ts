import type { ServiceStatus } from './types'

/**
 * Shared status color classes for bar/dot rendering
 */
export const STATUS_COLORS: Record<ServiceStatus, string> = {
  operational: 'bg-status-operational',
  degraded: 'bg-status-degraded',
  outage: 'bg-status-outage',
  maintenance: 'bg-status-maintenance',
  unknown: 'bg-muted',
}

/**
 * Shared status labels
 */
export const STATUS_LABELS: Record<ServiceStatus, string> = {
  operational: 'Operational',
  degraded: 'Degraded',
  outage: 'Major Outage',
  maintenance: 'Maintenance',
  unknown: 'Unknown',
}

/**
 * Full status config for badges (dot, background, text classes)
 */
export const STATUS_CONFIG: Record<ServiceStatus, {
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

/**
 * Generic group-by utility
 */
export function groupByKey<T>(items: T[], keyFn: (item: T) => string): Map<string, T[]> {
  const groups = new Map<string, T[]>()
  for (const item of items) {
    const key = keyFn(item)
    const list = groups.get(key) ?? []
    list.push(item)
    groups.set(key, list)
  }
  return groups
}
