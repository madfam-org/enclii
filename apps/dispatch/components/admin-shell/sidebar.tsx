'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import {
  Server,
  Globe,
  Layers,
  GitBranch,
  Shield,
  DollarSign,
  Network,
  LayoutDashboard,
  Share2,
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

export function Sidebar() {
  const pathname = usePathname()

  return (
    <aside className="w-56 border-r border-border bg-card/30 min-h-[calc(100vh-57px)]">
      <nav className="p-3 space-y-1">
        {navItems.map((item) => {
          const isActive = pathname === item.href || pathname.startsWith(item.href + '/')
          return (
            <Link
              key={item.href}
              href={item.href}
              className={`flex items-center gap-3 px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                isActive
                  ? 'bg-primary/10 text-primary border border-primary/20'
                  : 'text-muted-foreground hover:text-foreground hover:bg-muted/50'
              }`}
            >
              <item.icon className="size-4" />
              {item.label}
            </Link>
          )
        })}
      </nav>
    </aside>
  )
}
