'use client'

import { useEffect, useState } from 'react'
import { fleetApi } from '@/lib/admin-api'
import type { BareMetalHost, BMHState } from '@/types/admin'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Server, Power, HardDrive, Trash2, RefreshCw } from 'lucide-react'

const stateColors: Record<BMHState, string> = {
  discovered: 'bg-blue-500/20 border-blue-500/40 text-blue-400',
  inspecting: 'bg-amber-500/20 border-amber-500/40 text-amber-400',
  available: 'bg-emerald-500/20 border-emerald-500/40 text-emerald-400',
  provisioning: 'bg-cyan-500/20 border-cyan-500/40 text-cyan-400',
  provisioned: 'bg-green-500/20 border-green-500/40 text-green-400',
  deprovisioning: 'bg-orange-500/20 border-orange-500/40 text-orange-400',
  error: 'bg-red-500/20 border-red-500/40 text-red-400',
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

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  if (error) {
    // Show clean empty state for auth errors instead of red error banner
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
          >
            <div className="flex items-center justify-between mb-2">
              <Server className="size-5" />
              <span className={`size-2.5 rounded-full ${host.power_state === 'on' ? 'bg-green-400' : host.power_state === 'off' ? 'bg-red-400' : 'bg-gray-400'}`} />
            </div>
            <h4 className="font-mono text-sm font-semibold truncate">{host.name}</h4>
            <p className="text-xs opacity-75 mt-1">{host.state}</p>
            {host.mac_address && (
              <p className="text-xs opacity-50 font-mono mt-1">{host.mac_address}</p>
            )}
            <div className="flex items-center gap-1 mt-3">
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-7 p-0"
                title={host.power_state === 'on' ? 'Power Off' : 'Power On'}
                onClick={() => setConfirmAction({ host, action: 'power' })}
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
                onClick={() => setConfirmAction({ host, action: 'wipe' })}
              >
                <Trash2 className="size-3.5" />
              </Button>
            </div>
          </div>
        ))}
      </div>

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
