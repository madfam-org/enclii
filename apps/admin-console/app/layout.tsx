import type { Metadata, Viewport } from 'next'
import { Suspense } from 'react'
import { GeistSans } from 'geist/font/sans'
import { GeistMono } from 'geist/font/mono'
import './globals.css'
import { Providers } from './providers'
import { PostHogProvider } from '@/components/PostHogProvider'

export const viewport: Viewport = {
  width: 'device-width',
  initialScale: 1,
  maximumScale: 5,
}

export const metadata: Metadata = {
  title: 'Enclii Admin | Universal Control Plane',
  description: 'Universal Control Plane - Manage fleet, infrastructure, clusters, and governance',
  robots: 'noindex, nofollow', // Superuser-only, no indexing
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html
      lang="en"
      data-theme="solarpunk"
      suppressHydrationWarning
      className={`${GeistSans.variable} ${GeistMono.variable}`}
    >
      <body className="font-mono antialiased bg-background text-foreground">
        <Providers>
          <Suspense fallback={null}>
            <PostHogProvider>
              {children}
            </PostHogProvider>
          </Suspense>
        </Providers>
      </body>
    </html>
  )
}
