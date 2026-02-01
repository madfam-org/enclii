'use client'

import { useState, useEffect } from 'react'
import { DataTable } from './data-table'
import { columns } from './columns'
import { CommissionDialog } from './commission-dialog'
import { Button } from '@/components/ui/button'
import { Plus, RefreshCw, Globe } from 'lucide-react'
import type { DispatchDomain } from '@/types/cloudflare'

/**
 * Domain Matrix - The Registrar Module
 *
 * A sovereign interface to manage Cloudflare Zones across the ecosystem.
 * Lists all domains (madfam, suluna, primavera, etc.) with status indicators.
 */
export function DomainMatrix() {
  const [domains, setDomains] = useState<DispatchDomain[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showCommissionDialog, setShowCommissionDialog] = useState(false)

  const fetchDomains = async () => {
    setIsLoading(true)
    setError(null)

    try {
      const controller = new AbortController()
      const timeout = setTimeout(() => controller.abort(), 15000)
      const response = await fetch('/api/domains', { signal: controller.signal })
      clearTimeout(timeout)
      const data = await response.json()

      if (data.success) {
        setDomains(data.data)
      } else {
        setError(data.error || 'Failed to fetch domains')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch domains')
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    fetchDomains()
  }, [])

  const handleCommissionSuccess = () => {
    setShowCommissionDialog(false)
    fetchDomains() // Refresh the list
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-lg bg-primary/10 border border-primary/20">
            <Globe className="size-5 text-primary glow-effect" />
          </div>
          <div>
            <h2 className="text-lg font-semibold text-foreground">Domain Matrix</h2>
            <p className="text-sm text-muted-foreground">
              Manage Cloudflare zones across the ecosystem
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={fetchDomains}
            disabled={isLoading}
            className="gap-2"
          >
            <RefreshCw className={`size-4 ${isLoading ? 'animate-spin' : ''}`} />
            Refresh
          </Button>
          <Button
            size="sm"
            onClick={() => setShowCommissionDialog(true)}
            className="gap-2 bg-primary text-primary-foreground hover:bg-primary/90"
          >
            <Plus className="size-4" />
            Commission Domain
          </Button>
        </div>
      </div>

      {/* Error State */}
      {error && (
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-4">
          <p className="text-sm text-destructive">{error}</p>
          <Button
            variant="outline"
            size="sm"
            onClick={fetchDomains}
            className="mt-2"
          >
            Retry
          </Button>
        </div>
      )}

      {/* Data Table */}
      <DataTable columns={columns} data={domains} isLoading={isLoading} />

      {/* Commission Dialog */}
      <CommissionDialog
        open={showCommissionDialog}
        onOpenChange={setShowCommissionDialog}
        onSuccess={handleCommissionSuccess}
      />
    </div>
  )
}
