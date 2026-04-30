'use client'

import { ColumnDef } from '@tanstack/react-table'
import { Badge } from "@enclii/ui-components/badge"
import {
  Globe,
  Shield,
  Server,
  MoreHorizontal,
  Copy,
  ExternalLink,
  Settings,
  Trash2,
} from 'lucide-react'
import { Button } from "@enclii/ui-components/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@enclii/ui-components/dropdown-menu"
import { cn, copyToClipboard, formatRelativeTime } from '@/lib/utils'
import type { DispatchDomain, ZoneStatus, EcosystemTenant } from '@/types/cloudflare'

// =============================================================================
// STATUS BADGE VARIANTS
// =============================================================================

const zoneStatusConfig: Record<ZoneStatus, { label: string; variant: 'success' | 'warning' | 'error' | 'info' | 'neutral' }> = {
  active: { label: 'Active', variant: 'success' },
  pending: { label: 'Pending', variant: 'warning' },
  initializing: { label: 'Initializing', variant: 'info' },
  moved: { label: 'Moved', variant: 'neutral' },
  deleted: { label: 'Deleted', variant: 'error' },
  deactivated: { label: 'Deactivated', variant: 'neutral' },
}

const tenantColors: Record<EcosystemTenant, string> = {
  madfam: 'bg-purple-500/20 text-purple-400 border-purple-500/30',
  suluna: 'bg-blue-500/20 text-blue-400 border-blue-500/30',
  primavera: 'bg-amber-500/20 text-amber-400 border-amber-500/30',
  janua: 'bg-emerald-500/20 text-emerald-400 border-emerald-500/30',
  enclii: 'bg-cyan-500/20 text-cyan-400 border-cyan-500/30',
  other: 'bg-gray-500/20 text-gray-400 border-gray-500/30',
}

// =============================================================================
// TABLE COLUMNS
// =============================================================================

export const columns: ColumnDef<DispatchDomain>[] = [
  // Domain Name
  {
    accessorKey: 'domain',
    header: () => (
      <div className="flex items-center gap-2">
        <Globe className="size-4 text-primary" />
        <span>Domain</span>
      </div>
    ),
    cell: ({ row }) => {
      const domain = row.getValue('domain') as string
      return (
        <div className="flex items-center gap-2">
          <code className="font-mono text-sm text-primary">{domain}</code>
          <Button
            variant="ghost"
            size="icon"
            className="size-6 opacity-0 group-hover:opacity-100 transition-opacity"
            onClick={() => copyToClipboard(domain)}
          >
            <Copy className="size-3" />
          </Button>
        </div>
      )
    },
  },

  // Tenant
  {
    accessorKey: 'tenant',
    header: 'Tenant',
    cell: ({ row }) => {
      const tenant = row.getValue('tenant') as EcosystemTenant
      return (
        <Badge
          variant="outline"
          className={cn('font-mono text-xs uppercase', tenantColors[tenant])}
        >
          {tenant}
        </Badge>
      )
    },
    filterFn: (row, id, value) => {
      return value.includes(row.getValue(id))
    },
  },

  // Zone Status
  {
    accessorKey: 'status',
    header: 'Status',
    cell: ({ row }) => {
      const status = row.getValue('status') as ZoneStatus
      const config = zoneStatusConfig[status]
      return (
        <div className="flex items-center gap-2">
          <span
            className={cn(
              'size-2 rounded-full',
              config.variant === 'success' && 'bg-status-success shadow-glow-sm',
              config.variant === 'warning' && 'bg-status-warning',
              config.variant === 'error' && 'bg-status-error',
              config.variant === 'info' && 'bg-status-info animate-pulse',
              config.variant === 'neutral' && 'bg-status-neutral'
            )}
          />
          <span className="text-sm text-muted-foreground">{config.label}</span>
        </div>
      )
    },
    filterFn: (row, id, value) => {
      return value.includes(row.getValue(id))
    },
  },

  // SSL Status
  {
    accessorKey: 'sslStatus',
    header: () => (
      <div className="flex items-center gap-2">
        <Shield className="size-4" />
        <span>SSL</span>
      </div>
    ),
    cell: ({ row }) => {
      const status = row.getValue('sslStatus') as string
      return (
        <Badge
          variant="outline"
          className={cn(
            'text-xs',
            status === 'active' && 'bg-status-success-muted text-status-success-muted-foreground border-status-success/20',
            status === 'pending' && 'bg-status-warning-muted text-status-warning-muted-foreground border-status-warning/20',
            status === 'error' && 'bg-status-error-muted text-status-error-muted-foreground border-status-error/20',
            status === 'inactive' && 'bg-muted text-muted-foreground'
          )}
        >
          {status === 'active' ? 'Secured' : status}
        </Badge>
      )
    },
  },

  // Tunnel
  {
    accessorKey: 'tunnelName',
    header: () => (
      <div className="flex items-center gap-2">
        <Server className="size-4" />
        <span>Tunnel</span>
      </div>
    ),
    cell: ({ row }) => {
      const tunnelName = row.getValue('tunnelName') as string | undefined
      if (!tunnelName) {
        return <span className="text-muted-foreground text-sm">-</span>
      }
      return (
        <code className="font-mono text-xs text-muted-foreground">{tunnelName}</code>
      )
    },
  },

  // Activated At
  {
    accessorKey: 'activatedAt',
    header: 'Activated',
    cell: ({ row }) => {
      const activatedAt = row.getValue('activatedAt') as string | null
      if (!activatedAt) {
        return <span className="text-muted-foreground text-sm">Pending</span>
      }
      return (
        <span className="text-sm text-muted-foreground">
          {formatRelativeTime(activatedAt)}
        </span>
      )
    },
  },

  // Actions
  {
    id: 'actions',
    cell: ({ row }) => {
      const domain = row.original

      return (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" className="size-8 p-0">
              <span className="sr-only">Open menu</span>
              <MoreHorizontal className="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-48">
            <DropdownMenuLabel className="font-mono text-xs text-primary">
              {domain.domain}
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => copyToClipboard(domain.domain)}>
              <Copy className="mr-2 size-4" />
              Copy Domain
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => copyToClipboard(domain.nameservers.join('\n'))}>
              <Copy className="mr-2 size-4" />
              Copy Nameservers
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild>
              <a
                href={`https://dash.cloudflare.com/${domain.id}`}
                target="_blank"
                rel="noopener noreferrer"
              >
                <ExternalLink className="mr-2 size-4" />
                Open in Cloudflare
              </a>
            </DropdownMenuItem>
            <DropdownMenuItem>
              <Settings className="mr-2 size-4" />
              Manage DNS
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem className="text-destructive focus:text-destructive">
              <Trash2 className="mr-2 size-4" />
              Delete Zone
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )
    },
  },
]
