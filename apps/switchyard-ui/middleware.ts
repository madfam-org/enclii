import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

/**
 * Next.js Middleware for security headers and authentication routing
 *
 * SECURITY: This middleware runs on every request to add security headers
 * and handle authentication redirects
 */

// Public paths that don't require authentication
const publicPaths = [
  "/login",
  "/register",
  "/auth/callback",
  "/api/auth",
  "/api/health",  // Health check endpoint for K8s probes
  "/health",      // Alternative health endpoint
  "/_next",
  "/favicon.ico",
  "/public",
];

function isPublicPath(pathname: string): boolean {
  return publicPaths.some(
    (path) => pathname === path || pathname.startsWith(path + "/")
  );
}

export function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;

  // Skip middleware for public paths
  if (isPublicPath(pathname)) {
    return addSecurityHeaders(NextResponse.next());
  }

  // Check for authentication token
  // We check localStorage via cookie since middleware can't access localStorage
  // The token is also stored in a cookie by the AuthContext
  const token = request.cookies.get("enclii_auth")?.value;

  // For client-side auth, we'll let the AuthenticatedLayout handle redirects
  // But we can still add security headers
  const response = NextResponse.next();

  // If no token and trying to access protected route, redirect to login
  // Note: This provides server-side redirect for initial page loads
  // Client-side navigation is handled by AuthenticatedLayout
  if (!token && !pathname.startsWith("/login") && !pathname.startsWith("/auth")) {
    // For API routes or non-page requests, return 401
    if (pathname.startsWith("/api/")) {
      return new NextResponse(JSON.stringify({ error: "Unauthorized" }), {
        status: 401,
        headers: { "Content-Type": "application/json" },
      });
    }

    // For page requests, let client-side handle it for now
    // This allows the AuthProvider to initialize and check localStorage
    // If you want server-side redirects, you'd need to set an auth cookie
  }

  return addSecurityHeaders(response);
}

function addSecurityHeaders(response: NextResponse): NextResponse {
  // SECURITY FIX: Add comprehensive security headers
  const securityHeaders = {
    // Prevent clickjacking attacks
    "X-Frame-Options": "DENY",

    // Prevent MIME type sniffing
    "X-Content-Type-Options": "nosniff",

    // Enable XSS protection (legacy but still useful)
    "X-XSS-Protection": "1; mode=block",

    // Control referrer information
    "Referrer-Policy": "strict-origin-when-cross-origin",

    // Content Security Policy - restricts resource loading
    "Content-Security-Policy": [
      "default-src 'self'",
      "script-src 'self' 'unsafe-eval' 'unsafe-inline' https://static.cloudflareinsights.com", // Next.js requires unsafe-eval in dev
      "style-src 'self' 'unsafe-inline'", // Tailwind requires unsafe-inline
      "img-src 'self' data: https:",
      "font-src 'self' data:",
      // API endpoints: localhost for dev, api.enclii.dev for production
      // Janua SSO configured via NEXT_PUBLIC_JANUA_URL (default: api.janua.dev)
      `connect-src 'self' http://localhost:4200 https://api.enclii.dev ${process.env.NEXT_PUBLIC_JANUA_URL || 'https://api.janua.dev'} https://static.cloudflareinsights.com`,
      `frame-src 'self' ${process.env.NEXT_PUBLIC_JANUA_URL || 'https://auth.madfam.io'}`,
      "frame-ancestors 'none'",
    ].join("; "),

    // Permissions Policy - restrict browser features
    "Permissions-Policy": [
      "geolocation=()",
      "microphone=()",
      "camera=()",
      "payment=()",
      "usb=()",
      "magnetometer=()",
      "gyroscope=()",
      "accelerometer=()",
    ].join(", "),

    // HSTS - Force HTTPS (only in production)
    ...(process.env.NODE_ENV === "production" && {
      "Strict-Transport-Security":
        "max-age=31536000; includeSubDomains; preload",
    }),
  };

  // Apply all security headers
  Object.entries(securityHeaders).forEach(([key, value]) => {
    if (value) {
      response.headers.set(key, value);
    }
  });

  return response;
}

// Configure which routes the middleware should run on
export const config = {
  matcher: [
    /*
     * Match all request paths except:
     * - _next/static (static files)
     * - _next/image (image optimization files)
     * - favicon.ico (favicon file)
     * - public folder
     */
    "/((?!_next/static|_next/image|favicon.ico|public).*)",
  ],
};
