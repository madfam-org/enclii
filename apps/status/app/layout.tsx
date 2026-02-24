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
  title: siteConfig.name,
  description: `Current status and incident history for ${siteConfig.name}`,
  openGraph: {
    title: siteConfig.name,
    description: `Current status and incident history for ${siteConfig.name}`,
    type: 'website',
    url: siteConfig.url,
  },
  twitter: {
    card: 'summary',
    title: siteConfig.name,
    description: `Current status and incident history for ${siteConfig.name}`,
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
        <Header siteName={siteConfig.name} siteUrl={siteConfig.url} />
        <main className="flex-1">
          {children}
        </main>
        <Footer siteName={siteConfig.name} />
      </body>
    </html>
  )
}
