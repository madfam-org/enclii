import { Suspense } from 'react'
import { IncidentList } from '@/components/IncidentCard'
import type { Incident } from '@/lib/types'
import { getRecentIncidents } from '@/lib/incidents'
import { CheckCircle, History } from 'lucide-react'

// Revalidate every 5 minutes for incident history
export const revalidate = 300

function IncidentsSkeleton() {
  return (
    <div className="animate-pulse space-y-4">
      <div className="h-24 bg-muted rounded-lg" />
      <div className="h-24 bg-muted rounded-lg" />
      <div className="h-24 bg-muted rounded-lg" />
    </div>
  )
}

async function IncidentsContent() {
  const incidents = await getRecentIncidents()

  // Group incidents by month
  const groupedByMonth: Record<string, Incident[]> = {}

  incidents.forEach((incident) => {
    const date = new Date(incident.createdAt)
    const monthKey = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}`
    const monthLabel = date.toLocaleDateString('en-US', { month: 'long', year: 'numeric' })

    if (!groupedByMonth[monthKey]) {
      groupedByMonth[monthKey] = []
    }
    groupedByMonth[monthKey].push(incident)
  })

  // Sort months in descending order
  const sortedMonths = Object.keys(groupedByMonth).sort((a, b) => b.localeCompare(a))

  if (incidents.length === 0) {
    return (
      <div className="text-center py-16">
        <div className="inline-flex items-center justify-center size-16 rounded-full bg-status-operational-muted mb-4">
          <CheckCircle className="size-8 text-status-operational" />
        </div>
        <h2 className="text-xl font-semibold mb-2">No Incidents</h2>
        <p className="text-muted-foreground max-w-md mx-auto">
          There have been no reported incidents. We&apos;ll post updates here if any issues occur.
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-8">
      {sortedMonths.map((monthKey) => {
        const monthIncidents = groupedByMonth[monthKey]
        const firstIncident = monthIncidents[0]
        const date = new Date(firstIncident.createdAt)
        const monthLabel = date.toLocaleDateString('en-US', { month: 'long', year: 'numeric' })

        return (
          <section key={monthKey}>
            <h2 className="text-lg font-semibold text-muted-foreground mb-4">
              {monthLabel}
            </h2>
            <IncidentList incidents={monthIncidents} />
          </section>
        )
      })}
    </div>
  )
}

export default function IncidentsPage() {
  return (
    <div className="max-w-5xl mx-auto px-4 sm:px-6 py-8">
      {/* Header */}
      <div className="mb-8">
        <div className="flex items-center gap-3 mb-2">
          <History className="size-6 text-muted-foreground" />
          <h1 className="text-2xl font-bold">Incident History</h1>
        </div>
        <p className="text-muted-foreground">
          A history of all incidents and their resolutions.
        </p>
      </div>

      {/* Incident List */}
      <Suspense fallback={<IncidentsSkeleton />}>
        <IncidentsContent />
      </Suspense>
    </div>
  )
}
