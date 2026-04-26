'use client'

import { useMemo } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import type { TopologyResponse } from '@/lib/topology-api'
import { Cpu, ExternalLink, HardDrive } from 'lucide-react'

const phaseClasses: Record<string, string> = {
  Running: 'bg-green-500/20 text-green-400',
  Pending: 'bg-amber-500/20 text-amber-400',
  Succeeded: 'bg-blue-500/20 text-blue-400',
  Failed: 'bg-red-500/20 text-red-400',
  Unknown: 'bg-gray-500/20 text-gray-400',
}

function formatBytesShort(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KiB', 'MiB', 'GiB', 'TiB']
  let value = bytes / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(1)} ${units[unit]}`
}

function formatCpuShort(millicores: number): string {
  if (millicores < 1000) return `${millicores}m`
  return `${(millicores / 1000).toFixed(2)}c`
}

function formatAge(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  const m = Math.floor(seconds / 60)
  if (m < 60) return `${m}m`
  const h = Math.floor(m / 60)
  if (h < 48) return `${h}h`
  return `${Math.floor(h / 24)}d`
}

interface NodeDrawerProps {
  nodeName: string | null
  topology: TopologyResponse
  onClose: () => void
  onSelectPod: (namespace: string, name: string) => void
}

export function TopologyNodeDrawer({
  nodeName,
  topology,
  onClose,
  onSelectPod,
}: NodeDrawerProps) {
  const node = useMemo(
    () => (nodeName ? topology.nodes.find((n) => n.name === nodeName) : null),
    [nodeName, topology]
  )
  const pods = useMemo(
    () => (nodeName ? topology.pods.filter((p) => p.node === nodeName) : []),
    [nodeName, topology]
  )

  return (
    <Dialog open={!!nodeName} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {node?.name}
            <Badge variant="outline" className="font-mono text-xs">
              {node?.role}
            </Badge>
            <Badge
              className={
                node?.status === 'Ready'
                  ? 'bg-green-500/20 text-green-400'
                  : 'bg-red-500/20 text-red-400'
              }
            >
              {node?.status}
            </Badge>
          </DialogTitle>
          <DialogDescription>
            {node?.kubelet_version} • {node?.os_image}
          </DialogDescription>
        </DialogHeader>

        {node && (
          <div className="space-y-4">
            <div className="grid grid-cols-3 gap-3 text-sm">
              <div className="rounded border border-border p-3">
                <div className="text-xs text-muted-foreground flex items-center gap-1 mb-1">
                  <Cpu className="size-3" /> CPU
                </div>
                <div className="font-mono text-xs">
                  {formatCpuShort(node.used.cpu_millicores)} /{' '}
                  {formatCpuShort(node.allocatable.cpu_millicores)}
                </div>
              </div>
              <div className="rounded border border-border p-3">
                <div className="text-xs text-muted-foreground flex items-center gap-1 mb-1">
                  <HardDrive className="size-3" /> Memory
                </div>
                <div className="font-mono text-xs">
                  {formatBytesShort(node.used.memory_bytes)} /{' '}
                  {formatBytesShort(node.allocatable.memory_bytes)}
                </div>
              </div>
              <div className="rounded border border-border p-3">
                <div className="text-xs text-muted-foreground mb-1">Pods</div>
                <div className="font-mono text-xs">
                  {node.used.pod_count} / {node.allocatable.pods}
                </div>
              </div>
            </div>

            {node.taints.length > 0 && (
              <div>
                <div className="text-xs font-semibold mb-1">Taints</div>
                <div className="flex flex-wrap gap-1">
                  {node.taints.map((t, i) => (
                    <Badge key={i} variant="outline" className="font-mono text-[10px]">
                      {t.key}={t.value ?? ''}:{t.effect}
                    </Badge>
                  ))}
                </div>
              </div>
            )}

            <div>
              <div className="text-xs font-semibold mb-2">Pods on this node ({pods.length})</div>
              <div className="rounded border border-border max-h-72 overflow-y-auto">
                <table className="w-full text-xs">
                  <thead className="bg-muted/40 sticky top-0">
                    <tr>
                      <th className="text-left p-2">Namespace</th>
                      <th className="text-left p-2">Name</th>
                      <th className="text-left p-2">Phase</th>
                      <th className="text-left p-2">Ready</th>
                      <th className="text-left p-2">Restarts</th>
                      <th className="text-left p-2">Age</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pods.length === 0 ? (
                      <tr>
                        <td colSpan={6} className="p-3 text-center text-muted-foreground">
                          No pods scheduled.
                        </td>
                      </tr>
                    ) : (
                      pods.map((p) => (
                        <tr
                          key={`${p.namespace}/${p.name}`}
                          className="border-t border-border hover:bg-muted/20 cursor-pointer"
                          onClick={() => onSelectPod(p.namespace, p.name)}
                        >
                          <td className="p-2 font-mono">{p.namespace}</td>
                          <td className="p-2 font-mono truncate max-w-[200px]">{p.name}</td>
                          <td className="p-2">
                            <Badge className={phaseClasses[p.phase] ?? phaseClasses.Unknown}>
                              {p.phase}
                            </Badge>
                          </td>
                          <td className="p-2 font-mono">{p.ready}</td>
                          <td
                            className={`p-2 font-mono ${
                              p.restart_count > 0 ? 'text-amber-400' : ''
                            }`}
                          >
                            {p.restart_count}
                          </td>
                          <td className="p-2 font-mono">{formatAge(p.age_seconds)}</td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

interface PodDrawerProps {
  pod: { namespace: string; name: string } | null
  topology: TopologyResponse
  onClose: () => void
}

export function TopologyPodDrawer({ pod, topology, onClose }: PodDrawerProps) {
  const podData = useMemo(
    () =>
      pod ? topology.pods.find((p) => p.namespace === pod.namespace && p.name === pod.name) : null,
    [pod, topology]
  )

  return (
    <Dialog open={!!pod} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <span className="font-mono text-sm break-all">{podData?.name}</span>
            {podData && (
              <Badge className={phaseClasses[podData.phase] ?? phaseClasses.Unknown}>
                {podData.phase}
              </Badge>
            )}
          </DialogTitle>
          <DialogDescription>
            {podData?.namespace} • on node {podData?.node}
          </DialogDescription>
        </DialogHeader>

        {podData && (
          <div className="space-y-4 text-sm">
            <div className="grid grid-cols-3 gap-2 text-xs">
              <div className="rounded border border-border p-2">
                <div className="text-muted-foreground">Ready</div>
                <div className="font-mono">{podData.ready}</div>
              </div>
              <div className="rounded border border-border p-2">
                <div className="text-muted-foreground">Restarts</div>
                <div
                  className={`font-mono ${podData.restart_count > 0 ? 'text-amber-400' : ''}`}
                >
                  {podData.restart_count}
                </div>
              </div>
              <div className="rounded border border-border p-2">
                <div className="text-muted-foreground">Age</div>
                <div className="font-mono">{formatAge(podData.age_seconds)}</div>
              </div>
            </div>

            <div>
              <div className="text-xs font-semibold mb-1">Containers</div>
              <div className="space-y-2">
                {podData.containers.map((c) => (
                  <div key={c.name} className="rounded border border-border p-2 text-xs">
                    <div className="flex items-center justify-between mb-1">
                      <span className="font-semibold">{c.name}</span>
                      <Badge
                        className={
                          c.ready
                            ? 'bg-green-500/20 text-green-400'
                            : 'bg-red-500/20 text-red-400'
                        }
                      >
                        {c.ready ? 'ready' : 'not ready'}
                      </Badge>
                    </div>
                    <div className="font-mono text-[10px] text-muted-foreground break-all">
                      {c.image}
                    </div>
                  </div>
                ))}
              </div>
            </div>

            <div className="text-xs text-muted-foreground">
              Logs:{' '}
              <a
                href={`/api/admin/logs?namespace=${encodeURIComponent(podData.namespace)}&pod=${encodeURIComponent(podData.name)}`}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1 text-primary hover:underline"
              >
                view stream <ExternalLink className="size-3" />
              </a>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
