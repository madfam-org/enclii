'use client'

import { cn } from '@/lib/utils'
import { formatDateTimeHuman, formatRelativeTime } from '@/lib/utils'
import type { Incident, IncidentUpdate, ScheduledMaintenance } from '@/lib/types'
import { INCIDENT_STATUS_CONFIG, INCIDENT_SEVERITY_CONFIG } from '@/lib/status-config'
import {
  Clock,
  Wrench,
  Calendar,
  ChevronDown,
  CheckCircle,
} from 'lucide-react'
import { useState } from 'react'

interface IncidentCardProps {
  incident: Incident
  defaultExpanded?: boolean
}

export function IncidentCard({ incident, defaultExpanded = false }: IncidentCardProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded)
  const status = INCIDENT_STATUS_CONFIG[incident.status]
  const severity = INCIDENT_SEVERITY_CONFIG[incident.severity]
  const StatusIcon = status.icon
  const SeverityIcon = severity.icon

  return (
    <div className={cn(
      'border rounded-lg bg-card overflow-hidden',
      incident.status !== 'resolved' && 'border-status-outage/30'
    )}>
      {/* Header */}
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full text-left p-4 hover:bg-muted/50 transition-colors"
      >
        <div className="flex items-start justify-between gap-4">
          <div className="flex-1">
            <div className="flex items-center gap-2 mb-1">
              <StatusIcon className={cn('size-4', status.color)} />
              <span className={cn('text-sm font-medium', status.color)}>{status.label}</span>
              <span className="text-muted-foreground">•</span>
              <SeverityIcon className={cn('size-4', severity.color)} />
              <span className={cn('text-sm', severity.color)}>{severity.label}</span>
            </div>
            <h3 className="font-semibold text-lg">{incident.title}</h3>
            <div className="flex flex-wrap items-center gap-2 mt-2 text-sm text-muted-foreground">
              <span>Started {formatRelativeTime(incident.createdAt)}</span>
              {incident.resolvedAt && (
                <>
                  <span>•</span>
                  <span>Resolved {formatRelativeTime(incident.resolvedAt)}</span>
                </>
              )}
            </div>
          </div>
          <ChevronDown
            className={cn(
              'size-5 text-muted-foreground transition-transform',
              isExpanded && 'rotate-180'
            )}
          />
        </div>

        {/* Affected Services */}
        {incident.affectedServices.length > 0 && (
          <div className="flex flex-wrap gap-2 mt-3">
            {incident.affectedServices.map((service) => (
              <span
                key={service}
                className="px-2 py-0.5 text-xs rounded-full bg-muted text-muted-foreground"
              >
                {service}
              </span>
            ))}
          </div>
        )}
      </button>

      {/* Updates Timeline */}
      {isExpanded && incident.updates.length > 0 && (
        <div className="border-t border-border px-4 py-3">
          <div className="space-y-4">
            {incident.updates.map((update, index) => (
              <IncidentUpdateItem
                key={update.id}
                update={update}
                isLast={index === incident.updates.length - 1}
              />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

interface IncidentUpdateItemProps {
  update: IncidentUpdate
  isLast: boolean
}

function IncidentUpdateItem({ update, isLast }: IncidentUpdateItemProps) {
  const status = update.status ? INCIDENT_STATUS_CONFIG[update.status] : null
  const Icon = status?.icon || Clock

  return (
    <div className="relative flex gap-3">
      {/* Timeline line */}
      {!isLast && (
        <div className="absolute left-[11px] top-6 bottom-0 w-px bg-border" />
      )}

      {/* Icon */}
      <div className={cn(
        'flex-shrink-0 size-6 rounded-full flex items-center justify-center',
        status?.bgColor || 'bg-muted'
      )}>
        <Icon className={cn('size-3', status?.color || 'text-muted-foreground')} />
      </div>

      {/* Content */}
      <div className="flex-1 min-w-0 pb-4">
        <div className="flex items-center gap-2 text-sm">
          {status && (
            <span className={cn('font-medium', status.color)}>{status.label}</span>
          )}
          <span className="text-muted-foreground">
            {formatDateTimeHuman(update.createdAt)}
          </span>
        </div>
        <p className="mt-1 text-sm">{update.message}</p>
      </div>
    </div>
  )
}

interface ScheduledMaintenanceCardProps {
  maintenance: ScheduledMaintenance
}

export function ScheduledMaintenanceCard({ maintenance }: ScheduledMaintenanceCardProps) {
  const startDate = new Date(maintenance.scheduledStart)
  const endDate = new Date(maintenance.scheduledEnd)
  const isUpcoming = startDate > new Date()
  const isOngoing = startDate <= new Date() && endDate >= new Date()

  return (
    <div className={cn(
      'border rounded-lg bg-card p-4',
      'border-status-maintenance/30'
    )}>
      <div className="flex items-start gap-3">
        <div className="p-2 rounded-lg bg-status-maintenance-muted">
          <Wrench className="size-5 text-status-maintenance" />
        </div>
        <div className="flex-1">
          <div className="flex items-center gap-2 mb-1">
            <span className={cn(
              'text-xs font-medium px-2 py-0.5 rounded-full',
              isOngoing
                ? 'bg-status-maintenance text-status-maintenance-foreground'
                : 'bg-status-maintenance-muted text-status-maintenance'
            )}>
              {isOngoing ? 'In Progress' : 'Scheduled'}
            </span>
          </div>
          <h3 className="font-semibold">{maintenance.title}</h3>
          {maintenance.description && (
            <p className="text-sm text-muted-foreground mt-1">{maintenance.description}</p>
          )}
          <div className="flex items-center gap-2 mt-3 text-sm text-muted-foreground">
            <Calendar className="size-4" />
            <span>
              {formatDateTimeHuman(maintenance.scheduledStart)} — {formatDateTimeHuman(maintenance.scheduledEnd)}
            </span>
          </div>
          {maintenance.affectedServices.length > 0 && (
            <div className="flex flex-wrap gap-2 mt-3">
              {maintenance.affectedServices.map((service) => (
                <span
                  key={service}
                  className="px-2 py-0.5 text-xs rounded-full bg-muted text-muted-foreground"
                >
                  {service}
                </span>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

interface IncidentListProps {
  incidents: Incident[]
  emptyMessage?: string
}

export function IncidentList({ incidents, emptyMessage = 'No incidents to display' }: IncidentListProps) {
  if (incidents.length === 0) {
    return (
      <div className="text-center py-12 text-muted-foreground">
        <CheckCircle className="size-12 mx-auto mb-4 opacity-50" />
        <p>{emptyMessage}</p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {incidents.map((incident) => (
        <IncidentCard key={incident.id} incident={incident} />
      ))}
    </div>
  )
}
