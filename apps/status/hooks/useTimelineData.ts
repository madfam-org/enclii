'use client'

import { useEffect, useState } from 'react'
import type { TimelineResponse } from '@/lib/types'

interface UseTimelineDataResult {
  data: TimelineResponse | null
  loading: boolean
  error: string | null
  isInitialLoad: boolean
}

/**
 * Fetches timeline data with 60s auto-refresh and visibility-based pause.
 * Keeps stale data visible during refetches (no flash to loading skeleton).
 */
export function useTimelineData(windowMinutes: number): UseTimelineDataResult {
  const [data, setData] = useState<TimelineResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let active = true
    let interval: ReturnType<typeof setInterval> | null = null

    function fetchTimeline() {
      return Promise.resolve()
        .then(() => fetch(`/api/status/timeline?hours=24&window=${windowMinutes}`))
        .then(res => {
          if (!res.ok) throw new Error(`HTTP ${res.status}`)
          return res.json() as Promise<TimelineResponse>
        })
        .then(json => {
          if (!active) return
          setData(json)
          setError(null)
        })
        .catch(err => {
          if (active) setError(err instanceof Error ? err.message : 'Failed to load timeline')
        })
        .finally(() => {
          if (active) setLoading(false)
        })
    }

    void fetchTimeline()

    function startPolling() {
      stopPolling()
      interval = setInterval(fetchTimeline, 60_000)
    }

    function stopPolling() {
      if (interval !== null) {
        clearInterval(interval)
        interval = null
      }
    }

    function handleVisibility() {
      if (document.visibilityState === 'visible') {
        // Refresh immediately on tab focus, then resume polling
        fetchTimeline()
        startPolling()
      } else {
        stopPolling()
      }
    }

    if (document.visibilityState === 'visible') startPolling()
    document.addEventListener('visibilitychange', handleVisibility)

    return () => {
      active = false // Ignore responses from an obsolete window or unmounted view.
      stopPolling()
      document.removeEventListener('visibilitychange', handleVisibility)
    }
  }, [windowMinutes])

  return { data, loading, error, isInitialLoad: loading }
}
