import { NextRequest, NextResponse } from 'next/server'

/**
 * API Route to set auth cookies server-side
 *
 * Ensures cookies are properly set via Set-Cookie headers before
 * any redirect happens, avoiding race conditions with client-side cookie setting.
 */
export async function POST(request: NextRequest) {
  try {
    const body = await request.json()
    const { token, email } = body

    if (!token || !email) {
      return NextResponse.json(
        { error: 'Missing required fields' },
        { status: 400 }
      )
    }

    const isProduction = process.env.NODE_ENV === 'production'
    const domain = isProduction ? '.enclii.dev' : undefined

    const cookieOptions = {
      path: '/',
      maxAge: 86400, // 24 hours
      sameSite: 'lax' as const,
      secure: isProduction,
      httpOnly: false, // Needs to be readable by client for logout
      ...(domain && { domain }),
    }

    const response = NextResponse.json({ success: true })

    response.cookies.set('enclii_auth', token, cookieOptions)
    response.cookies.set('enclii_user_email', email, cookieOptions)

    return response
  } catch (error) {
    console.error('Session API error:', error)
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    )
  }
}

export async function DELETE() {
  const isProduction = process.env.NODE_ENV === 'production'
  const domain = isProduction ? '.enclii.dev' : undefined

  const response = NextResponse.json({ success: true })

  const clearOptions = {
    path: '/',
    maxAge: 0,
    ...(domain && { domain }),
  }

  response.cookies.set('enclii_auth', '', clearOptions)
  response.cookies.set('enclii_user_email', '', clearOptions)

  return response
}
