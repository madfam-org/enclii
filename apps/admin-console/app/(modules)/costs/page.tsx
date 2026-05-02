'use client'

import { CostDashboard } from '@/components/costs/cost-dashboard'

export default function CostsPage() {
  return (
    <div>
      <div className="mb-6">
        <h2 className="text-2xl font-semibold">Cost Allocation</h2>
        <p className="text-sm text-muted-foreground mt-1">
          Bare-metal host cost tracking and tenant allocation breakdown.
        </p>
      </div>
      <CostDashboard />
    </div>
  )
}
