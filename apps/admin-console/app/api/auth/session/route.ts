import { NextRequest, NextResponse } from 'next/server'

/**
 * API Route to set auth cookies server-side
 *
 * This ensures cookies are properly set via Set-Cookie headers before
 * any redirect happens, avoiding race conditions with client-side cookie setting.
 */
export async function POST(request: NextRequest) {
  try {
    const body = await request.json()
    const { token, email, roles } = body

    if (!token || !email) {
      return NextResponse.json(
        { error: 'Missing required fields' },
        { status: 400 }
      )
    }

    // Build cookie options
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

    // Set all auth cookies via Set-Cookie headers
    response.cookies.set('dispatch_auth', token, cookieOptions)
    response.cookies.set('dispatch_user_email', email, cookieOptions)
    response.cookies.set('dispatch_user_roles', roles || '', cookieOptions)

    return response
  } catch (error) {
    console.error('Session API error:', error)
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    )
  }
}

export async function DELETE(request: NextRequest) {
  // Clear cookies
  const isProduction = process.env.NODE_ENV === 'production'
  const domain = isProduction ? '.enclii.dev' : undefined

  const response = NextResponse.json({ success: true })

  const clearOptions = {
    path: '/',
    maxAge: 0,
    ...(domain && { domain }),
  }

  response.cookies.set('dispatch_auth', '', clearOptions)
  response.cookies.set('dispatch_user_email', '', clearOptions)
  response.cookies.set('dispatch_user_roles', '', clearOptions)

  return response
}
