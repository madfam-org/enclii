'use client'

import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import type { ServiceStatus, TimelineSlot } from '@/lib/types'
import { STATUS_COLORS, STATUS_LABELS } from '@/lib/status-config'
import { cn } from '@/lib/utils'
import { formatResponseTime } from '@/lib/utils'

export interface TooltipAnchor {
  x: number
  y: number
}

interface SharedTooltipProps {
  visible: boolean
  slot: TimelineSlot | null
  anchor: TooltipAnchor | null
}

function formatSlotTime(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

export function SharedTooltip({ visible, slot, anchor }: SharedTooltipProps) {
  const tooltipRef = useRef<HTMLDivElement>(null)
  const [mounted, setMounted] = useState(false)

  useEffect(() => {
    setMounted(true)
  }, [])

  if (!mounted || !slot || !anchor) return null

  // Clamp X so tooltip doesn't overflow viewport
  const tooltipWidth = 180 // approximate max width
  const halfWidth = tooltipWidth / 2
  const viewportWidth = window.innerWidth
  let clampedX = anchor.x
  if (clampedX - halfWidth < 8) clampedX = halfWidth + 8
  if (clampedX + halfWidth > viewportWidth - 8) clampedX = viewportWidth - halfWidth - 8

  return createPortal(
    <div
      ref={tooltipRef}
      className="pointer-events-none"
      style={{
        position: 'fixed',
        left: clampedX,
        top: anchor.y - 8,
        transform: 'translate(-50%, -100%)',
        zIndex: 50,
        opacity: visible ? 1 : 0,
        transition: 'opacity 100ms ease-out',
      }}
    >
      <div className="bg-card border border-border rounded-md px-2.5 py-1.5 text-xs shadow-lg whitespace-nowrap">
        <div className="font-medium">
          {formatSlotTime(slot.start)} &ndash; {formatSlotTime(slot.end)}
        </div>
        <div className="flex items-center gap-1.5 mt-0.5">
          <div className={cn('size-2 rounded-full', STATUS_COLORS[slot.status])} />
          <span className="text-muted-foreground">{STATUS_LABELS[slot.status]}</span>
        </div>
        {slot.checks > 0 && (
          <div className="text-muted-foreground mt-0.5">
            {slot.checks} check{slot.checks !== 1 ? 's' : ''}
            {slot.avgResponseMs !== null && ` \u00B7 ${formatResponseTime(slot.avgResponseMs)}`}
          </div>
        )}
      </div>
    </div>,
    document.body,
  )
}
