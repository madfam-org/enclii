/**
 * POST /api/argocd/applications/[name]/sync
 *
 * Triggers an ArgoCD sync for the named application by patching its
 * `operation.sync` field (equivalent to `kubectl patch application <name>
 * -n argocd --type merge -p '{"operation":{"sync":{}}}').
 *
 * Authorization: superadmin only. Middleware ensures the caller is an
 * authorized operator; we re-verify the JWT here to enforce the stricter
 * superadmin requirement.
 */
import { NextRequest, NextResponse } from 'next/server'
import { customObjectsApi, invalidateCache } from '@/lib/k8s-client'
import { hasRole, verifyAuth } from '@/lib/api-auth'

export const dynamic = 'force-dynamic'

const ARGO_GROUP = 'argoproj.io'
const ARGO_VERSION = 'v1alpha1'
const ARGO_NAMESPACE = process.env.ARGOCD_NAMESPACE || 'argocd'
const APPLICATIONS_PLURAL = 'applications'

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
    const api = customObjectsApi()
    // v0.22.x positional signature: patchNamespacedCustomObject(group, version,
    // namespace, plural, name, body, dryRun?, fieldManager?, force?, options?).
    await api.patchNamespacedCustomObject(
      ARGO_GROUP,
      ARGO_VERSION,
      ARGO_NAMESPACE,
      APPLICATIONS_PLURAL,
      name,
      { operation: { sync: {} } },
      undefined,
      undefined,
      undefined,
      { headers: { 'Content-Type': 'application/merge-patch+json' } }
    )

    invalidateCache('argocd:applications')

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
