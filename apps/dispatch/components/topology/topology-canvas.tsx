'use client'

import { useEffect, useState, useCallback } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  type Node,
  type Edge,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { topologyApi } from '@/lib/admin-api'
import type { TopologyNode, TopologyEdge } from '@/types/admin'
import { Network } from 'lucide-react'
import { EmptyState } from '@/components/empty-state'

const nodeColors: Record<string, string> = {
  cluster: '#22c55e',
  bmh: '#3b82f6',
  vcluster: '#a855f7',
  service: '#f59e0b',
}

function mapNodes(nodes: TopologyNode[]): Node[] {
  return nodes.map((n) => ({
    id: n.id,
    data: { label: n.label },
    position: n.position,
    style: {
      background: `${nodeColors[n.type] || '#6b7280'}20`,
      border: `1px solid ${nodeColors[n.type] || '#6b7280'}`,
      borderRadius: '8px',
      padding: '8px 12px',
      fontSize: '12px',
      color: nodeColors[n.type] || '#6b7280',
    },
  }))
}

function mapEdges(edges: TopologyEdge[]): Edge[] {
  return edges.map((e) => ({
    id: e.id,
    source: e.source,
    target: e.target,
    label: e.label,
    style: { stroke: '#6b728080' },
    labelStyle: { fontSize: 10, fill: '#9ca3af' },
  }))
}

export function TopologyCanvas() {
  const [nodes, setNodes] = useState<Node[]>([])
  const [edges, setEdges] = useState<Edge[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    topologyApi.get().then((data) => {
      setNodes(mapNodes(data.nodes || []))
      setEdges(mapEdges(data.edges || []))
    }).finally(() => setLoading(false))
  }, [])

  if (loading) {
    return <div className="flex justify-center py-12"><div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" /></div>
  }

  if (nodes.length === 0) {
    return <EmptyState icon={Network} title="No Topology Data" description="Register clusters and hosts to visualize your infrastructure topology." />
  }

  return (
    <div className="h-full rounded-lg border border-border overflow-hidden">
      <ReactFlow nodes={nodes} edges={edges} fitView>
        <Background />
        <Controls />
      </ReactFlow>
    </div>
  )
}
