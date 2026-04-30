'use client'

import { Button } from "@enclii/ui-components/button"
import { Shield, ArrowLeft, LogOut } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useAuth } from '@/contexts/AuthContext'

/**
 * Access Denied Page
 *
 * Shown when a non-superuser attempts to access Dispatch.
 */
export default function AccessDeniedPage() {
  const router = useRouter()
  const { logout, user } = useAuth()

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="max-w-md w-full text-center space-y-6">
        {/* Icon */}
        <div className="inline-flex p-4 rounded-full bg-destructive/10 border border-destructive/20">
          <Shield className="size-12 text-destructive" />
        </div>

        {/* Message */}
        <div className="space-y-2">
          <h1 className="text-2xl font-semibold text-foreground">Access Denied</h1>
          <p className="text-muted-foreground">
            Dispatch is restricted to infrastructure operators only.
          </p>
          {user && (
            <p className="text-sm text-muted-foreground">
              Signed in as: <span className="font-mono text-foreground">{user.email}</span>
            </p>
          )}
        </div>

        {/* Info Box */}
        <div className="rounded-lg border border-border bg-card p-4 text-left">
          <h3 className="font-medium text-foreground mb-2">What is Dispatch?</h3>
          <p className="text-sm text-muted-foreground">
            Dispatch is the Control Tower for managing Enclii infrastructure - domains,
            tunnels, and ecosystem resources. Access requires:
          </p>
          <ul className="mt-2 text-sm text-muted-foreground list-disc list-inside space-y-1">
            <li>Email from an authorized domain (e.g., <span className="font-mono text-primary">@madfam.io</span>)</li>
            <li>Operator role (<span className="font-mono text-primary">superadmin</span>, <span className="font-mono text-primary">admin</span>, or <span className="font-mono text-primary">operator</span>)</li>
          </ul>
        </div>

        {/* Actions */}
        <div className="flex flex-col sm:flex-row gap-3 justify-center">
          <Button variant="outline" onClick={() => router.push('https://app.enclii.dev')} className="gap-2">
            <ArrowLeft className="size-4" />
            Go to Switchyard
          </Button>
          <Button variant="destructive" onClick={logout} className="gap-2">
            <LogOut className="size-4" />
            Sign Out
          </Button>
        </div>

        {/* Footer */}
        <p className="text-xs text-muted-foreground">
          If you believe you should have access, contact the infrastructure team.
        </p>
      </div>
    </div>
  )
}
