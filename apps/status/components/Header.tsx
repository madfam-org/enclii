'use client'

import { cn } from '@/lib/utils'
import { Activity, Menu, X, History, Home, Rss } from 'lucide-react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useState } from 'react'
import { ThemeToggle } from './ThemeToggle'

interface HeaderProps {
  siteName: string
  siteUrl?: string
}

export function Header({ siteName, siteUrl }: HeaderProps) {
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  const pathname = usePathname()

  const navItems = [
    { href: '/', label: 'Status', icon: Home },
    { href: '/incidents', label: 'Incident History', icon: History },
  ]

  return (
    <header className="border-b border-border bg-card/80 backdrop-blur-sm sticky top-0 z-50">
      <div className="max-w-5xl mx-auto px-4 sm:px-6">
        <div className="flex items-center justify-between h-16">
          {/* Logo */}
          <Link href="/" className="flex items-center gap-3 group">
            <div className="p-2 rounded-lg bg-primary/10 group-hover:bg-primary/20 transition-colors">
              <Activity className="size-5 text-primary" />
            </div>
            <span className="font-semibold text-lg">{siteName}</span>
          </Link>

          {/* Desktop Navigation */}
          <nav className="hidden md:flex items-center gap-1">
            {navItems.map(({ href, label, icon: Icon }) => (
              <Link
                key={href}
                href={href}
                className={cn(
                  'flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium',
                  'transition-colors hover:bg-muted',
                  pathname === href
                    ? 'text-primary bg-primary/10'
                    : 'text-muted-foreground hover:text-foreground'
                )}
              >
                <Icon className="size-4" />
                {label}
              </Link>
            ))}
            <Link
              href="/feed.xml"
              className="flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors hover:bg-muted text-muted-foreground hover:text-foreground"
              aria-label="RSS Feed"
            >
              <Rss className="size-4" />
            </Link>
            <ThemeToggle />
          </nav>

          {/* Mobile Menu Button */}
          <button
            onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
            className="md:hidden p-2 -mr-2 rounded-md hover:bg-muted transition-colors"
            aria-label="Toggle menu"
            aria-expanded={mobileMenuOpen}
            aria-controls="mobile-nav"
          >
            {mobileMenuOpen ? (
              <X className="size-5" />
            ) : (
              <Menu className="size-5" />
            )}
          </button>
        </div>

        {/* Mobile Navigation */}
        {mobileMenuOpen && (
          <nav id="mobile-nav" className="md:hidden py-4 border-t border-border">
            {navItems.map(({ href, label, icon: Icon }) => (
              <Link
                key={href}
                href={href}
                onClick={() => setMobileMenuOpen(false)}
                className={cn(
                  'flex items-center gap-3 px-3 py-3 rounded-md text-sm font-medium',
                  'transition-colors',
                  pathname === href
                    ? 'text-primary bg-primary/10'
                    : 'text-muted-foreground hover:text-foreground hover:bg-muted'
                )}
              >
                <Icon className="size-5" />
                {label}
              </Link>
            ))}
            <div className="px-3 py-2">
              <ThemeToggle />
            </div>
          </nav>
        )}
      </div>
    </header>
  )
}

export function Footer({ siteName }: { siteName: string }) {
  const currentYear = new Date().getFullYear()

  return (
    <footer className="border-t border-border py-8 mt-12">
      <div className="max-w-5xl mx-auto px-4 sm:px-6">
        <div className="flex flex-col sm:flex-row items-center justify-between gap-4 text-sm text-muted-foreground">
          <div className="flex items-center gap-2">
            <Activity className="size-4" />
            <span>{siteName}</span>
          </div>
          <div className="flex items-center gap-4">
            <Link
              href="/feed.xml"
              className="flex items-center gap-1.5 hover:text-foreground transition-colors"
              aria-label="RSS Feed"
            >
              <Rss className="size-3.5" />
              <span>RSS</span>
            </Link>
            <span>&copy; {currentYear} {siteName.replace(/ (System )?Status$/, '')}. All rights reserved.</span>
          </div>
        </div>
      </div>
    </footer>
  )
}
