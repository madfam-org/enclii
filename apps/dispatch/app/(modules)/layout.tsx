'use client'

import { useAuth } from '@/contexts/AuthContext'
import { useRouter } from 'next/navigation'
import { useEffect } from 'react'
import { AdminHeader } from '@/components/admin-shell/header'
import { Sidebar } from '@/components/admin-shell/sidebar'
import { Shield } from 'lucide-react'
import { Button } from '@/components/ui/button'

export default function ModulesLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const { isLoading, isAuthenticated, error } = useAuth()
  const router = useRouter()

  useEffect(() => {
    if (!isLoading && !isAuthenticated) {
      router.push('/login')
    }
  }, [isLoading, isAuthenticated, router])

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="flex flex-col items-center gap-4">
          <div className="size-8 border-2 border-primary border-t-transparent rounded-full animate-spin" />
          <p className="text-muted-foreground font-mono text-sm">Initializing Admin</p>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="min-h-screen flex items-center justify-center p-4">
        <div className="max-w-md w-full rounded-lg border border-destructive/30 bg-destructive/10 p-6 text-center">
          <Shield className="size-12 mx-auto mb-4 text-destructive" />
          <h1 className="text-xl font-semibold text-foreground mb-2">Access Denied</h1>
          <p className="text-muted-foreground mb-4">{error}</p>
          <Button variant="outline" onClick={() => router.push('/login')}>
            Return to Login
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-background">
      <AdminHeader />
      <div className="flex">
        <Sidebar />
        <main className="flex-1 p-6">{children}</main>
      </div>
    </div>
  )
}
