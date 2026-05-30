'use client'

import Link from 'next/link'
import { DomainMatrix } from '@/components/domain-matrix'

export default function ProvidersCloudflarePage() {
  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        Cloudflare zones are loaded via Switchyard provider APIs.{' '}
        <Link href="/domains" className="text-primary hover:underline">
          Open full domain matrix
        </Link>
      </p>
      <DomainMatrix />
    </div>
  )
}
