import type { NextConfig } from 'next'

const nextConfig: NextConfig = {
  reactStrictMode: true,
  output: 'standalone',
  transpilePackages: ['@janua/nextjs', '@janua/ui', '@janua/react-sdk', '@janua/typescript-sdk'],

  // Environment variables for Dispatch (client-side only - NEXT_PUBLIC_ prefix)
  // Server-side env vars (CLOUDFLARE_*) are read directly from process.env at runtime
  env: {
    // Dispatch runs on port 4203 (admin.enclii.dev)
    NEXT_PUBLIC_APP_URL: process.env.NEXT_PUBLIC_APP_URL || 'http://localhost:4203',
    // Janua SSO for authentication
    NEXT_PUBLIC_JANUA_URL: process.env.NEXT_PUBLIC_JANUA_URL || 'https://api.janua.dev',
  },
}

export default nextConfig
