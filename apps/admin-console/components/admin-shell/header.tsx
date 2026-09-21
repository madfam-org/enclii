'use client'

import { useAuth } from '@/contexts/AuthContext'
import { Button } from "@enclii/ui-components/button"
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
} from "@enclii/ui-components/dropdown-menu"
import { Radio, LogOut, User, ChevronDown, Users, UserPlus } from 'lucide-react'
import { MobileSidebarToggle } from './sidebar'

export function AdminHeader() {
  const { user, isAuthorized, login, logout } = useAuth()

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
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="sm" className="gap-2" aria-label="Account menu">
                <User className="size-4" />
                <span className="hidden sm:inline font-mono truncate max-w-[180px]">{user?.email}</span>
                {isAuthorized && (
                  <span className="hidden sm:inline px-1.5 py-0.5 rounded text-xs bg-primary/20 text-primary border border-primary/30">
                    {displayRole}
                  </span>
                )}
                <ChevronDown className="size-4 opacity-60" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuLabel className="font-mono text-xs break-all">
                {user?.email}
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => login({ prompt: 'select_account' })} className="gap-2">
                <Users className="size-4" />
                Switch account
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => login({ prompt: 'login' })} className="gap-2">
                <UserPlus className="size-4" />
                Sign in as someone else
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={logout} className="gap-2">
                <LogOut className="size-4" />
                Sign out
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
    </header>
  )
}
