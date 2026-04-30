/**
 * ArgoCD Applications page.
 *
 * Server component reads the user's role from the dispatch_auth cookie via
 * the JWT verification helper, then passes a `canSync` flag to the client
 * component. Sync Now buttons only render for superadmin (the API also
 * enforces this — UI is just defense in depth).
 */
import { cookies } from 'next/headers'
import { jwtVerify, createRemoteJWKSet } from 'jose'
import { extractRoles } from '@/lib/auth-helpers'
import { ArgoCDApplicationsTable } from '@/components/argocd/applications-table'

const JANUA_ISSUER =
  process.env.JANUA_ISSUER || process.env.NEXT_PUBLIC_JANUA_URL || 'https://auth.madfam.io'
const jwks = createRemoteJWKSet(new URL('/.well-known/jwks.json', JANUA_ISSUER))

async function isSuperadmin(): Promise<boolean> {
  const store = await cookies()
  const token = store.get('dispatch_auth')?.value || store.get('admin_auth')?.value
  if (!token) return false
  try {
    const { payload } = await jwtVerify(token, jwks, { issuer: JANUA_ISSUER })
    return extractRoles(payload as Record<string, unknown>).includes('superadmin')
  } catch {
    return false
  }
}

export default async function ArgoCDPage() {
  const canSync = await isSuperadmin()
  return (
    <div>
      <h2 className="text-2xl font-semibold mb-2">ArgoCD Applications</h2>
      <p className="text-muted-foreground text-sm mb-6">
        Live state of every Application managed by ArgoCD in the cluster.
        {!canSync && ' Sync actions require the superadmin role.'}
      </p>
      <ArgoCDApplicationsTable canSync={canSync} />
    </div>
  )
}
