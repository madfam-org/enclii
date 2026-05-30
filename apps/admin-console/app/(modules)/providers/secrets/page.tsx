'use client'

import { useCallback, useEffect, useState } from 'react'
import { opsApi } from '@/lib/provider-api'
import { OperationPlanDialog } from '@/components/providers/operation-plan-dialog'
import { Button } from '@enclii/ui-components/button'
import { RefreshCw, Lock } from 'lucide-react'
import type { OperatorResponse } from '@/types/providers'

export default function ProvidersSecretsPage() {
  const [external, setExternal] = useState<OperatorResponse | null>(null)
  const [vault, setVault] = useState<OperatorResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [syncOpen, setSyncOpen] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [ext, v] = await Promise.all([
        opsApi.secretsExternal(),
        opsApi.secretsVault(),
      ])
      setExternal(ext)
      setVault(v)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load secrets status')
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
            <Lock className="size-5 text-primary" />
          </div>
          <div>
            <h2 className="text-lg font-semibold">Secrets & ESO</h2>
            <p className="text-sm text-muted-foreground">ExternalSecrets and Vault readiness (refs only)</p>
          </div>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={load} disabled={loading} className="gap-2">
            <RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </Button>
          <Button size="sm" onClick={() => setSyncOpen(true)}>
            Sync sweep
          </Button>
        </div>
      </div>

      {error && (
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
          {error}
        </div>
      )}

      <div className="grid gap-4 md:grid-cols-2">
        <StatusPanel title="ExternalSecrets" resp={external} />
        <StatusPanel title="Vault" resp={vault} />
      </div>

      <OperationPlanDialog
        open={syncOpen}
        onOpenChange={setSyncOpen}
        title="ESO sync sweep"
        description="Force-sync ExternalSecrets in enclii namespace"
        contract="ops"
        provider="secrets"
        action="sync-sweep"
        requestBody={{ scope: { namespace: 'enclii' } }}
        onSuccess={() => load()}
      />
    </div>
  )
}

function StatusPanel({ title, resp }: { title: string; resp: OperatorResponse | null }) {
  return (
    <div className="rounded-lg border border-border p-4 space-y-2">
      <h3 className="font-medium">{title}</h3>
      {resp ? (
        <>
          <p className="text-sm text-muted-foreground">{resp.summary}</p>
          <pre className="text-xs bg-muted/30 p-3 rounded overflow-auto max-h-48">
            {JSON.stringify(resp.data, null, 2)}
          </pre>
        </>
      ) : (
        <p className="text-sm text-muted-foreground">Loading…</p>
      )}
    </div>
  )
}
