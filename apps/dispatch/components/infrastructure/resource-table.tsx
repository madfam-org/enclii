'use client'

import { useEffect, useState } from 'react'
import { resourceApi } from '@/lib/admin-api'
import type { ManagedResource } from '@/types/admin'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

const syncColors: Record<string, string> = {
  Synced: 'bg-green-500/20 text-green-400',
  OutOfSync: 'bg-red-500/20 text-red-400',
  Unknown: 'bg-gray-500/20 text-gray-400',
  Error: 'bg-red-500/20 text-red-400',
}

export function ResourceTable() {
  const [resources, setResources] = useState<ManagedResource[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    resourceApi.list().then((d) => setResources(d.resources || [])).finally(() => setLoading(false))
  }, [])

  if (loading) {
    return <div className="flex justify-center py-12"><div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin" /></div>
  }

  if (resources.length === 0) {
    return <p className="text-muted-foreground text-sm text-center py-8">No managed resources found.</p>
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
              <TableCell><Badge variant="outline">{r.management_policy}</Badge></TableCell>
              <TableCell><Badge className={syncColors[r.sync_status]}>{r.sync_status}</Badge></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
