'use client'

import type { BareMetalHost } from '@/types/admin'
import { Badge } from '@/components/ui/badge'
import { Server, Cpu, HardDrive, Wifi } from 'lucide-react'

interface Props {
  host: BareMetalHost
}

export function HostDetailPanel({ host }: Props) {
  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <Server className="size-8 text-primary" />
        <div>
          <h3 className="text-xl font-semibold">{host.name}</h3>
          <p className="text-sm text-muted-foreground">{host.bmc_address}</p>
        </div>
        <Badge variant="outline" className="ml-auto">{host.state}</Badge>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <InfoCard icon={Cpu} label="Boot Mode" value={host.boot_mode} />
        <InfoCard icon={HardDrive} label="Firmware" value={host.firmware_version || 'Unknown'} />
        <InfoCard icon={Wifi} label="MAC Address" value={host.mac_address || 'N/A'} />
        <InfoCard icon={Server} label="Power" value={host.power_state} />
      </div>

      {host.hardware_profile && Object.keys(host.hardware_profile).length > 0 && (
        <div>
          <h4 className="text-sm font-semibold mb-2">Hardware Profile</h4>
          <pre className="text-xs bg-muted/50 rounded-md p-3 overflow-auto">
            {JSON.stringify(host.hardware_profile, null, 2)}
          </pre>
        </div>
      )}
    </div>
  )
}

function InfoCard({ icon: Icon, label, value }: { icon: React.ComponentType<{ className?: string }>; label: string; value: string }) {
  return (
    <div className="rounded-md border border-border bg-card/50 p-3">
      <div className="flex items-center gap-2 text-muted-foreground mb-1">
        <Icon className="size-4" />
        <span className="text-xs">{label}</span>
      </div>
      <p className="font-mono text-sm">{value}</p>
    </div>
  )
}
