/**
 * POST /api/argocd/applications/[name]/sync
 *
 * Requests an audited, non-pruning sync through the Switchyard ops adapter.
 * Switchyard resolves the caller's platform-admin rank before cluster access.
 *
 * Authorization: superadmin only. Middleware ensures the caller is an
 * authorized operator; we re-verify the JWT here to enforce the stricter
 * superadmin requirement.
 */
import { NextRequest, NextResponse } from 'next/server'
import { switchyardOpsCall } from '@/lib/switchyard-proxy'
import { hasRole, verifyAuth } from '@/lib/api-auth'

export const dynamic = 'force-dynamic'

const ARGO_NAMESPACE = process.env.ARGOCD_NAMESPACE || 'argocd'

export async function POST(
  request: NextRequest,
  { params }: { params: Promise<{ name: string }> }
) {
  const claims = await verifyAuth(request)
  if (!hasRole(claims, 'superadmin')) {
    return NextResponse.json(
      { error: 'Forbidden: superadmin role required to trigger ArgoCD syncs' },
      { status: 403 }
    )
  }

  const { name } = await params
  if (!name || !/^[a-zA-Z0-9-]+$/.test(name)) {
    return NextResponse.json({ error: 'Invalid application name' }, { status: 400 })
  }

  try {
    const token = request.cookies.get('dispatch_auth')?.value || request.cookies.get('admin_auth')?.value
    const { ok, data, status } = await switchyardOpsCall('apps', 'sync', {
      dry_run: false,
      reason: `Operator requested ArgoCD sync for ${name} from Dispatch`,
      scope: { namespace: ARGO_NAMESPACE },
      // The old direct patch never requested pruning. The adapter defaults to
      // true, so explicitly retain the existing non-destructive behavior.
      args: { target: name, prune: 'false' },
    }, token)
    if (!ok) {
      return NextResponse.json(
        { error: data.message || data.error || data.summary || 'Failed to trigger sync' },
        { status: status >= 400 ? status : 502 }
      )
    }

    return NextResponse.json({
      status: 'sync_triggered',
      name,
      triggered_at: new Date().toISOString(),
    })
  } catch (error) {
    console.error(`[Dispatch] argocd sync failed for ${name}:`, error)
    return NextResponse.json(
      { error: error instanceof Error ? error.message : 'Failed to trigger sync' },
      { status: 500 }
    )
  }
}
