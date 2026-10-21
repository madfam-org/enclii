import { inventoryResponse } from '@/lib/inventory-proxy'

export const dynamic = 'force-dynamic'

export interface NetworkPolicySummary {
  name: string
  namespace: string
  pod_selector: Record<string, string> | null
  pod_selector_summary: string
  policy_types: string[]
  ingress_rules: number
  egress_rules: number
  ingress_summary: string[]
  egress_summary: string[]
  created_at: string | null
}

export async function GET() {
  return inventoryResponse('network-policies')
}
