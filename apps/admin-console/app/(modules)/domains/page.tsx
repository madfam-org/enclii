'use client'

import { DomainMatrix } from '@/components/domain-matrix'

export default function DomainsPage() {
  return (
    <div>
      <h2 className="text-2xl font-semibold mb-6">Domain Management</h2>
      <DomainMatrix />
    </div>
  )
}
