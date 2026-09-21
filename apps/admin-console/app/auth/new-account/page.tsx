'use client'

import { useEffect } from 'react'
import { Radio, Loader2 } from 'lucide-react'
import { useAuth } from '@/contexts/AuthContext'

/**
 * "Open account in a new tab" bootstrap (two-tab focus).
 *
 * The account menu's «Open account in a new tab» opens THIS page in a fresh tab.
 * PKCE requires a `code_verifier` in the tab's own (per-tab) sessionStorage, so
 * the flow has to be STARTED from inside the new tab — the opener cannot seed it.
 * On mount we call `login({ prompt: 'select_account' })`, which sends this tab to
 * Janua's account chooser. Picking an account there brings that account back to
 * this tab (via the `#janua_sid` fragment the callback adopts), leaving the
 * original tab untouched.
 */
export default function NewAccountPage() {
  const { login } = useAuth()

  useEffect(() => {
    login({ prompt: 'select_account' })
  }, [login])

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="max-w-md w-full text-center space-y-6">
        <div className="inline-flex p-3 rounded-xl bg-primary/10 border border-primary/20">
          <Radio className="size-8 text-primary glow-effect" />
        </div>
        <div className="space-y-4">
          <Loader2 className="size-8 mx-auto text-primary animate-spin" />
          <div>
            <h2 className="text-lg font-medium text-foreground">Choose an account</h2>
            <p className="text-sm text-muted-foreground">
              Opening the account chooser<span className="terminal-cursor" />
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}
