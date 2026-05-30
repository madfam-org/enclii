'use client'

import { useCallback, useEffect, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { providerApi } from '@/lib/provider-api'
import { ProviderStatusCard } from '@/components/providers/provider-status-card'
import { Button } from '@enclii/ui-components/button'
import { RefreshCw, Plug } from 'lucide-react'
import type { ProviderCatalogResponse } from '@/types/providers'

export function ProvidersOverview() {
  const router = useRouter()
  const [catalog, setCatalog] = useState<ProviderCatalogResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await providerApi.catalog()
      setCatalog(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load catalog')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const tabRoutes: Record<string, string> = {
    resend: '/providers/resend',
    cloudflare: '/providers/cloudflare',
    github: '/providers/github',
    porkbun: '/providers/porkbun',
    secrets: '/providers/secrets',
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-lg bg-primary/10 border border-primary/20">
            <Plug className="size-5 text-primary" />
          </div>
          <div>
            <h2 className="text-lg font-semibold">Provider Hub</h2>
            <p className="text-sm text-muted-foreground">
              Enclii-first operator console for Resend, Cloudflare, GitHub, and secrets
            </p>
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

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {catalog?.providers.map((entry) => (
          <ProviderStatusCard
            key={entry.name}
            entry={entry}
            checkedAt={catalog.generated_at}
            onOpen={() => {
              const route = tabRoutes[entry.name]
              if (route) router.push(route)
            }}
          />
        ))}
      </div>

      <div className="flex flex-wrap gap-2 text-sm">
        <Link href="/providers/resend" className="text-primary hover:underline">Resend domains</Link>
        <span className="text-muted-foreground">·</span>
        <Link href="/domains" className="text-primary hover:underline">Domain matrix (Cloudflare)</Link>
        <span className="text-muted-foreground">·</span>
        <Link href="/providers/secrets" className="text-primary hover:underline">Secrets / ESO</Link>
      </div>
    </div>
  )
}
