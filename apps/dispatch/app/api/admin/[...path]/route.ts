import { adminProxy } from '@/lib/admin-proxy'
import { NextRequest, NextResponse } from 'next/server'

async function handler(
  request: NextRequest,
  { params }: { params: Promise<{ path: string[] }> }
) {
  const { path } = await params
  const adminPath = '/' + path.join('/')
  const url = new URL(request.url)
  const qs = url.search

  const body = ['POST', 'PUT', 'PATCH'].includes(request.method)
    ? await request.text()
    : undefined

  const res = await adminProxy(`${adminPath}${qs}`, {
    method: request.method,
    body,
  })

  const data = await res.json().catch(() => ({}))
  return NextResponse.json(data, { status: res.status })
}

export const GET = handler
export const POST = handler
export const PUT = handler
export const DELETE = handler
