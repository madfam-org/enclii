'use client'

import { useState } from 'react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import {
  Server,
  Globe,
  Layers,
  GitBranch,
  Shield,
  Network,
  Share2,
  Menu,
  X,
} from 'lucide-react'

const navItems = [
  { href: '/domains', label: 'Domains', icon: Globe },
  { href: '/fleet', label: 'Fleet', icon: Server },
  { href: '/clusters', label: 'Clusters', icon: Layers },
  { href: '/infrastructure', label: 'Infrastructure', icon: GitBranch },
  { href: '/propagation', label: 'Propagation', icon: Share2 },
  { href: '/governance', label: 'Governance', icon: Shield },
  { href: '/topology', label: 'Topology', icon: Network },
]

function NavLinks({ pathname, onNavigate }: { pathname: string; onNavigate?: () => void }) {
  return (
    <nav className="p-3 space-y-1">
      {navItems.map((item) => {
        const isActive = pathname === item.href || pathname.startsWith(item.href + '/')
        return (
          <Link
            key={item.href}
            href={item.href}
            onClick={onNavigate}
            className={`flex items-center gap-3 px-3 py-2 rounded-md text-sm font-medium transition-colors min-h-[44px] ${
              isActive
                ? 'bg-primary/10 text-primary border border-primary/20'
                : 'text-muted-foreground hover:text-foreground hover:bg-muted/50'
            }`}
          >
            <item.icon className="size-4 shrink-0" />
            {item.label}
          </Link>
        )
      })}
    </nav>
  )
}

export function Sidebar() {
  const pathname = usePathname()

  return (
    <aside className="hidden lg:block w-56 border-r border-border bg-card/30 min-h-[calc(100vh-57px)]">
      <NavLinks pathname={pathname} />
    </aside>
  )
}

export function MobileSidebarToggle() {
  const [open, setOpen] = useState(false)
  const pathname = usePathname()

  return (
    <div className="lg:hidden">
      <button
        onClick={() => setOpen(true)}
        className="p-2 rounded-md hover:bg-muted/50 min-h-[44px] min-w-[44px] flex items-center justify-center"
        aria-label="Open navigation"
      >
        <Menu className="size-5" />
      </button>

      {/* Mobile sidebar overlay */}
      {open && (
        <>
          <div
            className="fixed inset-0 z-40 bg-black/50"
            onClick={() => setOpen(false)}
          />
          <div className="fixed inset-y-0 left-0 z-50 w-64 bg-background border-r border-border shadow-lg">
            <div className="flex items-center justify-between p-3 border-b border-border">
              <span className="font-mono font-semibold text-sm">Navigation</span>
              <button
                onClick={() => setOpen(false)}
                className="p-2 rounded-md hover:bg-muted/50 min-h-[44px] min-w-[44px] flex items-center justify-center"
                aria-label="Close navigation"
              >
                <X className="size-5" />
              </button>
            </div>
            <NavLinks pathname={pathname} onNavigate={() => setOpen(false)} />
          </div>
        </>
      )}
    </div>
  )
}
