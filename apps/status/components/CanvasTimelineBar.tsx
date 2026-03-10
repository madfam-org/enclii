'use client'

import { useCallback, useEffect, useRef } from 'react'
import type { ServiceStatus, TimelineSlot, ServiceTimeline } from '@/lib/types'
import { STATUS_LABELS } from '@/lib/status-config'
import { cn } from '@/lib/utils'
import type { TooltipAnchor } from './SharedTooltip'

// ---- Color resolution from CSS custom properties ----

type StatusColorMap = Record<ServiceStatus, string>

const CSS_VAR_MAP: Record<ServiceStatus, string> = {
  operational: '--status-operational',
  degraded: '--status-degraded',
  outage: '--status-outage',
  maintenance: '--status-maintenance',
  unknown: '--muted',
}

function resolveCanvasColors(el: HTMLElement): StatusColorMap {
  const style = getComputedStyle(el)
  const colors = {} as StatusColorMap
  for (const status of Object.keys(CSS_VAR_MAP) as ServiceStatus[]) {
    const hslValue = style.getPropertyValue(CSS_VAR_MAP[status]).trim()
    colors[status] = hslValue ? `hsl(${hslValue})` : '#888'
  }
  return colors
}

function lightenHsl(hslString: string, amount: number): string {
  // Parse "hsl(H S% L%)" → adjust L
  const match = hslString.match(/hsl\(([^)]+)\)/)
  if (!match) return hslString
  const parts = match[1].split(/[\s,]+/)
  if (parts.length < 3) return hslString
  const h = parts[0]
  const s = parts[1]
  const l = parseFloat(parts[2])
  const newL = Math.min(100, l + amount)
  return `hsl(${h} ${s} ${newL}%)`
}

// ---- Component ----

interface CanvasTimelineBarProps {
  timeline: ServiceTimeline
  onSlotHover: (slot: TimelineSlot, anchor: TooltipAnchor) => void
  onSlotLeave: () => void
}

export function CanvasTimelineBar({ timeline, onSlotHover, onSlotLeave }: CanvasTimelineBarProps) {
  const { service, slots, uptime24h } = timeline
  const containerRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const colorsRef = useRef<StatusColorMap | null>(null)
  const hoveredIndexRef = useRef<number>(-1)
  const widthRef = useRef<number>(0)

  const BAR_HEIGHT = 24

  // Resolve colors from CSS (theme-aware)
  const ensureColors = useCallback(() => {
    if (!containerRef.current) return
    colorsRef.current = resolveCanvasColors(containerRef.current)
  }, [])

  // Draw the timeline onto the canvas
  const drawTimeline = useCallback(() => {
    const canvas = canvasRef.current
    if (!canvas || slots.length === 0) return

    const colors = colorsRef.current
    if (!colors) return

    const dpr = window.devicePixelRatio || 1
    const displayWidth = widthRef.current
    if (displayWidth <= 0) return

    // Size the canvas for crisp rendering
    canvas.width = Math.round(displayWidth * dpr)
    canvas.height = Math.round(BAR_HEIGHT * dpr)
    canvas.style.width = `${displayWidth}px`
    canvas.style.height = `${BAR_HEIGHT}px`

    const ctx = canvas.getContext('2d')
    if (!ctx) return

    ctx.scale(dpr, dpr)

    const slotWidth = displayWidth / slots.length
    const hoveredIdx = hoveredIndexRef.current

    for (let i = 0; i < slots.length; i++) {
      const slot = slots[i]
      const x = i * slotWidth
      // Use ceil for width to avoid sub-pixel gaps
      const w = Math.ceil((i + 1) * slotWidth) - Math.floor(x)

      if (i === hoveredIdx) {
        ctx.fillStyle = lightenHsl(colors[slot.status], 15)
      } else {
        ctx.fillStyle = colors[slot.status]
      }
      ctx.fillRect(Math.floor(x), 0, w, BAR_HEIGHT)
    }
  }, [slots])

  // ResizeObserver to track container width
  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    ensureColors()

    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const width = entry.contentRect.width
        if (Math.abs(width - widthRef.current) > 0.5) {
          widthRef.current = width
          drawTimeline()
        }
      }
    })
    observer.observe(container)

    return () => observer.disconnect()
  }, [ensureColors, drawTimeline])

  // Theme change: re-resolve colors and redraw
  useEffect(() => {
    const html = document.documentElement
    const observer = new MutationObserver(() => {
      ensureColors()
      drawTimeline()
    })
    observer.observe(html, { attributes: true, attributeFilter: ['data-theme', 'class'] })
    return () => observer.disconnect()
  }, [ensureColors, drawTimeline])

  // Redraw when slots data changes (e.g., auto-refresh)
  useEffect(() => {
    ensureColors()
    drawTimeline()
  }, [slots, ensureColors, drawTimeline])

  // ---- Hover handling ----

  function handleMouseMove(e: React.MouseEvent<HTMLCanvasElement>) {
    const canvas = canvasRef.current
    if (!canvas || slots.length === 0) return

    const rect = canvas.getBoundingClientRect()
    const x = e.clientX - rect.left
    const slotWidth = rect.width / slots.length
    const index = Math.min(Math.floor(x / slotWidth), slots.length - 1)

    if (index >= 0 && index < slots.length) {
      if (hoveredIndexRef.current !== index) {
        hoveredIndexRef.current = index
        drawTimeline()
      }
      onSlotHover(slots[index], { x: e.clientX, y: rect.top })
    }
  }

  function handleMouseLeave() {
    if (hoveredIndexRef.current !== -1) {
      hoveredIndexRef.current = -1
      drawTimeline()
    }
    onSlotLeave()
  }

  // ---- Accessibility ----

  const operationalCount = slots.filter(s => s.status === 'operational').length
  const degradedCount = slots.filter(s => s.status === 'degraded').length
  const outageCount = slots.filter(s => s.status === 'outage').length
  const maintenanceCount = slots.filter(s => s.status === 'maintenance').length

  const summaryParts: string[] = []
  if (operationalCount > 0) summaryParts.push(`${operationalCount} operational`)
  if (degradedCount > 0) summaryParts.push(`${degradedCount} degraded`)
  if (outageCount > 0) summaryParts.push(`${outageCount} outage`)
  if (maintenanceCount > 0) summaryParts.push(`${maintenanceCount} maintenance`)

  const ariaLabel = `${service}: ${uptime24h.toFixed(2)}% uptime over 24 hours`
  const srDetail = `${service}: ${uptime24h.toFixed(2)}% uptime. ${summaryParts.join(', ')} windows.`

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between">
        <a
          href={timeline.href || timeline.url}
          target="_blank"
          rel="noopener noreferrer"
          className="text-sm font-medium truncate hover:underline hover:text-primary transition-colors"
        >
          {service}
        </a>
        <span
          className={cn(
            'text-xs font-mono',
            uptime24h >= 99.9
              ? 'text-status-operational'
              : uptime24h >= 99
                ? 'text-status-degraded'
                : 'text-status-outage',
          )}
        >
          {uptime24h.toFixed(2)}%
        </span>
      </div>
      <div ref={containerRef} className="relative">
        <canvas
          ref={canvasRef}
          className="w-full rounded-[2px] cursor-crosshair"
          style={{ height: `${BAR_HEIGHT}px` }}
          role="img"
          aria-label={ariaLabel}
          onMouseMove={handleMouseMove}
          onMouseLeave={handleMouseLeave}
        />
        <span className="sr-only">{srDetail}</span>
      </div>
    </div>
  )
}
