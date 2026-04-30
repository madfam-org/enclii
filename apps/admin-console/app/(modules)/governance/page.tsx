'use client'

import { DriftFeed } from '@/components/governance/drift-feed'
import { CostDashboard } from '@/components/governance/cost-dashboard'

export default function GovernancePage() {
  return (
    <div className="space-y-8">
      <div>
        <h2 className="text-2xl font-semibold mb-6">Governance</h2>
      </div>
      <DriftFeed />
      <CostDashboard />
    </div>
  )
}
