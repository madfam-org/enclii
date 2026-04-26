'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  type Node,
  type Edge,
  type NodeMouseHandler,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Box, Cpu, HardDrive, Network, RefreshCw, Server } from 'lucide-react'
import { fetchTopology, type TopologyResponse } from '@/lib/topology-api'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { TopologyNodeDrawer, TopologyPodDrawer } from './topology-drawers'

const POLL_INTERVAL_MS = 60_000

const phaseColors: Record<string, string> = {
  Running: '#22c55e',
  Pending: '#f59e0b',
  Succeeded: '#3b82f6',
  Failed: '#ef4444',
  Unknown: '#6b7280',
}

const nodeRoleColors: Record<string, string> = {
  'control-plane': '#a855f7',
  worker: '#3b82f6',
  builder: '#f59e0b',
  unknown: '#6b7280',
}

function formatRelative(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const secs = Math.max(0, Math.floor(diff / 1000))
  if (secs < 5) return 'just now'
  if (secs < 60) return `${secs}s ago`
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  return `${hrs}h ago`
}

function pct(used: number, capacity: number): number {
  if (!capacity) return 0
  return Math.min(100, Math.round((used / capacity) * 100))
}

interface CanvasGraph {
  nodes: Node[]
  edges: Edge[]
}

function buildGraph(data: TopologyResponse): CanvasGraph {
  const flowNodes: Node[] = []
  const flowEdges: Edge[] = []

  const NODE_WIDTH = 240
  const NODE_GAP_X = 60
  const NODE_Y = 0

  data.nodes.forEach((n, idx) => {
    const cpuPct = pct(n.used.cpu_millicores, n.allocatable.cpu_millicores)
    const memPct = pct(n.used.memory_bytes, n.allocatable.memory_bytes)
    flowNodes.push({
      id: `node:${n.name}`,
      type: 'default',
      position: { x: idx * (NODE_WIDTH + NODE_GAP_X), y: NODE_Y },
      data: {
        label: (
          <div className="text-left text-xs">
            <div className="flex items-center justify-between gap-2 mb-1">
              <span className="font-semibold truncate">{n.name}</span>
              <span
                className={`size-2 rounded-full ${
                  n.status === 'Ready' ? 'bg-green-500' : 'bg-red-500'
                }`}
                aria-label={`Status: ${n.status}`}
              />
            </div>
            <div className="text-[10px] text-muted-foreground mb-2">
              {n.role} • {n.kubelet_version || 'unknown'}
            </div>
            <div className="space-y-1">
              <div className="flex justify-between text-[10px]">
                <span>CPU</span>
                <span>{cpuPct}%</span>
              </div>
              <div className="h-1 rounded bg-muted overflow-hidden">
                <div className="h-full bg-blue-500" style={{ width: `${cpuPct}%` }} />
              </div>
              <div className="flex justify-between text-[10px]">
                <span>Memory</span>
                <span>{memPct}%</span>
              </div>
              <div className="h-1 rounded bg-muted overflow-hidden">
                <div className="h-full bg-purple-500" style={{ width: `${memPct}%` }} />
              </div>
              <div className="flex justify-between text-[10px] pt-1">
                <span>Pods</span>
                <span>
                  {n.used.pod_count}/{n.allocatable.pods}
                </span>
              </div>
            </div>
          </div>
        ),
      },
      style: {
        width: NODE_WIDTH,
        background: 'hsl(var(--card))',
        border: `2px solid ${nodeRoleColors[n.role] || nodeRoleColors.unknown}`,
        borderRadius: 8,
        padding: 12,
      },
    })
  })

  const NS_Y = 260
  const NS_WIDTH = 160
  const NS_GAP_X = 24
  const PER_ROW = Math.max(
    1,
    Math.floor((data.nodes.length * (NODE_WIDTH + NODE_GAP_X)) / (NS_WIDTH + NS_GAP_X))
  )
  const visibleNamespaces = data.namespaces.filter((ns) => ns.pod_count > 0)
  visibleNamespaces.forEach((ns, idx) => {
    const row = Math.floor(idx / PER_ROW)
    const col = idx % PER_ROW
    const failedCount = ns.pod_phases.Failed ?? 0
    const pendingCount = ns.pod_phases.Pending ?? 0
    const runningCount = ns.pod_phases.Running ?? 0
    const colorBorder =
      failedCount > 0
        ? phaseColors.Failed
        : pendingCount > 0
          ? phaseColors.Pending
          : phaseColors.Running

    flowNodes.push({
      id: `ns:${ns.name}`,
      type: 'default',
      position: { x: col * (NS_WIDTH + NS_GAP_X), y: NS_Y + row * 110 },
      data: {
        label: (
          <div className="text-left text-xs">
            <div className="font-semibold truncate mb-1">{ns.name}</div>
            <div className="text-[10px] text-muted-foreground space-y-0.5">
              <div>
                {ns.pod_count} pods • {ns.deployment_count} deploys
              </div>
              <div className="flex gap-2">
                {runningCount > 0 && <span className="text-green-500">✓ {runningCount}</span>}
                {pendingCount > 0 && <span className="text-amber-500">⏱ {pendingCount}</span>}
                {failedCount > 0 && <span className="text-red-500">✗ {failedCount}</span>}
              </div>
            </div>
          </div>
        ),
      },
      style: {
        width: NS_WIDTH,
        background: 'hsl(var(--card))',
        border: `1px solid ${colorBorder}`,
        borderRadius: 6,
        padding: 8,
      },
    })
  })

  const nsToNodes = new Map<string, Set<string>>()
  for (const p of data.pods) {
    if (!p.node) continue
    const set = nsToNodes.get(p.namespace) ?? new Set<string>()
    set.add(p.node)
    nsToNodes.set(p.namespace, set)
  }
  for (const [ns, nodeNames] of nsToNodes) {
    if (!visibleNamespaces.find((v) => v.name === ns)) continue
    for (const nName of nodeNames) {
      flowEdges.push({
        id: `${ns}->${nName}`,
        source: `ns:${ns}`,
        target: `node:${nName}`,
        style: { stroke: '#6b728060', strokeWidth: 1 },
      })
    }
  }

  return { nodes: flowNodes, edges: flowEdges }
}

export function TopologyCanvas() {
  const [data, setData] = useState<TopologyResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [selectedNode, setSelectedNode] = useState<string | null>(null)
  const [selectedPod, setSelectedPod] = useState<{ namespace: string; name: string } | null>(null)

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true)
    setRefreshing(true)
    try {
      const fresh = await fetchTopology()
      setData(fresh)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load topology')
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [])

  useEffect(() => {
    load()
    const id = setInterval(() => load(true), POLL_INTERVAL_MS)
    return () => clearInterval(id)
  }, [load])

  const graph = useMemo<CanvasGraph>(
    () => (data ? buildGraph(data) : { nodes: [], edges: [] }),
    [data]
  )

  const handleNodeClick = useCallback<NodeMouseHandler>((_, node) => {
    if (node.id.startsWith('node:')) {
      setSelectedNode(node.id.slice('node:'.length))
    }
  }, [])

  if (loading && !data) {
    return (
      <div className="flex justify-center py-12">
        <div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  if (error && !data) {
    return <EmptyState icon={Network} title="Topology unavailable" description={error} />
  }

  if (!data || graph.nodes.length === 0) {
    return (
      <EmptyState
        icon={Network}
        title="No topology data"
        description="Cluster returned no nodes."
      />
    )
  }

  return (
    <div className="h-full flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3 rounded-md border border-border bg-card/30 p-3">
        <div className="flex items-center gap-2 text-sm">
          <Server className="size-4 text-muted-foreground" />
          <span className="font-medium">{data.totals.nodes}</span>
          <span className="text-muted-foreground">nodes</span>
        </div>
        <div className="flex items-center gap-2 text-sm">
          <Box className="size-4 text-muted-foreground" />
          <span className="font-medium">{data.totals.pods}</span>
          <span className="text-muted-foreground">pods</span>
        </div>
        <div className="flex items-center gap-2 text-sm">
          <Network className="size-4 text-muted-foreground" />
          <span className="font-medium">{data.totals.services}</span>
          <span className="text-muted-foreground">services</span>
        </div>
        <div className="flex items-center gap-2 text-sm">
          <Cpu className="size-4 text-muted-foreground" />
          <span className="font-mono text-xs">
            {data.display.cpu_used} / {data.display.cpu_capacity}
          </span>
        </div>
        <div className="flex items-center gap-2 text-sm">
          <HardDrive className="size-4 text-muted-foreground" />
          <span className="font-mono text-xs">
            {data.display.memory_used} / {data.display.memory_capacity}
          </span>
        </div>
        <div className="ml-auto flex items-center gap-3">
          <Badge variant="secondary" className="font-mono text-xs">
            synced {formatRelative(data.synced_at)}
          </Badge>
          <Button
            size="sm"
            variant="outline"
            onClick={() => load()}
            disabled={refreshing}
            aria-label="Refresh topology"
          >
            <RefreshCw className={`size-3.5 mr-1 ${refreshing ? 'animate-spin' : ''}`} />
            Refresh
          </Button>
        </div>
      </div>

      {error && (
        <div className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-400">
          Last refresh failed: {error}. Showing cached data.
        </div>
      )}

      <div className="flex-1 rounded-lg border border-border overflow-hidden">
        <ReactFlow nodes={graph.nodes} edges={graph.edges} fitView onNodeClick={handleNodeClick}>
          <Background />
          <Controls />
        </ReactFlow>
      </div>

      <TopologyNodeDrawer
        nodeName={selectedNode}
        topology={data}
        onClose={() => setSelectedNode(null)}
        onSelectPod={(ns, name) => {
          setSelectedNode(null)
          setSelectedPod({ namespace: ns, name })
        }}
      />
      <TopologyPodDrawer
        pod={selectedPod}
        topology={data}
        onClose={() => setSelectedPod(null)}
      />
    </div>
  )
}
