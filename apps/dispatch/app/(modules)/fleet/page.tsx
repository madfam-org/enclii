'use client'

import { FleetGrid } from '@/components/fleet/fleet-grid'

export default function FleetPage() {
  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-2xl font-semibold">Bare Metal Fleet</h2>
      </div>
      <FleetGrid />
    </div>
  )
}
