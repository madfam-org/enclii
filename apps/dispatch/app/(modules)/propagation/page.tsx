'use client'

import { PolicyList } from '@/components/propagation/policy-list'

export default function PropagationPage() {
  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-2xl font-semibold">Propagation Policies</h2>
      </div>
      <PolicyList />
    </div>
  )
}
