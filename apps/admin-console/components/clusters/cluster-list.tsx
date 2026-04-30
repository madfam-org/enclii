'use client'

import { useEffect, useState } from 'react'
import { clusterApi, vclusterApi } from '@/lib/admin-api'
import type { Cluster, VirtualCluster } from '@/types/admin'
import { Badge } from "@enclii/ui-components/badge"
import { Button } from "@enclii/ui-components/button"
import { Input } from "@enclii/ui-components/input"
import { Label } from "@enclii/ui-components/label"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@enclii/ui-components/dialog"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@enclii/ui-components/select"
import { Layers, Cloud, Server, Plus, Trash2, Download, Cpu, HardDrive, Box, Clock } from 'lucide-react'

const statusColors: Record<string, string> = {
  ready: 'bg-green-500/20 text-green-400',
  pending: 'bg-amber-500/20 text-amber-400',
  degraded: 'bg-orange-500/20 text-orange-400',
  offline: 'bg-red-500/20 text-red-400',
  running: 'bg-green-500/20 text-green-400',
  creating: 'bg-cyan-500/20 text-cyan-400',
  error: 'bg-red-500/20 text-red-400',
}

function relativeTime(dateStr: string | undefined): string {
  if (!dateStr) return ''
  const diff = Date.now() - new Date(dateStr).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

interface ClusterMetadata {
  node_count?: number
  k8s_version?: string
  cpu_capacity?: string
  memory_capacity?: string
  pod_count?: number
  synced_at?: string
}

function parseMetadata(metadata: Record<string, unknown> | undefined): ClusterMetadata {
  if (!metadata) return {}
  const m = metadata as Record<string, unknown>
  return {
    node_count: m.node_count as number | undefined,
    k8s_version: m.k8s_version as string | undefined,
    cpu_capacity: m.cpu_capacity as string | undefined ?? (m.cpu_millicores ? `${Math.round(Number(m.cpu_millicores) / 1000)}c` : undefined),
    memory_capacity: m.memory_capacity as string | undefined ?? (m.memory_bytes ? `${Math.round(Number(m.memory_bytes) / (1024 ** 3))}Gi` : undefined),
    pod_count: m.pod_count as number | undefined,
    synced_at: m.synced_at as string | undefined,
  }
}

export function ClusterList() {
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [vclusters, setVClusters] = useState<VirtualCluster[]>([])
  const [loading, setLoading] = useState(true)
  const [showRegister, setShowRegister] = useState(false)
  const [showProvision, setShowProvision] = useState(false)
  const [confirmDeregister, setConfirmDeregister] = useState<Cluster | null>(null)
  const [confirmTeardown, setConfirmTeardown] = useState<VirtualCluster | null>(null)
  const [actionLoading, setActionLoading] = useState(false)

  // Register cluster form state
  const [clusterForm, setClusterForm] = useState<{ name: string; type: Cluster['type']; endpoint: string }>({ name: '', type: 'k3s', endpoint: '' })

  // Provision vCluster form state
  const [vclusterForm, setVclusterForm] = useState({ name: '', namespace: '', host_cluster_id: '' })

  const fetchAll = async () => {
    setLoading(true)
    await Promise.all([
      clusterApi.list().then((d) => setClusters(d.clusters || [])),
      vclusterApi.list().then((d) => setVClusters(d.vclusters || [])),
    ]).finally(() => setLoading(false))
  }

  useEffect(() => {
    fetchAll()
  }, [])

  const handleRegister = async () => {
    setActionLoading(true)
    try {
      await clusterApi.register(clusterForm)
      setShowRegister(false)
      setClusterForm({ name: '', type: 'k3s', endpoint: '' })
      await fetchAll()
    } catch {
      // error handled by adminFetch
    } finally {
      setActionLoading(false)
    }
  }

  const handleDeregister = async () => {
    if (!confirmDeregister) return
    setActionLoading(true)
    try {
      await clusterApi.deregister(confirmDeregister.id)
      setConfirmDeregister(null)
      await fetchAll()
    } catch {
      // error handled by adminFetch
    } finally {
      setActionLoading(false)
    }
  }

  const handleProvision = async () => {
    setActionLoading(true)
    try {
      await vclusterApi.provision(vclusterForm)
      setShowProvision(false)
      setVclusterForm({ name: '', namespace: '', host_cluster_id: '' })
      await fetchAll()
    } catch {
      // error handled by adminFetch
    } finally {
      setActionLoading(false)
    }
  }

  const handleTeardown = async () => {
    if (!confirmTeardown) return
    setActionLoading(true)
    try {
      await vclusterApi.teardown(confirmTeardown.id)
      setConfirmTeardown(null)
      await fetchAll()
    } catch {
      // error handled by adminFetch
    } finally {
      setActionLoading(false)
    }
  }

  const handleKubeconfig = async (id: string) => {
    try {
      const data = await vclusterApi.kubeconfig(id)
      const blob = new Blob([data.kubeconfig], { type: 'text/yaml' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `kubeconfig-${id}.yaml`
      a.click()
      URL.revokeObjectURL(url)
    } catch {
      // error handled by adminFetch
    }
  }

  if (loading) {
    return <div className="flex justify-center py-12"><div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" /></div>
  }

  return (
    <div className="space-y-6">
      {/* Physical Clusters */}
      <div>
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-lg font-semibold flex items-center gap-2"><Server className="size-5" /> Physical Clusters</h3>
          <Button size="sm" onClick={() => setShowRegister(true)}>
            <Plus className="size-4 mr-2" />
            Register Cluster
          </Button>
        </div>
        {clusters.length === 0 ? (
          <p className="text-muted-foreground text-sm">No clusters registered.</p>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {clusters.map((c) => {
              const meta = parseMetadata(c.metadata)
              return (
                <div key={c.id} className="rounded-lg border border-border bg-card/50 p-4">
                  <div className="flex items-center justify-between mb-2">
                    <h4 className="font-semibold">{c.name}</h4>
                    <Badge className={statusColors[c.status]}>{c.status}</Badge>
                  </div>
                  <p className="text-xs text-muted-foreground font-mono">{c.type} • {c.region || 'no region'}</p>
                  {c.endpoint && <p className="text-xs text-muted-foreground mt-1 truncate">{c.endpoint}</p>}

                  {/* Reconciler metadata */}
                  {(meta.node_count || meta.k8s_version || meta.pod_count) && (
                    <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                      {meta.k8s_version && (
                        <span className="flex items-center gap-1"><Layers className="size-3" />{meta.k8s_version}</span>
                      )}
                      {meta.node_count != null && (
                        <span className="flex items-center gap-1"><Server className="size-3" />{meta.node_count} nodes</span>
                      )}
                      {meta.cpu_capacity && (
                        <span className="flex items-center gap-1"><Cpu className="size-3" />{meta.cpu_capacity}</span>
                      )}
                      {meta.memory_capacity && (
                        <span className="flex items-center gap-1"><HardDrive className="size-3" />{meta.memory_capacity}</span>
                      )}
                      {meta.pod_count != null && (
                        <span className="flex items-center gap-1"><Box className="size-3" />{meta.pod_count} pods</span>
                      )}
                      {meta.synced_at && (
                        <span className="flex items-center gap-1"><Clock className="size-3" />synced {relativeTime(meta.synced_at)}</span>
                      )}
                    </div>
                  )}

                  <div className="mt-3 flex justify-end">
                    <Button variant="ghost" size="sm" onClick={() => setConfirmDeregister(c)}>
                      <Trash2 className="size-3.5 mr-1" /> Deregister
                    </Button>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Virtual Clusters */}
      <div>
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-lg font-semibold flex items-center gap-2"><Cloud className="size-5" /> Virtual Clusters</h3>
          <Button size="sm" onClick={() => setShowProvision(true)}>
            <Plus className="size-4 mr-2" />
            Provision vCluster
          </Button>
        </div>
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
                <div className="mt-3 flex justify-end gap-1">
                  <Button variant="ghost" size="sm" onClick={() => handleKubeconfig(vc.id)}>
                    <Download className="size-3.5 mr-1" /> Kubeconfig
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => setConfirmTeardown(vc)}>
                    <Trash2 className="size-3.5 mr-1" /> Teardown
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Register Cluster Dialog */}
      <Dialog open={showRegister} onOpenChange={setShowRegister}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Register Cluster</DialogTitle>
            <DialogDescription>Add a new physical cluster to the fleet.</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="cluster-name">Name</Label>
              <Input id="cluster-name" value={clusterForm.name} onChange={(e) => setClusterForm({ ...clusterForm, name: e.target.value })} placeholder="foundry-core" />
            </div>
            <div className="space-y-2">
              <Label>Type</Label>
              <Select value={clusterForm.type} onValueChange={(v) => setClusterForm({ ...clusterForm, type: v as 'k3s' | 'k8s' | 'vcluster' })}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="k3s">k3s</SelectItem>
                  <SelectItem value="k8s">k8s</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="cluster-endpoint">Endpoint</Label>
              <Input id="cluster-endpoint" value={clusterForm.endpoint} onChange={(e) => setClusterForm({ ...clusterForm, endpoint: e.target.value })} placeholder="https://10.0.0.1:6443" />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowRegister(false)} disabled={actionLoading}>Cancel</Button>
            <Button onClick={handleRegister} disabled={actionLoading || !clusterForm.name}>
              {actionLoading ? 'Registering...' : 'Register'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Deregister Confirm Dialog */}
      <Dialog open={!!confirmDeregister} onOpenChange={() => setConfirmDeregister(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Deregister Cluster — {confirmDeregister?.name}</DialogTitle>
            <DialogDescription>This will remove the cluster from management. Workloads on the cluster are not affected.</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmDeregister(null)} disabled={actionLoading}>Cancel</Button>
            <Button variant="destructive" onClick={handleDeregister} disabled={actionLoading}>
              {actionLoading ? 'Deregistering...' : 'Deregister'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Provision vCluster Dialog */}
      <Dialog open={showProvision} onOpenChange={setShowProvision}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Provision Virtual Cluster</DialogTitle>
            <DialogDescription>Create a new vCluster on a host cluster.</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="vc-name">Name</Label>
              <Input id="vc-name" value={vclusterForm.name} onChange={(e) => setVclusterForm({ ...vclusterForm, name: e.target.value })} placeholder="tenant-dev" />
            </div>
            <div className="space-y-2">
              <Label htmlFor="vc-namespace">Namespace</Label>
              <Input id="vc-namespace" value={vclusterForm.namespace} onChange={(e) => setVclusterForm({ ...vclusterForm, namespace: e.target.value })} placeholder="vc-tenant-dev" />
            </div>
            <div className="space-y-2">
              <Label>Host Cluster</Label>
              <Select value={vclusterForm.host_cluster_id} onValueChange={(v) => setVclusterForm({ ...vclusterForm, host_cluster_id: v })}>
                <SelectTrigger><SelectValue placeholder="Select host cluster" /></SelectTrigger>
                <SelectContent>
                  {clusters.map((c) => (
                    <SelectItem key={c.id} value={c.id}>{c.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowProvision(false)} disabled={actionLoading}>Cancel</Button>
            <Button onClick={handleProvision} disabled={actionLoading || !vclusterForm.name || !vclusterForm.namespace || !vclusterForm.host_cluster_id}>
              {actionLoading ? 'Provisioning...' : 'Provision'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Teardown Confirm Dialog */}
      <Dialog open={!!confirmTeardown} onOpenChange={() => setConfirmTeardown(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Teardown vCluster — {confirmTeardown?.name}</DialogTitle>
            <DialogDescription>This will destroy the virtual cluster and all its resources. This action cannot be undone.</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmTeardown(null)} disabled={actionLoading}>Cancel</Button>
            <Button variant="destructive" onClick={handleTeardown} disabled={actionLoading}>
              {actionLoading ? 'Tearing down...' : 'Teardown'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
