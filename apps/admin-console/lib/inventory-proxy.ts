import { cookies } from 'next/headers'
import { NextResponse } from 'next/server'
import { callerProxy } from './caller-proxy'

/** Switchyard owns authorization and cluster credentials. No service-token fallback. */
export async function inventoryResponse(action: 'topology') {
  try {
    const store = await cookies()
    const token = store.get('dispatch_auth')?.value || store.get('admin_auth')?.value
    const response = await callerProxy(`/ops/inventory/${action}`, {
      method: 'POST',
      body: JSON.stringify({ dry_run: true }),
      userToken: token,
      signal: AbortSignal.timeout(25_000),
    })
    if (!response.ok) {
      return NextResponse.json(
        { error: response.status === 401 ? 'Authentication required' : response.status === 403 ? 'Platform operator access required' : 'Cluster inventory is unavailable' },
        { status: response.status >= 400 ? response.status : 502 }
      )
    }
    const envelope = await response.json()
    if (envelope.status !== 'succeeded' || !validTopology(envelope.data)) {
      return NextResponse.json(
        { error: 'Cluster inventory is unavailable' },
        { status: envelope.status === 'adapter_unconfigured' ? 503 : 502 }
      )
    }
    return NextResponse.json(envelope.data, { headers: { 'Cache-Control': 'no-store' } })
  } catch {
    return NextResponse.json({ error: 'Switchyard inventory is unreachable' }, { status: 502 })
  }
}

function validTopology(data: unknown): boolean {
  if (!data || typeof data !== 'object') return false
  const snapshot = data as Record<string, unknown>
  return ['nodes', 'namespaces', 'pods', 'services'].every((key) => Array.isArray(snapshot[key]))
    && typeof snapshot.synced_at === 'string'
    && !!snapshot.totals && typeof snapshot.totals === 'object'
    && !!snapshot.display && typeof snapshot.display === 'object'
}
