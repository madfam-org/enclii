'use client'

import { useEffect, useState } from 'react'
import { resourceApi } from '@/lib/admin-api'
import type { ManagedResource, ManagementPolicy } from '@/types/admin'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Boxes } from 'lucide-react'
import { EmptyState } from '@/components/empty-state'

const syncColors: Record<string, string> = {
  Synced: 'bg-green-500/20 text-green-400',
  OutOfSync: 'bg-red-500/20 text-red-400',
  Unknown: 'bg-gray-500/20 text-gray-400',
  Error: 'bg-red-500/20 text-red-400',
}

const policies: ManagementPolicy[] = ['FullControl', 'ObserveOnly', 'OrphanOnDelete']

export function ResourceTable() {
  const [resources, setResources] = useState<ManagedResource[]>([])
  const [loading, setLoading] = useState(true)

  const fetchResources = async () => {
    setLoading(true)
    resourceApi.list().then((d) => setResources(d.resources || [])).finally(() => setLoading(false))
  }

  useEffect(() => {
    fetchResources()
  }, [])

  const handlePolicyChange = async (id: string, policy: ManagementPolicy) => {
    try {
      await resourceApi.updatePolicy(id, policy)
      await fetchResources()
    } catch {
      // error handled by adminFetch
    }
  }

  if (loading) {
    return <div className="flex justify-center py-12"><div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" /></div>
  }

  if (resources.length === 0) {
    return <EmptyState icon={Boxes} title="No Managed Resources" description="Infrastructure resources managed by Crossplane will appear here." />
  }

  return (
    <div className="rounded-lg border border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Provider</TableHead>
            <TableHead>Kind</TableHead>
            <TableHead>Policy</TableHead>
            <TableHead>Sync</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {resources.map((r) => (
            <TableRow key={r.id}>
              <TableCell className="font-mono text-sm">{r.name}</TableCell>
              <TableCell>{r.provider}</TableCell>
              <TableCell>{r.kind}</TableCell>
              <TableCell>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <button className="cursor-pointer">
                      <Badge variant="outline" className="hover:bg-accent transition-colors">
                        {r.management_policy}
                      </Badge>
                    </button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start">
                    {policies.map((p) => (
                      <DropdownMenuItem
                        key={p}
                        onClick={() => handlePolicyChange(r.id, p)}
                        className={p === r.management_policy ? 'font-semibold' : ''}
                      >
                        {p}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
              </TableCell>
              <TableCell><Badge className={syncColors[r.sync_status]}>{r.sync_status}</Badge></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
