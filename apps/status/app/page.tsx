import { Suspense } from 'react'
import { OverallStatusBadge } from '@/components/StatusBadge'
import { ServiceList } from '@/components/ServiceGroup'
import { UptimeLegend } from '@/components/UptimeBar'
import { ScheduledMaintenanceCard, IncidentCard } from '@/components/IncidentCard'
import { Timeline } from '@/components/Timeline'
import { checkAllServices } from '@/lib/health-checker'
import { getAllServicesUptime, isPrometheusAvailable } from '@/lib/prometheus'
import { getSiteConfig } from '@/lib/config'
import { getActiveIncidents as fetchActiveIncidents, getActiveMaintenances } from '@/lib/incidents'
import { calculateOverallStatus } from '@/lib/types'
import type { HealthCheckResult, UptimeData } from '@/lib/types'
import { RefreshCw, AlertCircle, Clock } from 'lucide-react'

// Revalidate every 60 seconds
export const revalidate = 60

async function getStatusData(): Promise<{
  services: HealthCheckResult[]
  uptimeData: Record<string, UptimeData> | null
  hasPrometheus: boolean
}> {
  const config = getSiteConfig()

  // Fetch health check results
  const services = await checkAllServices(config.services)

  // Check if Prometheus is available
  const hasPrometheus = await isPrometheusAvailable()

  // Fetch uptime data if Prometheus is available
  let uptimeData: Record<string, UptimeData> | null = null
  if (hasPrometheus) {
    uptimeData = await getAllServicesUptime(
      config.services.map(s => ({ name: s.name, url: s.url }))
    )
  }

  return { services, uptimeData, hasPrometheus }
}


function StatusSkeleton() {
  return (
    <div className="animate-pulse space-y-6">
      <div className="h-14 bg-muted rounded-lg w-64" />
      <div className="space-y-4">
        <div className="h-32 bg-muted rounded-lg" />
        <div className="h-32 bg-muted rounded-lg" />
        <div className="h-32 bg-muted rounded-lg" />
      </div>
    </div>
  )
}

async function StatusContent() {
  const { services, uptimeData, hasPrometheus } = await getStatusData()
  const overallStatus = calculateOverallStatus(services)
  const affectedCount = services.filter(s => s.status !== 'operational').length
  const activeIncidents = await fetchActiveIncidents()
  const scheduledMaintenance = await getActiveMaintenances()
  const lastUpdated = new Date().toISOString()

  return (
    <div className="space-y-8" aria-live="polite">
      {/* Overall Status */}
      <section className="text-center">
        <OverallStatusBadge
          status={overallStatus}
          totalServices={services.length}
          affectedServices={affectedCount}
        />
        <p className="text-sm text-muted-foreground mt-3 flex items-center justify-center gap-2">
          <Clock className="size-3.5" />
          Last updated: {new Date(lastUpdated).toLocaleTimeString()}
        </p>
      </section>

      {/* Active Incidents */}
      {activeIncidents.length > 0 && (
        <section>
          <div className="flex items-center gap-2 mb-4">
            <AlertCircle className="size-5 text-status-outage" />
            <h2 className="text-xl font-semibold">Active Incidents</h2>
          </div>
          <div className="space-y-4">
            {activeIncidents.map((incident) => (
              <IncidentCard key={incident.id} incident={incident} defaultExpanded />
            ))}
          </div>
        </section>
      )}

      {/* Scheduled Maintenance */}
      {scheduledMaintenance.length > 0 && (
        <section>
          <div className="flex items-center gap-2 mb-4">
            <Clock className="size-5 text-status-maintenance" />
            <h2 className="text-xl font-semibold">Scheduled Maintenance</h2>
          </div>
          <div className="space-y-4">
            {scheduledMaintenance.map((maintenance) => (
              <ScheduledMaintenanceCard key={maintenance.id} maintenance={maintenance} />
            ))}
          </div>
        </section>
      )}

      {/* Service Status */}
      <section>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-xl font-semibold">Service Status</h2>
          {hasPrometheus && <UptimeLegend />}
        </div>
        <ServiceList
          services={services}
          uptimeData={uptimeData || undefined}
          groupBy="group"
          variant="card"
        />
      </section>

      {/* 24-Hour Timeline */}
      <section>
        <Timeline />
      </section>

      {/* Auto-refresh indicator */}
      <div className="text-center text-sm text-muted-foreground flex items-center justify-center gap-2">
        <RefreshCw className="size-4 animate-spin-slow" />
        <span>Auto-refreshes every 60 seconds</span>
      </div>
    </div>
  )
}

export default function StatusPage() {
  return (
    <div className="max-w-5xl mx-auto px-4 sm:px-6 py-8">
      <Suspense fallback={<StatusSkeleton />}>
        <StatusContent />
      </Suspense>
    </div>
  )
}
