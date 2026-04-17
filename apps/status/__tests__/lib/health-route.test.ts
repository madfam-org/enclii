/**
 * @jest-environment node
 *
 * Tests for /api/health. The route now pings the DB pool so that the
 * probe fails fast when the status page has lost its DB connection.
 *
 * Node env (not jsdom) so `next/server` can load — it imports Request/
 * Response globals that only exist in Node >= 18.
 */
jest.mock('@/lib/db', () => ({
  query: jest.fn(),
}))

import { GET } from '@/app/api/health/route'
import { query } from '@/lib/db'

const mockQuery = query as jest.MockedFunction<typeof query>

describe('GET /api/health', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('returns 200 and db.status=ok when SELECT 1 succeeds', async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [{ ok: 1 }],
      command: 'SELECT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    const response = await GET()
    expect(response.status).toBe(200)
    const body = await response.json()
    expect(body.status).toBe('healthy')
    expect(body.db.status).toBe('ok')
    expect(body.db.error).toBeUndefined()
  })

  it('returns 200 and db.status=unconfigured when the pool is not set', async () => {
    // `query()` returns null when getPool() returns null (no DATABASE_URL).
    mockQuery.mockResolvedValueOnce(null)

    const response = await GET()
    expect(response.status).toBe(200)
    const body = await response.json()
    expect(body.status).toBe('healthy')
    expect(body.db.status).toBe('unconfigured')
  })

  it('returns 503 and db.status=error when the query throws', async () => {
    mockQuery.mockRejectedValueOnce(new Error('connection refused'))

    const response = await GET()
    expect(response.status).toBe(503)
    const body = await response.json()
    expect(body.status).toBe('degraded')
    expect(body.db.status).toBe('error')
    expect(body.db.error).toBe('connection refused')
  })

  it('returns 503 when SELECT 1 returns an unexpected shape', async () => {
    // Unexpected result shape is treated as an error — probe fails so the
    // pool gets recycled rather than continuing to serve stale data.
    mockQuery.mockResolvedValueOnce({
      rows: [{ ok: 42 }],
      command: 'SELECT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    const response = await GET()
    expect(response.status).toBe(503)
    const body = await response.json()
    expect(body.db.status).toBe('error')
    expect(body.db.error).toBe('unexpected result shape')
  })
})
