'use client';

import { ThemeProvider } from 'next-themes';
import { AuthProvider } from '@/contexts/AuthContext';
import { ScopeProvider } from '@/contexts/ScopeContext';
import { PostHogProvider } from '@/lib/analytics/PostHogProvider';

export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <ThemeProvider
      attribute="class"
      defaultTheme="dark"
      enableSystem
      disableTransitionOnChange
    >
      <AuthProvider>
        <PostHogProvider>
          <ScopeProvider>{children}</ScopeProvider>
        </PostHogProvider>
      </AuthProvider>
    </ThemeProvider>
  );
}
