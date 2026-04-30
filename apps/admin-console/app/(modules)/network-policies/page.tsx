'use client'

import { PoliciesTree } from '@/components/network-policies/policies-tree'

export default function NetworkPoliciesPage() {
  return (
    <div>
      <h2 className="text-2xl font-semibold mb-2">Network Policies</h2>
      <p className="text-muted-foreground text-sm mb-6">
        Per-namespace ingress and egress rules currently enforced in the cluster. Read-only — to
        modify, update each repo's <code className="text-xs">enclii.yaml</code> network section.
      </p>
      <PoliciesTree />
    </div>
  )
}
