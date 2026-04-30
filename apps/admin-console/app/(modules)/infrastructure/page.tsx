'use client'

import { ResourceTable } from '@/components/infrastructure/resource-table'

export default function InfrastructurePage() {
  return (
    <div>
      <h2 className="text-2xl font-semibold mb-6">Infrastructure Composition</h2>
      <ResourceTable />
    </div>
  )
}
