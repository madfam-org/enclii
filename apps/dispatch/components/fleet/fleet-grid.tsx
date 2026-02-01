'use client'

import { useEffect, useState } from 'react'
import { fleetApi } from '@/lib/admin-api'
import type { BareMetalHost, BMHState } from '@/types/admin'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Server, Power, HardDrive, Trash2, RefreshCw, Clock, Cpu, DollarSign } from 'lucide-react'

const stateColors: Record<BMHState, string> = {
  discovered: 'bg-blue-500/20 border-blue-500/40 text-blue-400',
  inspecting: 'bg-amber-500/20 border-amber-500/40 text-amber-400',
  available: 'bg-emerald-500/20 border-emerald-500/40 text-emerald-400',
  provisioning: 'bg-cyan-500/20 border-cyan-500/40 text-cyan-400',
  provisioned: 'bg-green-500/20 border-green-500/40 text-green-400',
  deprovisioning: 'bg-orange-500/20 border-orange-500/40 text-orange-400',
  error: 'bg-red-500/20 border-red-500/40 text-red-400',
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

function hwSummary(hw: Record<string, unknown> | undefined): string {
  if (!hw) return ''
  const parts: string[] = []
  if (hw.os) parts.push(String(hw.os))
  if (hw.arch) parts.push(String(hw.arch))
  if (hw.cpu_cores) parts.push(`${hw.cpu_cores} cores`)
  const mem = hw.memory_gb ?? (hw.memory_bytes ? Math.round(Number(hw.memory_bytes) / (1024 ** 3)) : null)
  if (mem) parts.push(`${mem}GB`)
  return parts.join(' · ')
}

export function FleetGrid() {
  const [hosts, setHosts] = useState<BareMetalHost[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [confirmAction, setConfirmAction] = useState<{
    host: BareMetalHost
    action: 'power' | 'wipe'
  } | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [selectedHost, setSelectedHost] = useState<BareMetalHost | null>(null)
  const [editCost, setEditCost] = useState('')
  const [costSaving, setCostSaving] = useState(false)

  const fetchHosts = async () => {
    try {
      setLoading(true)
      const data = await fleetApi.list()
      setHosts(data.hosts || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch fleet')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchHosts()
  }, [])

  const handleConfirm = async () => {
    if (!confirmAction) return
    setActionLoading(true)
    try {
      if (confirmAction.action === 'power') {
        const nextState = confirmAction.host.power_state === 'on' ? 'off' : 'on'
        await fleetApi.power(confirmAction.host.id, nextState)
      } else {
        await fleetApi.wipe(confirmAction.host.id)
      }
      setConfirmAction(null)
      await fetchHosts()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Action failed')
    } finally {
      setActionLoading(false)
    }
  }

  const handleSaveCost = async () => {
    if (!selectedHost) return
    setCostSaving(true)
    try {
      await fleetApi.update(selectedHost.id, { cost_per_hour_cents: Number(editCost) })
      await fetchHosts()
      setSelectedHost((prev) => prev ? { ...prev, cost_per_hour_cents: Number(editCost) } : null)
    } catch {
      // error handled by adminFetch
    } finally {
      setCostSaving(false)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  if (error) {
    if (error.includes('401') || error.includes('Authorization')) {
      return (
        <div className="rounded-lg border border-border bg-card/50 p-8 text-center">
          <Server className="size-12 mx-auto mb-4 text-muted-foreground" />
          <h3 className="text-lg font-semibold mb-2">No Bare Metal Hosts</h3>
          <p className="text-muted-foreground mb-4">Register your first bare metal host to get started.</p>
        </div>
      )
    }
    return (
      <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-center">
        <p className="text-destructive">{error}</p>
        <Button variant="outline" size="sm" className="mt-2" onClick={fetchHosts}>
          <RefreshCw className="size-4 mr-2" />
          Retry
        </Button>
      </div>
    )
  }

  if (hosts.length === 0) {
    return (
      <div className="rounded-lg border border-border bg-card/50 p-8 text-center">
        <Server className="size-12 mx-auto mb-4 text-muted-foreground" />
        <h3 className="text-lg font-semibold mb-2">No Bare Metal Hosts</h3>
        <p className="text-muted-foreground mb-4">Register your first bare metal host to get started.</p>
      </div>
    )
  }

  return (
    <>
      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-3">
        {hosts.map((host) => (
          <div
            key={host.id}
            className={`rounded-lg border p-4 transition-all hover:shadow-md cursor-pointer ${stateColors[host.state] || 'bg-muted/20 border-border'}`}
            onClick={() => {
              setSelectedHost(host)
              setEditCost(String(host.cost_per_hour_cents || 0))
            }}
          >
            <div className="flex items-center justify-between mb-2">
              <Server className="size-5" />
              <span className={`size-2.5 rounded-full ${host.power_state === 'on' ? 'bg-green-400' : host.power_state === 'off' ? 'bg-red-400' : 'bg-gray-400'}`} />
            </div>
            <h4 className="font-mono text-sm font-semibold truncate">{host.name}</h4>
            <p className="text-xs opacity-75 mt-1">{host.state}</p>
            {hwSummary(host.hardware_profile) && (
              <p className="text-xs opacity-60 mt-1 flex items-center gap-1">
                <Cpu className="size-3 inline" />
                {hwSummary(host.hardware_profile)}
              </p>
            )}
            {host.mac_address && (
              <p className="text-xs opacity-50 font-mono mt-1">{host.mac_address}</p>
            )}
            {host.last_inspection_at && (
              <p className="text-xs opacity-50 mt-1 flex items-center gap-1">
                <Clock className="size-3 inline" />
                synced {relativeTime(host.last_inspection_at)}
              </p>
            )}
            <div className="flex items-center gap-1 mt-3">
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-7 p-0"
                title={host.power_state === 'on' ? 'Power Off' : 'Power On'}
                onClick={(e) => { e.stopPropagation(); setConfirmAction({ host, action: 'power' }) }}
              >
                <Power className="size-3.5" />
              </Button>
              <Button variant="ghost" size="sm" className="h-7 w-7 p-0" title="Firmware" disabled>
                <HardDrive className="size-3.5" />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-7 p-0"
                title="Wipe"
                onClick={(e) => { e.stopPropagation(); setConfirmAction({ host, action: 'wipe' }) }}
              >
                <Trash2 className="size-3.5" />
              </Button>
            </div>
          </div>
        ))}
      </div>

      {/* Host Detail Dialog */}
      <Dialog open={!!selectedHost} onOpenChange={() => setSelectedHost(null)}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle className="font-mono">{selectedHost?.name}</DialogTitle>
            <DialogDescription>
              {selectedHost?.state} · power {selectedHost?.power_state}
            </DialogDescription>
          </DialogHeader>
          {selectedHost && (
            <div className="space-y-4 text-sm">
              <div className="grid grid-cols-2 gap-x-4 gap-y-2">
                <div className="text-muted-foreground">MAC Address</div>
                <div className="font-mono">{selectedHost.mac_address || '—'}</div>
                <div className="text-muted-foreground">BMC Address</div>
                <div className="font-mono">{selectedHost.bmc_address || '—'}</div>
                <div className="text-muted-foreground">Firmware</div>
                <div className="font-mono">{selectedHost.firmware_version || '—'}</div>
                <div className="text-muted-foreground">Boot Mode</div>
                <div>{selectedHost.boot_mode || '—'}</div>
                <div className="text-muted-foreground">Last Sync</div>
                <div>{selectedHost.last_inspection_at ? relativeTime(selectedHost.last_inspection_at) : '—'}</div>
              </div>

              {selectedHost.hardware_profile && Object.keys(selectedHost.hardware_profile).length > 0 && (
                <div>
                  <h5 className="font-semibold mb-1">Hardware Profile</h5>
                  <pre className="text-xs bg-muted/30 rounded p-2 overflow-auto max-h-40">
                    {JSON.stringify(selectedHost.hardware_profile, null, 2)}
                  </pre>
                </div>
              )}

              <div className="flex items-end gap-2">
                <div className="flex-1 space-y-1">
                  <Label htmlFor="cost-input" className="flex items-center gap-1">
                    <DollarSign className="size-3" /> Cost (cents/hr)
                  </Label>
                  <Input
                    id="cost-input"
                    type="number"
                    min={0}
                    value={editCost}
                    onChange={(e) => setEditCost(e.target.value)}
                  />
                </div>
                <Button
                  size="sm"
                  onClick={handleSaveCost}
                  disabled={costSaving || Number(editCost) === selectedHost.cost_per_hour_cents}
                >
                  {costSaving ? 'Saving...' : 'Save'}
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>

      {/* Confirm Action Dialog */}
      <Dialog open={!!confirmAction} onOpenChange={() => setConfirmAction(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {confirmAction?.action === 'power'
                ? `Power ${confirmAction.host.power_state === 'on' ? 'Off' : 'On'} — ${confirmAction?.host.name}`
                : `Wipe — ${confirmAction?.host.name}`}
            </DialogTitle>
            <DialogDescription>
              {confirmAction?.action === 'power'
                ? `This will ${confirmAction.host.power_state === 'on' ? 'shut down' : 'start'} the host.`
                : 'This will erase all data on this host. This action cannot be undone.'}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmAction(null)} disabled={actionLoading}>
              Cancel
            </Button>
            <Button
              variant={confirmAction?.action === 'wipe' ? 'destructive' : 'default'}
              onClick={handleConfirm}
              disabled={actionLoading}
            >
              {actionLoading ? 'Processing...' : 'Confirm'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
