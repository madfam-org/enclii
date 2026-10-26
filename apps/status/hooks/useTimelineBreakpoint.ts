'use client'

import { useSyncExternalStore } from 'react'

interface TimelineBreakpoint {
  windowMinutes: number
  gapClass: string
}

const TIERS: { query: string; windowMinutes: number; gapClass: string }[] = [
  { query: '(min-width: 1280px)', windowMinutes: 5, gapClass: 'gap-0' },
  { query: '(min-width: 768px)', windowMinutes: 15, gapClass: 'gap-0' },
  { query: '(min-width: 480px)', windowMinutes: 30, gapClass: 'gap-[0.5px]' },
]

const FALLBACK: TimelineBreakpoint = { windowMinutes: 60, gapClass: 'gap-[0.5px]' }

function resolve(): TimelineBreakpoint {
  if (typeof window === 'undefined') return FALLBACK
  for (const tier of TIERS) {
    if (window.matchMedia(tier.query).matches) {
      return tier // Stable snapshot identity until a breakpoint actually changes.
    }
  }
  return FALLBACK
}

/**
 * Returns adaptive timeline window size based on viewport width.
 * Uses matchMedia listeners — fires only on threshold crossings (no debounce needed).
 */
function subscribe(notify: () => void) {
  const mediaLists = TIERS.map(tier => window.matchMedia(tier.query))
  for (const mql of mediaLists) mql.addEventListener('change', notify)
  return () => {
    for (const mql of mediaLists) mql.removeEventListener('change', notify)
  }
}

const serverSnapshot = () => FALLBACK

export function useTimelineBreakpoint(): TimelineBreakpoint {
  return useSyncExternalStore(subscribe, resolve, serverSnapshot)
}
