import type { NextConfig } from 'next'

const nextConfig: NextConfig = {
  reactStrictMode: true,
  output: 'standalone',

  // Environment variables (client-side via NEXT_PUBLIC_ prefix)
  //
  // NEXT_PUBLIC_COMMIT_SHA / NEXT_PUBLIC_BUILD_DATE are injected by the
  // Docker build (apps/status/Dockerfile) and surfaced in the footer for
  // incident-time version verification (audit ST-2). Falling back to
  // 'local'/'unknown' avoids a misleading "production"-looking SHA in
  // dev builds.
  env: {
    NEXT_PUBLIC_APP_URL: process.env.NEXT_PUBLIC_APP_URL || 'http://localhost:4204',
    NEXT_PUBLIC_COMMIT_SHA: process.env.NEXT_PUBLIC_COMMIT_SHA || 'local',
    NEXT_PUBLIC_BUILD_DATE: process.env.NEXT_PUBLIC_BUILD_DATE || 'unknown',
  },
}

export default nextConfig
