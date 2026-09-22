import type { Metadata, Viewport } from 'next'
import './globals.css'
import { StructuredData } from '@/components/structured-data'

export const viewport: Viewport = {
  width: 'device-width',
  initialScale: 1,
  maximumScale: 5,
}

export const metadata: Metadata = {
  // metadataBase lets Next resolve the canonical + OG URLs against the
  // production origin in the static export.
  metadataBase: new URL('https://enclii.dev'),
  title: 'Enclii - Deploy Without the Bill Shock',
  description: 'Open source DevOps platform. Auto-scaling, zero-downtime deployments, and built-in observability on infrastructure you own.',
  keywords: ['PaaS', 'deployment', 'Kubernetes', 'containers', 'DevOps', 'open source', 'GitOps'],
  authors: [{ name: 'MADFAM', url: 'https://madfam.io' }],
  // Canonical URL — one stable, citation-worthy address for the entity.
  alternates: { canonical: '/' },
  openGraph: {
    title: 'Enclii - Deploy Without the Bill Shock',
    description: 'Open source DevOps platform. Auto-scaling, zero-downtime deployments, and built-in observability.',
    url: 'https://enclii.dev',
    siteName: 'Enclii',
    type: 'website',
  },
  twitter: {
    card: 'summary_large_image',
    title: 'Enclii - Deploy Without the Bill Shock',
    description: 'Open source DevOps platform for containerized services.',
  },
  icons: { icon: '/favicon.ico' },
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en">
      <head>
        <StructuredData />
      </head>
      <body className="antialiased">{children}</body>
    </html>
  )
}
