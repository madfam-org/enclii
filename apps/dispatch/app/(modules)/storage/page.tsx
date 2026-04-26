'use client'

import { VolumesTable } from '@/components/storage/volumes-table'

export default function StoragePage() {
  return (
    <div>
      <h2 className="text-2xl font-semibold mb-2">Storage Health</h2>
      <p className="text-muted-foreground text-sm mb-6">
        Live state of every Longhorn volume, sorted faulted-first.
      </p>
      <VolumesTable />
    </div>
  )
}
