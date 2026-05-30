'use client'

import { useCallback, useEffect, useState } from 'react'
import { apiPost } from '@/lib/api'
import { useAuth } from '@/contexts/AuthContext'

/** Map apex domain → Resend verification status (admin-only provider read). */
export function useResendDomainStatus() {
  const { user } = useAuth()
  const isAdmin = !!user?.roles?.includes('admin')
  const [statusByDomain, setStatusByDomain] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(false)

  const refresh = useCallback(async () => {
    if (!isAdmin) return
    setLoading(true)
    try {
      const resp = await apiPost<{
        data?: { domains?: Array<{ name: string; status: string }> };
      }>('/v1/providers/resend/domains', { dry_run: true })
      const map: Record<string, string> = {}
      for (const d of resp.data?.domains ?? []) {
        map[d.name.toLowerCase()] = d.status
      }
      setStatusByDomain(map)
    } catch {
      setStatusByDomain({})
    } finally {
      setLoading(false)
    }
  }, [isAdmin])

  useEffect(() => {
    refresh()
  }, [refresh])

  return { statusByDomain, loading, refresh, isAdmin }
}
