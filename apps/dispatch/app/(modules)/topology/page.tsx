'use client'

import { TopologyCanvas } from '@/components/topology/topology-canvas'

export default function TopologyPage() {
  return (
    <div className="h-[calc(100vh-130px)]">
      <h2 className="text-2xl font-semibold mb-4">Infrastructure Topology</h2>
      <TopologyCanvas />
    </div>
  )
}
