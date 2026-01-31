'use client'

import { ClusterList } from '@/components/clusters/cluster-list'

export default function ClustersPage() {
  return (
    <div>
      <h2 className="text-2xl font-semibold mb-6">Clusters</h2>
      <ClusterList />
    </div>
  )
}
