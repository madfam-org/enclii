'use client'

import { useEffect, useState } from 'react'
import { clusterApi, vclusterApi } from '@/lib/admin-api'
import type { Cluster, VirtualCluster } from '@/types/admin'
import { Badge } from '@/components/ui/badge'
import { Layers, Cloud, Server } from 'lucide-react'

const statusColors: Record<string, string> = {
  ready: 'bg-green-500/20 text-green-400',
  pending: 'bg-amber-500/20 text-amber-400',
  degraded: 'bg-orange-500/20 text-orange-400',
  offline: 'bg-red-500/20 text-red-400',
  running: 'bg-green-500/20 text-green-400',
  creating: 'bg-cyan-500/20 text-cyan-400',
  error: 'bg-red-500/20 text-red-400',
}

export function ClusterList() {
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [vclusters, setVClusters] = useState<VirtualCluster[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    Promise.all([
      clusterApi.list().then((d) => setClusters(d.clusters || [])),
      vclusterApi.list().then((d) => setVClusters(d.vclusters || [])),
    ]).finally(() => setLoading(false))
  }, [])

  if (loading) {
    return <div className="flex justify-center py-12"><div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" /></div>
  }

  return (
    <div className="space-y-6">
      <div>
        <h3 className="text-lg font-semibold mb-3 flex items-center gap-2"><Server className="size-5" /> Physical Clusters</h3>
        {clusters.length === 0 ? (
          <p className="text-muted-foreground text-sm">No clusters registered.</p>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {clusters.map((c) => (
              <div key={c.id} className="rounded-lg border border-border bg-card/50 p-4">
                <div className="flex items-center justify-between mb-2">
                  <h4 className="font-semibold">{c.name}</h4>
                  <Badge className={statusColors[c.status]}>{c.status}</Badge>
                </div>
                <p className="text-xs text-muted-foreground font-mono">{c.type} • {c.region || 'no region'}</p>
                {c.endpoint && <p className="text-xs text-muted-foreground mt-1 truncate">{c.endpoint}</p>}
              </div>
            ))}
          </div>
        )}
      </div>

      <div>
        <h3 className="text-lg font-semibold mb-3 flex items-center gap-2"><Cloud className="size-5" /> Virtual Clusters</h3>
        {vclusters.length === 0 ? (
          <p className="text-muted-foreground text-sm">No virtual clusters provisioned.</p>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {vclusters.map((vc) => (
              <div key={vc.id} className="rounded-lg border border-border bg-card/50 p-4">
                <div className="flex items-center justify-between mb-2">
                  <h4 className="font-semibold">{vc.name}</h4>
                  <Badge className={statusColors[vc.status]}>{vc.status}</Badge>
                </div>
                <p className="text-xs text-muted-foreground font-mono">ns: {vc.namespace} • {vc.k8s_version || 'auto'}</p>
                {vc.tenant_id && <p className="text-xs text-muted-foreground mt-1">Tenant: {vc.tenant_id}</p>}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
