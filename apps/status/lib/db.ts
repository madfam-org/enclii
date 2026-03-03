/**
 * Database connection singleton for the status page.
 * Uses the native `pg` driver with a connection pool.
 */

import { Pool, type QueryResult } from 'pg'
import { getDatabaseUrl } from './config'

let pool: Pool | null = null

/**
 * Get or create the shared connection pool.
 * Returns null if DATABASE_URL is not configured.
 */
export function getPool(): Pool | null {
  if (pool) return pool

  const url = getDatabaseUrl()
  if (!url) return null

  pool = new Pool({
    connectionString: url,
    max: 5,
    idleTimeoutMillis: 30_000,
    connectionTimeoutMillis: 5_000,
  })

  pool.on('error', (err) => {
    console.error('Unexpected pool error:', err)
  })

  return pool
}

/**
 * Execute a parameterized query. Returns null if DB is not configured.
 */
export async function query<T extends Record<string, unknown> = Record<string, unknown>>(
  text: string,
  params?: unknown[],
): Promise<QueryResult<T> | null> {
  const p = getPool()
  if (!p) return null
  return p.query<T>(text, params)
}
