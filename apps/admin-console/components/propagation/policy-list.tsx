'use client'

import { useEffect, useState } from 'react'
import { propagationApi, clusterApi } from '@/lib/admin-api'
import type { PropagationPolicy, Cluster, PlacementStrategy } from '@/types/admin'
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
import { Plus, Trash2, GitBranch } from 'lucide-react'

const strategyColors: Record<PlacementStrategy, string> = {
  Spread: 'bg-blue-500/20 text-blue-400',
  Binpack: 'bg-amber-500/20 text-amber-400',
  GPUAffinity: 'bg-purple-500/20 text-purple-400',
}

export function PolicyList() {
  const [policies, setPolicies] = useState<PropagationPolicy[]>([])
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [loading, setLoading] = useState(true)
  const [showCreate, setShowCreate] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState<PropagationPolicy | null>(null)
  const [actionLoading, setActionLoading] = useState(false)

  const [form, setForm] = useState({
    name: '',
    placement_strategy: 'Spread' as PlacementStrategy,
    cluster_ids: [] as string[],
    gpu_required: false,
    priority: 100,
  })

  const fetchAll = async () => {
    setLoading(true)
    await Promise.all([
      propagationApi.list().then((d) => setPolicies(d.policies || [])),
      clusterApi.list().then((d) => setClusters(d.clusters || [])),
    ]).finally(() => setLoading(false))
  }

  useEffect(() => {
    fetchAll()
  }, [])

  const handleCreate = async () => {
    setActionLoading(true)
    try {
      await propagationApi.create(form)
      setShowCreate(false)
      setForm({ name: '', placement_strategy: 'Spread', cluster_ids: [], gpu_required: false, priority: 100 })
      await fetchAll()
    } catch {
      // error handled by adminFetch
    } finally {
      setActionLoading(false)
    }
  }

  const handleDelete = async () => {
    if (!confirmDelete) return
    setActionLoading(true)
    try {
      await propagationApi.delete(confirmDelete.id)
      setConfirmDelete(null)
      await fetchAll()
    } catch {
      // error handled by adminFetch
    } finally {
      setActionLoading(false)
    }
  }

  const toggleCluster = (id: string) => {
    setForm((prev) => ({
      ...prev,
      cluster_ids: prev.cluster_ids.includes(id)
        ? prev.cluster_ids.filter((c) => c !== id)
        : [...prev.cluster_ids, id],
    }))
  }

  if (loading) {
    return <div className="flex justify-center py-12"><div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" /></div>
  }

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <Button size="sm" onClick={() => setShowCreate(true)}>
          <Plus className="size-4 mr-2" />
          Create Policy
        </Button>
      </div>

      {policies.length === 0 ? (
        <div className="rounded-lg border border-border bg-card/50 p-8 text-center">
          <GitBranch className="size-12 mx-auto mb-4 text-muted-foreground" />
          <h3 className="text-lg font-semibold mb-2">No Propagation Policies</h3>
          <p className="text-muted-foreground mb-4">Create a policy to control how resources propagate across clusters.</p>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {policies.map((p) => (
            <div key={p.id} className="rounded-lg border border-border bg-card/50 p-4">
              <div className="flex items-center justify-between mb-2">
                <h4 className="font-semibold">{p.name}</h4>
                <Badge className={strategyColors[p.placement_strategy]}>{p.placement_strategy}</Badge>
              </div>
              <p className="text-xs text-muted-foreground">
                {p.cluster_ids.length} cluster{p.cluster_ids.length !== 1 ? 's' : ''} • Priority {p.priority}
                {p.gpu_required && ' • GPU required'}
              </p>
              <div className="mt-3 flex justify-end">
                <Button variant="ghost" size="sm" onClick={() => setConfirmDelete(p)}>
                  <Trash2 className="size-3.5 mr-1" /> Delete
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Create Policy Dialog */}
      <Dialog open={showCreate} onOpenChange={setShowCreate}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create Propagation Policy</DialogTitle>
            <DialogDescription>Define how resources are placed across clusters.</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="policy-name">Name</Label>
              <Input id="policy-name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="spread-all" />
            </div>
            <div className="space-y-2">
              <Label>Placement Strategy</Label>
              <Select value={form.placement_strategy} onValueChange={(v) => setForm({ ...form, placement_strategy: v as PlacementStrategy })}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="Spread">Spread</SelectItem>
                  <SelectItem value="Binpack">Binpack</SelectItem>
                  <SelectItem value="GPUAffinity">GPU Affinity</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>Target Clusters</Label>
              <div className="flex flex-wrap gap-2">
                {clusters.map((c) => (
                  <button
                    key={c.id}
                    type="button"
                    onClick={() => toggleCluster(c.id)}
                    className={`px-3 py-1 rounded-md text-xs border transition-colors ${
                      form.cluster_ids.includes(c.id)
                        ? 'bg-primary/20 border-primary/40 text-primary'
                        : 'bg-card border-border text-muted-foreground hover:border-primary/30'
                    }`}
                  >
                    {c.name}
                  </button>
                ))}
                {clusters.length === 0 && <p className="text-xs text-muted-foreground">No clusters registered.</p>}
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="policy-priority">Priority</Label>
              <Input id="policy-priority" type="number" value={form.priority} onChange={(e) => setForm({ ...form, priority: parseInt(e.target.value) || 0 })} />
            </div>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={form.gpu_required} onChange={(e) => setForm({ ...form, gpu_required: e.target.checked })} className="rounded" />
              GPU required
            </label>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowCreate(false)} disabled={actionLoading}>Cancel</Button>
            <Button onClick={handleCreate} disabled={actionLoading || !form.name}>
              {actionLoading ? 'Creating...' : 'Create'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Confirm Dialog */}
      <Dialog open={!!confirmDelete} onOpenChange={() => setConfirmDelete(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Policy — {confirmDelete?.name}</DialogTitle>
            <DialogDescription>This will remove the propagation policy. Resources already propagated are not affected.</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmDelete(null)} disabled={actionLoading}>Cancel</Button>
            <Button variant="destructive" onClick={handleDelete} disabled={actionLoading}>
              {actionLoading ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
