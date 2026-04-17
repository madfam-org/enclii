import { NextResponse } from 'next/server'
import { query } from '@/lib/db'

/**
 * Health check endpoint for the status page itself.
 * Used by Kubernetes probes. Returns 503 if the backing DB is unreachable,
 * so the status page doesn't serve stale data from a disconnected pool.
 */
export async function GET() {
  let dbStatus: 'ok' | 'unconfigured' | 'error' = 'unconfigured'
  let dbError: string | undefined

  try {
    const res = await query<{ ok: number }>('SELECT 1 AS ok')
    if (res === null) {
      dbStatus = 'unconfigured'
    } else if (res.rows.length === 1 && res.rows[0].ok === 1) {
      dbStatus = 'ok'
    } else {
      dbStatus = 'error'
      dbError = 'unexpected result shape'
    }
  } catch (err) {
    dbStatus = 'error'
    dbError = err instanceof Error ? err.message : 'unknown error'
  }

  const healthy = dbStatus !== 'error'

  return NextResponse.json(
    {
      status: healthy ? 'healthy' : 'degraded',
      timestamp: new Date().toISOString(),
      service: 'enclii-status',
      version: process.env.npm_package_version || '0.1.0',
      db: { status: dbStatus, ...(dbError ? { error: dbError } : {}) },
    },
    { status: healthy ? 200 : 503 },
  )
}
