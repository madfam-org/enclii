'use client'

import { useEffect, useState } from 'react'
import { costApi } from '@/lib/admin-api'
import type { CostAllocation } from '@/types/admin'
import { DollarSign, TrendingUp, Calendar, Filter } from 'lucide-react'

function formatCents(cents: number): string {
  return `$${(cents / 100).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

function formatDate(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

function getPeriodRange(period: 'month' | 'quarter' | 'year'): { start: string; end: string } {
  const now = new Date()
  const end = now.toISOString()
  let start: Date
  if (period === 'month') {
    start = new Date(now.getFullYear(), now.getMonth(), 1)
  } else if (period === 'quarter') {
    const q = Math.floor(now.getMonth() / 3)
    start = new Date(now.getFullYear(), q * 3, 1)
  } else {
    start = new Date(now.getFullYear(), 0, 1)
  }
  return { start: start.toISOString(), end }
}

export function CostDashboard() {
  const [allocations, setAllocations] = useState<CostAllocation[]>([])
  const [summary, setSummary] = useState<CostAllocation[]>([])
  const [loading, setLoading] = useState(true)
  const [period, setPeriod] = useState<'month' | 'quarter' | 'year'>('month')
  const [error, setError] = useState<string | null>(null)

  const fetchCosts = async () => {
    setLoading(true)
    setError(null)
    try {
      const range = getPeriodRange(period)
      const [allocData, sumData] = await Promise.all([
        costApi.list(range),
        costApi.summary(range),
      ])
      setAllocations(allocData.allocations || [])
      setSummary(sumData.summary || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load cost data')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchCosts()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [period])

  const totalCents = allocations.reduce((sum, a) => sum + a.cost_cents, 0)
  const byTenant = allocations.reduce<Record<string, number>>((acc, a) => {
    acc[a.tenant_id] = (acc[a.tenant_id] ?? 0) + a.cost_cents
    return acc
  }, {})
  const tenantRows = Object.entries(byTenant)
    .sort(([, a], [, b]) => b - a)
    .slice(0, 10)

  return (
    <div className="space-y-6">
      {/* Period selector */}
      <div className="flex items-center gap-1 border-b border-border pb-3">
        <Filter className="size-4 text-muted-foreground mr-2" />
        {(['month', 'quarter', 'year'] as const).map((p) => (
          <button
            key={p}
            onClick={() => setPeriod(p)}
            className={`px-3 py-1 rounded-md text-sm font-medium capitalize transition-colors ${
              period === p
                ? 'bg-primary/10 text-primary'
                : 'text-muted-foreground hover:text-foreground hover:bg-muted/50'
            }`}
          >
            {p === 'month' ? 'This Month' : p === 'quarter' ? 'This Quarter' : 'This Year'}
          </button>
        ))}
      </div>

      {loading ? (
        <div className="flex justify-center py-12">
          <div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" />
        </div>
      ) : error ? (
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
          {error}
        </div>
      ) : (
        <>
          {/* KPI cards */}
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <div className="rounded-lg border border-border bg-card/50 p-4">
              <div className="flex items-center gap-2 mb-2">
                <DollarSign className="size-4 text-primary" />
                <span className="text-sm text-muted-foreground">Total Cost</span>
              </div>
              <p className="text-2xl font-bold font-mono">{formatCents(totalCents)}</p>
            </div>
            <div className="rounded-lg border border-border bg-card/50 p-4">
              <div className="flex items-center gap-2 mb-2">
                <TrendingUp className="size-4 text-primary" />
                <span className="text-sm text-muted-foreground">Allocations</span>
              </div>
              <p className="text-2xl font-bold font-mono">{allocations.length}</p>
            </div>
            <div className="rounded-lg border border-border bg-card/50 p-4">
              <div className="flex items-center gap-2 mb-2">
                <Calendar className="size-4 text-primary" />
                <span className="text-sm text-muted-foreground">Tenants</span>
              </div>
              <p className="text-2xl font-bold font-mono">{Object.keys(byTenant).length}</p>
            </div>
          </div>

          {/* Per-tenant breakdown */}
          <div>
            <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
              Top Tenants by Cost
            </h3>
            {tenantRows.length === 0 ? (
              <p className="text-muted-foreground text-sm">No cost data for this period.</p>
            ) : (
              <div className="rounded-lg border border-border overflow-hidden">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border bg-muted/30">
                      <th className="text-left px-4 py-2 font-medium text-muted-foreground">Tenant</th>
                      <th className="text-right px-4 py-2 font-medium text-muted-foreground">Cost</th>
                      <th className="text-right px-4 py-2 font-medium text-muted-foreground">Share</th>
                    </tr>
                  </thead>
                  <tbody>
                    {tenantRows.map(([tenantId, cents]) => (
                      <tr key={tenantId} className="border-b border-border last:border-0">
                        <td className="px-4 py-2.5 font-mono text-xs">{tenantId}</td>
                        <td className="px-4 py-2.5 text-right font-mono">{formatCents(cents)}</td>
                        <td className="px-4 py-2.5 text-right text-muted-foreground">
                          {totalCents > 0 ? `${((cents / totalCents) * 100).toFixed(1)}%` : '—'}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          {/* Recent allocations */}
          {summary.length > 0 && (
            <div>
              <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
                Allocation Summary
              </h3>
              <div className="rounded-lg border border-border overflow-hidden">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border bg-muted/30">
                      <th className="text-left px-4 py-2 font-medium text-muted-foreground">Tenant</th>
                      <th className="text-left px-4 py-2 font-medium text-muted-foreground">Period</th>
                      <th className="text-right px-4 py-2 font-medium text-muted-foreground">Alloc %</th>
                      <th className="text-right px-4 py-2 font-medium text-muted-foreground">Cost</th>
                    </tr>
                  </thead>
                  <tbody>
                    {summary.slice(0, 20).map((a) => (
                      <tr key={a.id} className="border-b border-border last:border-0">
                        <td className="px-4 py-2.5 font-mono text-xs">{a.tenant_id}</td>
                        <td className="px-4 py-2.5 text-xs text-muted-foreground">
                          {formatDate(a.period_start)} – {formatDate(a.period_end)}
                        </td>
                        <td className="px-4 py-2.5 text-right">{a.allocation_percent.toFixed(1)}%</td>
                        <td className="px-4 py-2.5 text-right font-mono">{formatCents(a.cost_cents)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}
