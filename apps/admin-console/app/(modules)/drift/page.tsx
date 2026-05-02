'use client'

import { Metadata } from 'next'
import { DriftEventsList } from '@/components/drift/drift-events-list'

export default function DriftPage() {
  return (
    <div>
      <div className="mb-6">
        <h2 className="text-2xl font-semibold">Drift Events</h2>
        <p className="text-sm text-muted-foreground mt-1">
          Configuration drift detected by ArgoCD, Crossplane, and manual inspection.
        </p>
      </div>
      <DriftEventsList />
    </div>
  )
}
