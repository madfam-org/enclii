'use client'

import { useCallback, useEffect, useState } from 'react'
import { providerApi } from '@/lib/provider-api'
import { Button } from '@enclii/ui-components/button'
import { RefreshCw, Github } from 'lucide-react'
import type { OperatorResponse } from '@/types/providers'

const DEFAULT_REPO = 'madfam-org/enclii'

export default function ProvidersGithubPage() {
  const [runs, setRuns] = useState<OperatorResponse | null>(null)
  const [repo, setRepo] = useState(DEFAULT_REPO)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await providerApi.operation('github', 'runs', {
        dry_run: true,
        args: { target: repo },
      })
      setRuns(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load GitHub runs')
    } finally {
      setLoading(false)
    }
  }, [repo])

  useEffect(() => {
    load()
  }, [load])

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-lg bg-primary/10 border border-primary/20">
            <Github className="size-5 text-primary" />
          </div>
          <div>
            <h2 className="text-lg font-semibold">GitHub</h2>
            <p className="text-sm text-muted-foreground">Read-only workflow runs for ecosystem repos</p>
          </div>
        </div>
        <Button variant="outline" size="sm" onClick={load} disabled={loading} className="gap-2">
          <RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>

      <div className="flex gap-2 items-center">
        <label className="text-sm text-muted-foreground">Repository</label>
        <input
          className="flex-1 max-w-md rounded-md border border-border bg-background px-3 py-2 text-sm font-mono"
          value={repo}
          onChange={(e) => setRepo(e.target.value)}
        />
      </div>

      {error && (
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
          {error}
        </div>
      )}

      <pre className="text-xs bg-muted/30 p-4 rounded overflow-auto max-h-[480px]">
        {JSON.stringify(runs?.data ?? runs, null, 2)}
      </pre>
    </div>
  )
}
