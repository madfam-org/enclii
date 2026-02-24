'use client';

import { useAuth } from '@/contexts/AuthContext';
import { AlertCircle, X } from 'lucide-react';

/**
 * AuthErrorBanner displays auth-related errors (session expiry, token refresh failures, etc.)
 * Should be placed near the top of the authenticated layout.
 */
export function AuthErrorBanner() {
  const { authError, clearAuthError } = useAuth();

  if (!authError) {
    return null;
  }

  return (
    <div className="fixed top-0 left-0 right-0 z-[60] bg-destructive/10 border-b border-destructive/30 shadow-sm animate-in slide-in-from-top duration-300">
      <div className="max-w-7xl mx-auto px-4 py-3 sm:px-6 lg:px-8">
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <AlertCircle className="h-5 w-5 text-destructive flex-shrink-0" />
            <p className="text-sm text-destructive">{authError}</p>
          </div>
          <button
            onClick={clearAuthError}
            className="flex-shrink-0 p-1 rounded hover:bg-destructive/20 transition-colors"
            aria-label="Dismiss"
          >
            <X className="h-4 w-4 text-destructive" />
          </button>
        </div>
      </div>
    </div>
  );
}
