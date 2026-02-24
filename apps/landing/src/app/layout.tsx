import type { Metadata, Viewport } from 'next'
import './globals.css'

export const viewport: Viewport = {
  width: 'device-width',
  initialScale: 1,
  maximumScale: 5,
}

export const metadata: Metadata = {
  title: 'Enclii - Deploy Without the Bill Shock',
  description: 'Open source DevOps platform. Auto-scaling, zero-downtime deployments, and built-in observability on infrastructure you own.',
  keywords: ['PaaS', 'deployment', 'Kubernetes', 'containers', 'DevOps', 'open source', 'GitOps'],
  authors: [{ name: 'Enclii Team' }],
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
      <body className="antialiased">{children}</body>
    </html>
  )
}
