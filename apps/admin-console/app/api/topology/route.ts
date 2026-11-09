import { inventoryResponse } from '@/lib/inventory-proxy'

export const dynamic = 'force-dynamic'

export async function GET() {
  return inventoryResponse('topology')
}
