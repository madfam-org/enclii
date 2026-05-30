'use client'

import { useCallback, useEffect, useState } from 'react'
import { providerApi } from '@/lib/provider-api'
import { Button } from '@enclii/ui-components/button'
import { RefreshCw, Server } from 'lucide-react'
import type { OperatorResponse } from '@/types/providers'

export default function ProvidersPorkbunPage() {
  const [domains, setDomains] = useState<OperatorResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await providerApi.operation('porkbun', 'domains', { dry_run: true })
      setDomains(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load Porkbun domains')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-lg bg-primary/10 border border-primary/20">
            <Server className="size-5 text-primary" />
          </div>
          <div>
            <h2 className="text-lg font-semibold">Porkbun</h2>
            <p className="text-sm text-muted-foreground">Registrar inventory and nameserver fallback</p>
          </div>
        </div>
        <Button variant="outline" size="sm" onClick={load} disabled={loading} className="gap-2">
          <RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>

      {error && (
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
          {error}
        </div>
      )}

      <pre className="text-xs bg-muted/30 p-4 rounded overflow-auto max-h-[480px]">
        {JSON.stringify(domains?.data ?? domains, null, 2)}
      </pre>
    </div>
  )
}
