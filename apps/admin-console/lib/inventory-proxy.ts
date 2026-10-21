import { cookies } from 'next/headers'
import { NextResponse } from 'next/server'
import { switchyardOpsCall } from './switchyard-proxy'

type InventoryAction = 'topology' | 'applications' | 'volumes' | 'network-policies'

/** Switchyard owns authorization, cluster credentials, and inventory projection. */
export async function inventoryResponse(action: InventoryAction, namespace?: string) {
  try {
    const store = await cookies()
    const token = store.get('dispatch_auth')?.value || store.get('admin_auth')?.value
    const { ok, status, data } = await switchyardOpsCall('inventory', action, {
      dry_run: true,
      ...(namespace ? { scope: { namespace } } : {}),
    }, token)
    if (!ok) {
      return NextResponse.json(
        { error: status === 401 ? 'Authentication required' : status === 403 ? 'Platform operator access required' : 'Cluster inventory is unavailable' },
        { status: status >= 400 ? status : 502 }
      )
    }
    // Operator adapters can return an HTTP 200 envelope reporting an unavailable
    // adapter. Never turn that into an apparently healthy, empty inventory.
    if (data.status !== 'succeeded' || !data.data || typeof data.data !== 'object') {
      return NextResponse.json(
        { error: 'Cluster inventory is unavailable' },
        { status: data.status === 'adapter_unconfigured' ? 503 : 502 }
      )
    }
    return NextResponse.json(data.data)
  } catch {
    return NextResponse.json({ error: 'Switchyard inventory is unreachable' }, { status: 502 })
  }
}
