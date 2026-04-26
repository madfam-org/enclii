'use client'

import { useCallback, useEffect, useState } from 'react'
import {
  ArrowUpRight,
  Check,
  GitBranch,
  RefreshCw,
  TriangleAlert,
  X,
} from 'lucide-react'
import {
  fetchArgoApplications,
  syncArgoApplication,
  type ArgoApplicationSummary,
} from '@/lib/argocd-api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { EmptyState } from '@/components/empty-state'

const POLL_INTERVAL_MS = 30_000

const syncBadgeClass: Record<string, string> = {
  Synced: 'bg-green-500/20 text-green-400',
  OutOfSync: 'bg-amber-500/20 text-amber-400',
  Unknown: 'bg-gray-500/20 text-gray-400',
}

const healthBadgeClass: Record<string, string> = {
  Healthy: 'bg-green-500/20 text-green-400',
  Progressing: 'bg-cyan-500/20 text-cyan-400',
  Suspended: 'bg-purple-500/20 text-purple-400',
  Degraded: 'bg-red-500/20 text-red-400',
  Missing: 'bg-orange-500/20 text-orange-400',
  Unknown: 'bg-gray-500/20 text-gray-400',
}

function shortRevision(rev: string | null): string {
  if (!rev) return ''
  return rev.length > 8 ? rev.slice(0, 8) : rev
}

function relativeTime(iso: string | null): string {
  if (!iso) return 'never'
  const diff = Date.now() - new Date(iso).getTime()
  const m = Math.floor(diff / 60_000)
  if (m < 1) return 'just now'
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

interface Toast {
  id: number
  kind: 'success' | 'error'
  message: string
}

interface Props {
  /** When true, display the Sync Now button. False for non-superadmins. */
  canSync: boolean
}

export function ArgoCDApplicationsTable({ canSync }: Props) {
  const [apps, setApps] = useState<ArgoApplicationSummary[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [syncedAt, setSyncedAt] = useState<string | null>(null)
  const [confirmSync, setConfirmSync] = useState<ArgoApplicationSummary | null>(null)
  const [syncing, setSyncing] = useState<string | null>(null)
  const [toasts, setToasts] = useState<Toast[]>([])

  const pushToast = useCallback((toast: Omit<Toast, 'id'>) => {
    const id = Date.now() + Math.random()
    setToasts((prev) => [...prev, { ...toast, id }])
    setTimeout(() => setToasts((prev) => prev.filter((t) => t.id !== id)), 5000)
  }, [])

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true)
    try {
      const data = await fetchArgoApplications()
      setApps(data.applications)
      setSyncedAt(data.synced_at)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load ArgoCD applications')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
    const id = setInterval(() => load(true), POLL_INTERVAL_MS)
    return () => clearInterval(id)
  }, [load])

  const handleSync = useCallback(async () => {
    if (!confirmSync) return
    const name = confirmSync.name
    setSyncing(name)
    try {
      await syncArgoApplication(name)
      pushToast({ kind: 'success', message: `Sync triggered for ${name}` })
      setConfirmSync(null)
      // Refresh after a short delay to let ArgoCD pick up the operation.
      setTimeout(() => load(true), 1500)
    } catch (e) {
      pushToast({
        kind: 'error',
        message: e instanceof Error ? e.message : `Failed to sync ${name}`,
      })
    } finally {
      setSyncing(null)
    }
  }, [confirmSync, load, pushToast])

  if (loading && !apps) {
    return (
      <div className="flex justify-center py-12">
        <div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  if (error && !apps) {
    return <EmptyState icon={TriangleAlert} title="ArgoCD unavailable" description={error} />
  }

  if (!apps || apps.length === 0) {
    return (
      <EmptyState
        icon={GitBranch}
        title="No ArgoCD applications"
        description="Applications registered in the argocd namespace will appear here."
      />
    )
  }

  const outOfSync = apps.filter((a) => a.sync_status === 'OutOfSync').length
  const degraded = apps.filter((a) => a.health_status === 'Degraded').length

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3 rounded-md border border-border bg-card/30 p-3">
        <span className="text-sm">
          <span className="font-semibold">{apps.length}</span>{' '}
          <span className="text-muted-foreground">applications</span>
        </span>
        {outOfSync > 0 && (
          <Badge className="bg-amber-500/20 text-amber-400">{outOfSync} out of sync</Badge>
        )}
        {degraded > 0 && <Badge className="bg-red-500/20 text-red-400">{degraded} degraded</Badge>}
        <div className="ml-auto flex items-center gap-3">
          {syncedAt && (
            <Badge variant="secondary" className="font-mono text-xs">
              synced {relativeTime(syncedAt)}
            </Badge>
          )}
          <Button
            size="sm"
            variant="outline"
            onClick={() => load()}
            aria-label="Refresh applications"
          >
            <RefreshCw className="size-3.5 mr-1" />
            Refresh
          </Button>
        </div>
      </div>

      {error && (
        <div className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-400">
          Last refresh failed: {error}. Showing cached data.
        </div>
      )}

      <div className="rounded-lg border border-border overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-muted/40 text-xs">
            <tr>
              <th className="text-left p-3 font-semibold">Name</th>
              <th className="text-left p-3 font-semibold">Sync</th>
              <th className="text-left p-3 font-semibold">Health</th>
              <th className="text-left p-3 font-semibold">Revision</th>
              <th className="text-left p-3 font-semibold">Source</th>
              <th className="text-left p-3 font-semibold">Last Sync</th>
              {canSync && <th className="text-right p-3 font-semibold">Actions</th>}
            </tr>
          </thead>
          <tbody>
            {apps.map((app) => (
              <tr key={app.name} className="border-t border-border hover:bg-muted/20">
                <td className="p-3 font-mono text-xs font-semibold">{app.name}</td>
                <td className="p-3">
                  <Badge className={syncBadgeClass[app.sync_status] ?? syncBadgeClass.Unknown}>
                    {app.sync_status}
                  </Badge>
                </td>
                <td className="p-3">
                  <Badge
                    className={healthBadgeClass[app.health_status] ?? healthBadgeClass.Unknown}
                  >
                    {app.health_status}
                  </Badge>
                </td>
                <td className="p-3 font-mono text-xs">
                  <span title={app.current_revision ?? ''}>
                    {shortRevision(app.current_revision)}
                  </span>
                  {app.target_revision && app.target_revision !== app.current_revision && (
                    <span className="text-muted-foreground"> → {app.target_revision}</span>
                  )}
                </td>
                <td className="p-3 text-xs">
                  {app.source_repo ? (
                    <a
                      href={app.source_repo}
                      target="_blank"
                      rel="noreferrer"
                      className="text-primary hover:underline inline-flex items-center gap-1"
                    >
                      {app.source_path || 'repo'} <ArrowUpRight className="size-3" />
                    </a>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </td>
                <td className="p-3 text-xs font-mono">{relativeTime(app.last_sync_at)}</td>
                {canSync && (
                  <td className="p-3 text-right">
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={syncing === app.name}
                      onClick={() => setConfirmSync(app)}
                      aria-label={`Sync ${app.name}`}
                    >
                      <RefreshCw
                        className={`size-3.5 mr-1 ${syncing === app.name ? 'animate-spin' : ''}`}
                      />
                      Sync Now
                    </Button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <Dialog open={!!confirmSync} onOpenChange={(open) => !open && setConfirmSync(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Sync {confirmSync?.name}?</DialogTitle>
            <DialogDescription>
              This will trigger ArgoCD to reconcile{' '}
              <span className="font-mono">{confirmSync?.name}</span> against{' '}
              <span className="font-mono">{confirmSync?.target_revision || 'HEAD'}</span>. The
              operation runs against the live cluster and may roll deployments. Continue?
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmSync(null)} disabled={!!syncing}>
              Cancel
            </Button>
            <Button onClick={handleSync} disabled={!!syncing}>
              {syncing ? 'Syncing...' : 'Sync Now'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Toast viewport */}
      <div className="fixed bottom-4 right-4 z-50 space-y-2">
        {toasts.map((t) => (
          <div
            key={t.id}
            role="status"
            aria-live="polite"
            className={`rounded-md border px-4 py-3 text-sm shadow-lg flex items-center gap-2 max-w-md ${
              t.kind === 'success'
                ? 'border-green-500/40 bg-green-500/10 text-green-300'
                : 'border-red-500/40 bg-red-500/10 text-red-300'
            }`}
          >
            {t.kind === 'success' ? <Check className="size-4" /> : <X className="size-4" />}
            <span>{t.message}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
