'use client'

import { Badge } from '@enclii/ui-components/badge'
import { formatRelativeTime } from '@/lib/utils'
import type { ProviderCatalogEntry } from '@/types/providers'

type ProviderStatusCardProps = {
  entry: ProviderCatalogEntry
  checkedAt?: string
  onOpen?: () => void
}

function readinessLabel(readiness?: Record<string, unknown>): string {
  if (!readiness) return 'unknown'
  if (readiness.configured === true) return 'ready'
  if (readiness.apiKeyPresent === true || readiness.tokenPresent === true) return 'partial'
  return 'not configured'
}

export function ProviderStatusCard({ entry, checkedAt, onOpen }: ProviderStatusCardProps) {
  const readiness = entry.readiness as Record<string, unknown> | undefined
  const label = readinessLabel(readiness)
  const variant =
    label === 'ready' ? 'default' : label === 'partial' ? 'secondary' : 'outline'

  return (
    <button
      type="button"
      onClick={onOpen}
      className="rounded-lg border border-border bg-card/50 p-4 text-left hover:border-primary/40 transition-colors w-full"
    >
      <div className="flex items-center justify-between mb-2">
        <h3 className="text-base font-semibold capitalize">{entry.name}</h3>
        <Badge variant={variant}>{label}</Badge>
      </div>
      <p className="text-sm text-muted-foreground line-clamp-2 mb-3">{entry.description}</p>
      <div className="text-xs text-muted-foreground space-y-1">
        <p>Contract: {entry.status}</p>
        {checkedAt && <p>Checked {formatRelativeTime(checkedAt)}</p>}
        {entry.actions && <p>{entry.actions.length} actions</p>}
      </div>
    </button>
  )
}
