import type { Metadata } from 'next'
import './globals.css'

export const metadata: Metadata = {
  title: 'Enclii - Deploy Without the Bill Shock',
  description: 'Railway-style PaaS at 95% less cost. Auto-scaling, zero-downtime deployments, and built-in observability on cost-effective infrastructure.',
  keywords: ['PaaS', 'deployment', 'Kubernetes', 'containers', 'DevOps', 'Railway alternative'],
  authors: [{ name: 'Enclii Team' }],
  openGraph: {
    title: 'Enclii - Deploy Without the Bill Shock',
    description: 'Railway-style PaaS at 95% less cost. Auto-scaling, zero-downtime deployments, and built-in observability.',
    url: 'https://enclii.dev',
    siteName: 'Enclii',
    type: 'website',
  },
  twitter: {
    card: 'summary_large_image',
    title: 'Enclii - Deploy Without the Bill Shock',
    description: 'Railway-style PaaS at 95% less cost.',
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
