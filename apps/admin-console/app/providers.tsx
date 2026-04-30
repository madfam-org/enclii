'use client'

/**
 * Dispatch Providers
 *
 * IMPORTANT: Dispatch forces Solarpunk theme - no theme switching allowed.
 * This is the God View for infrastructure management.
 */

import { ThemeProvider } from 'next-themes'
import { AuthProvider } from '@/contexts/AuthContext'

export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <ThemeProvider
      attribute="data-theme"
      defaultTheme="solarpunk"
      forcedTheme="solarpunk" // FORCED: No theme switching in Dispatch
      disableTransitionOnChange
      enableSystem={false} // Ignore system preference
    >
      <AuthProvider>{children}</AuthProvider>
    </ThemeProvider>
  )
}
