'use client'

import { useEffect, useState } from 'react'

interface TimelineBreakpoint {
  windowMinutes: number
  gapClass: string
}

const TIERS: { query: string; windowMinutes: number; gapClass: string }[] = [
  { query: '(min-width: 1024px)', windowMinutes: 5, gapClass: 'gap-0' },
  { query: '(min-width: 768px)', windowMinutes: 15, gapClass: 'gap-0' },
  { query: '(min-width: 640px)', windowMinutes: 30, gapClass: 'gap-[0.5px]' },
]

const FALLBACK: TimelineBreakpoint = { windowMinutes: 60, gapClass: 'gap-[0.5px]' }

function resolve(): TimelineBreakpoint {
  if (typeof window === 'undefined') return FALLBACK
  for (const tier of TIERS) {
    if (window.matchMedia(tier.query).matches) {
      return { windowMinutes: tier.windowMinutes, gapClass: tier.gapClass }
    }
  }
  return FALLBACK
}

/**
 * Returns adaptive timeline window size based on viewport width.
 * Uses matchMedia listeners — fires only on threshold crossings (no debounce needed).
 */
export function useTimelineBreakpoint(): TimelineBreakpoint {
  const [breakpoint, setBreakpoint] = useState<TimelineBreakpoint>(FALLBACK)

  useEffect(() => {
    setBreakpoint(resolve())

    const mediaLists = TIERS.map(tier => window.matchMedia(tier.query))
    const handler = () => setBreakpoint(resolve())

    for (const mql of mediaLists) {
      mql.addEventListener('change', handler)
    }

    return () => {
      for (const mql of mediaLists) {
        mql.removeEventListener('change', handler)
      }
    }
  }, [])

  return breakpoint
}
