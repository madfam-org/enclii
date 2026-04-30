'use client'

import { useCallback, useEffect, useState } from 'react'
import { HardDrive, RefreshCw, ShieldAlert } from 'lucide-react'
import {
  fetchStorageVolumes,
  type LonghornVolumeSummary,
  type StorageVolumesResponse,
} from '@/lib/storage-api'
import { Badge } from "@enclii/ui-components/badge"
import { Button } from "@enclii/ui-components/button"
import { EmptyState } from '@/components/empty-state'

const POLL_INTERVAL_MS = 5 * 60_000

const robustnessClass: Record<string, string> = {
  healthy: 'bg-green-500/20 text-green-400',
  degraded: 'bg-amber-500/20 text-amber-400',
  faulted: 'bg-red-500/20 text-red-400',
  unknown: 'bg-gray-500/20 text-gray-400',
}

const stateClass: Record<string, string> = {
  attached: 'bg-blue-500/20 text-blue-400',
  detached: 'bg-gray-500/20 text-gray-400',
  attaching: 'bg-cyan-500/20 text-cyan-400',
  detaching: 'bg-cyan-500/20 text-cyan-400',
  deleting: 'bg-red-500/20 text-red-400',
}

const rowAccent: Record<string, string> = {
  faulted: 'bg-red-500/5',
  degraded: 'bg-amber-500/5',
}

function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const m = Math.floor(diff / 60_000)
  if (m < 1) return 'just now'
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

export function VolumesTable() {
  const [data, setData] = useState<StorageVolumesResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true)
    try {
      const fresh = await fetchStorageVolumes()
      setData(fresh)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load volumes')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
    const id = setInterval(() => load(true), POLL_INTERVAL_MS)
    return () => clearInterval(id)
  }, [load])

  if (loading && !data) {
    return (
      <div className="flex justify-center py-12">
        <div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  if (error && !data) {
    return <EmptyState icon={ShieldAlert} title="Longhorn unavailable" description={error} />
  }

  if (!data || data.volumes.length === 0) {
    return (
      <EmptyState
        icon={HardDrive}
        title="No Longhorn volumes"
        description="No volumes were returned by longhorn-system. Verify Longhorn is installed."
      />
    )
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 lg:grid-cols-5 gap-3">
        <SummaryCard label="Total" value={data.summary.total} />
        <SummaryCard
          label="Healthy"
          value={data.summary.healthy}
          color="text-green-400"
        />
        <SummaryCard
          label="Degraded"
          value={data.summary.degraded}
          color="text-amber-400"
        />
        <SummaryCard
          label="Faulted"
          value={data.summary.faulted}
          color="text-red-400"
        />
        <SummaryCard label="Capacity" value={data.summary.total_size_display} />
      </div>

      <div className="flex items-center gap-3">
        <Badge variant="secondary" className="font-mono text-xs">
          synced {relativeTime(data.synced_at)}
        </Badge>
        <Button size="sm" variant="outline" onClick={() => load()}>
          <RefreshCw className="size-3.5 mr-1" />
          Refresh
        </Button>
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
              <th className="text-left p-3 font-semibold">Volume</th>
              <th className="text-left p-3 font-semibold">PVC</th>
              <th className="text-left p-3 font-semibold">State</th>
              <th className="text-left p-3 font-semibold">Robustness</th>
              <th className="text-left p-3 font-semibold">Size</th>
              <th className="text-left p-3 font-semibold">Replicas</th>
              <th className="text-left p-3 font-semibold">Attached To</th>
            </tr>
          </thead>
          <tbody>
            {data.volumes.map((v) => (
              <VolumeRow key={v.name} volume={v} />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function SummaryCard({
  label,
  value,
  color,
}: {
  label: string
  value: number | string
  color?: string
}) {
  return (
    <div className="rounded-lg border border-border bg-card/50 p-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={`text-2xl font-semibold mt-1 ${color ?? ''}`}>{value}</div>
    </div>
  )
}

function VolumeRow({ volume }: { volume: LonghornVolumeSummary }) {
  const replicasRunning = volume.replicas.filter((r) => r.running).length
  return (
    <tr
      className={`border-t border-border align-top ${rowAccent[volume.robustness] ?? ''}`}
    >
      <td className="p-3 font-mono text-xs font-semibold break-all">{volume.name}</td>
      <td className="p-3 text-xs">
        {volume.pvc_name ? (
          <>
            <div className="font-mono">{volume.pvc_name}</div>
            <div className="text-muted-foreground">{volume.namespace}</div>
          </>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </td>
      <td className="p-3">
        <Badge className={stateClass[volume.state] ?? stateClass.detached}>{volume.state}</Badge>
      </td>
      <td className="p-3">
        <Badge className={robustnessClass[volume.robustness] ?? robustnessClass.unknown}>
          {volume.robustness}
        </Badge>
      </td>
      <td className="p-3 font-mono text-xs">{volume.size_display}</td>
      <td className="p-3 text-xs">
        <div className="font-mono">
          {replicasRunning}/{volume.replica_count_target}
        </div>
        <div className="text-[10px] text-muted-foreground space-y-0.5 mt-1">
          {volume.replicas.map((r) => (
            <div key={r.name} className="flex items-center gap-1">
              <span
                className={`size-1.5 rounded-full ${
                  r.running ? 'bg-green-500' : 'bg-red-500'
                }`}
              />
              <span className="font-mono">{r.node}</span>
              {r.mode && <span className="text-muted-foreground/70">({r.mode})</span>}
            </div>
          ))}
        </div>
      </td>
      <td className="p-3 text-xs">
        {volume.attached_to_node ? (
          <>
            <div className="font-mono">{volume.attached_to_node}</div>
            {volume.attached_to_pod && (
              <div className="text-muted-foreground font-mono text-[10px]">
                {volume.attached_to_pod}
              </div>
            )}
          </>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </td>
    </tr>
  )
}
