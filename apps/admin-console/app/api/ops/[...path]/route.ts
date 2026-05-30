import { switchyardProxy } from '@/lib/switchyard-proxy'
import { NextRequest, NextResponse } from 'next/server'

async function handler(
  request: NextRequest,
  { params }: { params: Promise<{ path: string[] }> }
) {
  const { path } = await params
  const opsPath = '/' + path.join('/')
  const url = new URL(request.url)
  const qs = url.search

  const body = ['POST', 'PUT', 'PATCH'].includes(request.method)
    ? await request.text()
    : undefined

  const token = request.cookies.get('dispatch_auth')?.value
  const res = await switchyardProxy('ops', `${opsPath}${qs}`, {
    method: request.method,
    body,
    userToken: token,
  })

  const data = await res.json().catch(() => ({}))
  return NextResponse.json(data, { status: res.status })
}

export const GET = handler
export const POST = handler
export const PUT = handler
export const DELETE = handler
