'use client'

import { useEffect, useRef, useState, useCallback } from 'react'
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
  const isInitialLoad = useRef(true)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const fetchTimeline = useCallback(async () => {
    try {
      const res = await fetch(`/api/status/timeline?hours=24&window=${windowMinutes}`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const json: TimelineResponse = await res.json()
      setData(json)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load timeline')
    } finally {
      if (isInitialLoad.current) {
        isInitialLoad.current = false
        setLoading(false)
      }
    }
  }, [windowMinutes])

  useEffect(() => {
    fetchTimeline()

    function startPolling() {
      stopPolling()
      intervalRef.current = setInterval(fetchTimeline, 60_000)
    }

    function stopPolling() {
      if (intervalRef.current !== null) {
        clearInterval(intervalRef.current)
        intervalRef.current = null
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

    startPolling()
    document.addEventListener('visibilitychange', handleVisibility)

    return () => {
      stopPolling()
      document.removeEventListener('visibilitychange', handleVisibility)
    }
  }, [fetchTimeline])

  return { data, loading, error, isInitialLoad: isInitialLoad.current }
}
