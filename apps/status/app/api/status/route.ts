import { NextResponse } from 'next/server'
import { checkAllServices } from '@/lib/health-checker'
import { getAllServicesUptime, isPrometheusAvailable } from '@/lib/prometheus'
import { getSiteConfig, getResponseTimeThresholds } from '@/lib/config'
import { calculateOverallStatus } from '@/lib/types'
import type { StatusResponse } from '@/lib/types'

/**
 * Get aggregated status for all monitored services
 *
 * Response includes:
 * - overall: Overall system status
 * - lastUpdated: Timestamp of last check
 * - services: Array of service health check results
 * - uptimeData: Historical uptime data (if Prometheus available)
 */
export async function GET() {
  try {
    const config = getSiteConfig()

    // Fetch health check results
    const services = await checkAllServices(config.services)

    // Calculate overall status
    const overall = calculateOverallStatus(services)

    // Check if Prometheus is available and fetch uptime data
    const hasPrometheus = await isPrometheusAvailable()
    let uptimeData = undefined

    if (hasPrometheus) {
      uptimeData = await getAllServicesUptime(
        config.services.map(s => ({ name: s.name, url: s.url }))
      )
    }

    const response: StatusResponse = {
      overall,
      lastUpdated: new Date().toISOString(),
      services,
      uptimeData,
      responseTimeThresholds: getResponseTimeThresholds(),
    }

    return NextResponse.json(response, {
      headers: {
        'Cache-Control': 'public, s-maxage=30, stale-while-revalidate=60',
      },
    })
  } catch (error) {
    console.error('Status API error:', error)

    return NextResponse.json(
      {
        overall: 'unknown',
        lastUpdated: new Date().toISOString(),
        services: [],
        error: 'Failed to fetch status',
      },
      { status: 500 }
    )
  }
}

/**
 * Force refresh status (bypass cache)
 */
export async function POST() {
  try {
    const config = getSiteConfig()

    // Clear cache by importing and calling clearCache
    const { clearCache } = await import('@/lib/health-checker')
    clearCache()

    // Fetch fresh health check results
    const services = await checkAllServices(config.services)
    const overall = calculateOverallStatus(services)

    const response: StatusResponse = {
      overall,
      lastUpdated: new Date().toISOString(),
      services,
    }

    return NextResponse.json(response)
  } catch (error) {
    console.error('Status refresh error:', error)

    return NextResponse.json(
      {
        overall: 'unknown',
        lastUpdated: new Date().toISOString(),
        services: [],
        error: 'Failed to refresh status',
      },
      { status: 500 }
    )
  }
}
