'use client'

import { useEffect, useState } from 'react'
import { costApi } from '@/lib/admin-api'
import type { CostAllocation } from '@/types/admin'
import { DollarSign } from 'lucide-react'

export function CostDashboard() {
  const [summary, setSummary] = useState<CostAllocation[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    costApi.summary().then((d) => setSummary(d.summary || [])).finally(() => setLoading(false))
  }, [])

  if (loading) {
    return <div className="flex justify-center py-6"><div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" /></div>
  }

  const totalCents = summary.reduce((sum, s) => sum + s.cost_cents, 0)

  return (
    <div>
      <h3 className="text-lg font-semibold mb-3 flex items-center gap-2"><DollarSign className="size-5" /> Cost Allocation</h3>
      <div className="rounded-lg border border-border bg-card/50 p-4 mb-4">
        <p className="text-sm text-muted-foreground">Total Infrastructure Cost</p>
        <p className="text-3xl font-semibold">${(totalCents / 100).toFixed(2)}</p>
      </div>
      {summary.length === 0 ? (
        <p className="text-muted-foreground text-sm text-center">No cost data available.</p>
      ) : (
        <div className="space-y-2">
          {summary.map((s, i) => (
            <div key={i} className="rounded-md border border-border bg-card/30 p-3 flex items-center justify-between">
              <div>
                <p className="font-mono text-sm">{s.tenant_id}</p>
                <p className="text-xs text-muted-foreground">{s.allocation_percent}% allocation</p>
              </div>
              <p className="font-semibold">${(s.cost_cents / 100).toFixed(2)}</p>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
