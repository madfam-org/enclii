import { NextRequest, NextResponse } from 'next/server'
import type { IncidentStatus, IncidentSeverity } from '@/lib/types'
import {
  createIncident,
  getIncidents,
  updateIncident,
  deleteIncident,
  isDatabaseConfigured,
} from '@/lib/incidents'
import { ensureSchema } from '@/lib/status-history'

function isAuthorized(request: NextRequest): boolean {
  const authHeader = request.headers.get('authorization')
  const adminSecret = process.env.ADMIN_SECRET
  if (!adminSecret) return false
  return authHeader === `Bearer ${adminSecret}`
}

/**
 * Get all incidents
 *
 * Query params:
 * - status: Filter by status (investigating, identified, monitoring, resolved)
 * - limit: Maximum number of incidents to return (default: 50)
 * - offset: Number of incidents to skip (default: 0)
 */
export async function GET(request: NextRequest) {
  if (!isDatabaseConfigured()) {
    return NextResponse.json({ incidents: [], total: 0, limit: 50, offset: 0 })
  }

  const { searchParams } = new URL(request.url)
  const status = searchParams.get('status') as IncidentStatus | null
  const limit = parseInt(searchParams.get('limit') || '50', 10)
  const offset = parseInt(searchParams.get('offset') || '0', 10)

  try {
    await ensureSchema()
    const result = await getIncidents({
      status: status ?? undefined,
      limit,
      offset,
    })

    return NextResponse.json({
      incidents: result.incidents,
      total: result.total,
      limit,
      offset,
    })
  } catch (err) {
    console.error('Failed to fetch incidents:', err)
    return NextResponse.json(
      { error: 'Failed to fetch incidents' },
      { status: 500 },
    )
  }
}

/**
 * Create a new incident
 *
 * Body:
 * - title: Incident title (required)
 * - severity: minor | major | critical (required)
 * - affectedServices: Array of service names (required)
 * - message: Initial update message (optional)
 */
export async function POST(request: NextRequest) {
  if (!isAuthorized(request)) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }

  if (!isDatabaseConfigured()) {
    return NextResponse.json(
      { error: 'Database not configured' },
      { status: 503 },
    )
  }

  try {
    const body = await request.json()

    if (!body.title) {
      return NextResponse.json(
        { error: 'title is required' },
        { status: 400 },
      )
    }

    if (!body.severity || !['minor', 'major', 'critical'].includes(body.severity)) {
      return NextResponse.json(
        { error: 'severity must be one of: minor, major, critical' },
        { status: 400 },
      )
    }

    if (!body.affectedServices || !Array.isArray(body.affectedServices)) {
      return NextResponse.json(
        { error: 'affectedServices must be an array' },
        { status: 400 },
      )
    }

    await ensureSchema()
    const incident = await createIncident({
      title: body.title,
      severity: body.severity as IncidentSeverity,
      affectedServices: body.affectedServices,
      initialMessage: body.message,
    })

    return NextResponse.json(incident, { status: 201 })
  } catch (err) {
    console.error('Failed to create incident:', err)
    return NextResponse.json(
      { error: 'Failed to create incident' },
      { status: 500 },
    )
  }
}

/**
 * Update an incident
 *
 * Body:
 * - id: Incident ID (required)
 * - status: New status (optional)
 * - message: Update message (optional, creates a new update)
 */
export async function PATCH(request: NextRequest) {
  if (!isAuthorized(request)) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }

  if (!isDatabaseConfigured()) {
    return NextResponse.json(
      { error: 'Database not configured' },
      { status: 503 },
    )
  }

  try {
    const body = await request.json()

    if (!body.id) {
      return NextResponse.json(
        { error: 'id is required' },
        { status: 400 },
      )
    }

    if (body.status) {
      const validStatuses: IncidentStatus[] = ['investigating', 'identified', 'monitoring', 'resolved']
      if (!validStatuses.includes(body.status)) {
        return NextResponse.json(
          { error: 'Invalid status' },
          { status: 400 },
        )
      }
    }

    const incident = await updateIncident(body.id, {
      status: body.status,
      message: body.message,
    })

    if (!incident) {
      return NextResponse.json(
        { error: 'Incident not found' },
        { status: 404 },
      )
    }

    return NextResponse.json(incident)
  } catch (err) {
    console.error('Failed to update incident:', err)
    return NextResponse.json(
      { error: 'Failed to update incident' },
      { status: 500 },
    )
  }
}

/**
 * Delete an incident
 *
 * Query params:
 * - id: Incident ID to delete
 */
export async function DELETE(request: NextRequest) {
  if (!isAuthorized(request)) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }

  if (!isDatabaseConfigured()) {
    return NextResponse.json(
      { error: 'Database not configured' },
      { status: 503 },
    )
  }

  const { searchParams } = new URL(request.url)
  const id = searchParams.get('id')

  if (!id) {
    return NextResponse.json(
      { error: 'id is required' },
      { status: 400 },
    )
  }

  try {
    const deleted = await deleteIncident(id)

    if (!deleted) {
      return NextResponse.json(
        { error: 'Incident not found' },
        { status: 404 },
      )
    }

    return NextResponse.json({ success: true })
  } catch (err) {
    console.error('Failed to delete incident:', err)
    return NextResponse.json(
      { error: 'Failed to delete incident' },
      { status: 500 },
    )
  }
}
