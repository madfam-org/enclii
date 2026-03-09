import type { Metadata, Viewport } from 'next'
import { GeistSans } from 'geist/font/sans'
import { GeistMono } from 'geist/font/mono'
import './globals.css'
import { Header, Footer } from '@/components/Header'
import { getSiteConfig } from '@/lib/config'

const siteConfig = getSiteConfig()

export const viewport: Viewport = {
  width: 'device-width',
  initialScale: 1,
  maximumScale: 5,
}

export const metadata: Metadata = {
  metadataBase: new URL(siteConfig.url),
  title: siteConfig.name,
  description: `Current status and incident history for ${siteConfig.name}`,
  alternates: {
    canonical: '/',
    types: {
      'application/atom+xml': '/feed.xml',
    },
  },
  openGraph: {
    title: siteConfig.name,
    description: `Current status and incident history for ${siteConfig.name}`,
    type: 'website',
    url: siteConfig.url,
    siteName: siteConfig.name,
  },
  twitter: {
    card: 'summary',
    title: siteConfig.name,
    description: `Current status and incident history for ${siteConfig.name}`,
  },
  other: {
    'theme-color': '#0d1117',
  },
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html
      lang="en"
      data-theme="dark"
      suppressHydrationWarning
      className={`${GeistSans.variable} ${GeistMono.variable}`}
    >
      <body className="font-sans antialiased bg-background text-foreground min-h-screen flex flex-col">
        <a
          href="#main-content"
          className="sr-only focus:not-sr-only focus:absolute focus:z-[100] focus:top-2 focus:left-2 focus:bg-primary focus:text-primary-foreground focus:px-4 focus:py-2 focus:rounded-md"
        >
          Skip to main content
        </a>
        <Header siteName={siteConfig.name} siteUrl={siteConfig.url} />
        <main id="main-content" className="flex-1">
          {children}
        </main>
        <Footer siteName={siteConfig.name} />
      </body>
    </html>
  )
}
