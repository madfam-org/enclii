'use client'

import { useAuth } from '@/contexts/AuthContext'
import { Button } from '@/components/ui/button'
import { Radio, LogOut, User } from 'lucide-react'
import { MobileSidebarToggle } from './sidebar'

export function AdminHeader() {
  const { user, isAuthorized, logout } = useAuth()

  const displayRole = user?.roles?.includes('superadmin')
    ? 'SUPERADMIN'
    : user?.roles?.includes('admin')
      ? 'ADMIN'
      : 'OPERATOR'

  return (
    <header className="border-b border-border bg-card/50 backdrop-blur-sm sticky top-0 z-50">
      <div className="px-4 py-3 flex items-center justify-between gap-2">
        <div className="flex items-center gap-3 min-w-0">
          <MobileSidebarToggle />
          <div className="p-2 rounded-lg bg-primary/10 border border-primary/20 shrink-0">
            <Radio className="size-5 text-primary" />
          </div>
          <div className="min-w-0">
            <h1 className="font-mono font-semibold text-foreground text-lg tracking-tight truncate">
              ENCLII ADMIN
            </h1>
            <p className="text-xs text-muted-foreground hidden sm:block">Universal Control Plane</p>
          </div>
        </div>

        <div className="flex items-center gap-2 sm:gap-4 shrink-0">
          <div className="hidden sm:flex items-center gap-2 text-sm text-muted-foreground">
            <User className="size-4" />
            <span className="font-mono truncate max-w-[200px]">{user?.email}</span>
            {isAuthorized && (
              <span className="px-1.5 py-0.5 rounded text-xs bg-primary/20 text-primary border border-primary/30">
                {displayRole}
              </span>
            )}
          </div>
          <Button variant="ghost" size="sm" onClick={logout} className="gap-2">
            <LogOut className="size-4" />
            <span className="hidden sm:inline">Logout</span>
          </Button>
        </div>
      </div>
    </header>
  )
}
