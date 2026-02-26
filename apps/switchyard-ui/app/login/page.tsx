"use client";

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/contexts/AuthContext";
import { SignIn } from "@janua/ui";
import { Spinner } from "@/components/ui/spinner";

export default function LoginPage() {
  const router = useRouter();
  const { login, loginWithOIDC, isAuthenticated, isLoading, authMode } = useAuth();

  const [error, setError] = useState<string | null>(null);

  // Redirect if already authenticated
  useEffect(() => {
    if (isAuthenticated && !isLoading) {
      router.push("/");
    }
  }, [isAuthenticated, isLoading, router]);

  const handleOIDCLogin = () => {
    loginWithOIDC();
  };

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-background">
        <div className="text-center">
          <Spinner size="lg" className="mx-auto" />
          <p className="mt-4 text-muted-foreground">Loading...</p>
        </div>
      </div>
    );
  }

  // Already authenticated - will redirect
  if (isAuthenticated) {
    return null;
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-background py-12 px-4 sm:px-6 lg:px-8">
      <div className="max-w-md w-full space-y-8">
        {/* Header */}
        <div className="text-center">
          <h1 className="text-4xl font-bold text-enclii-blue mb-2">Enclii</h1>
          <p className="text-muted-foreground text-sm mb-6">Switchyard Platform</p>
        </div>

        {/* Error message */}
        {error && (
          <div className="bg-status-error-muted border border-status-error/30 rounded-md p-4">
            <div className="flex">
              <div className="flex-shrink-0">
                <svg
                  aria-hidden="true"
                  className="h-5 w-5 text-status-error"
                  viewBox="0 0 20 20"
                  fill="currentColor"
                >
                  <path
                    fillRule="evenodd"
                    d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z"
                    clipRule="evenodd"
                  />
                </svg>
              </div>
              <div className="ml-3">
                <p className="text-sm text-status-error-foreground">{error}</p>
              </div>
            </div>
          </div>
        )}

        {/* OIDC Login (Primary for production) */}
        {authMode === "oidc" ? (
          <div className="space-y-6">
            <button
              onClick={handleOIDCLogin}
              className="w-full flex justify-center items-center gap-2 py-3 px-4 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-enclii-blue hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-enclii-blue transition-colors"
            >
              <svg aria-hidden="true" className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
              </svg>
              Sign in with Janua SSO
            </button>

            <div className="text-center text-sm text-muted-foreground">
              <p>
                You will be redirected to your organization's identity provider.
              </p>
            </div>
          </div>
        ) : (
          /* Local Login Form (Bootstrap mode) — uses shared SignIn component */
          <SignIn
            apiUrl={process.env.NEXT_PUBLIC_API_URL || "http://localhost:4200"}
            afterSignIn={() => router.push("/")}
            onError={(err) => setError(err.message)}
            socialProviders={{ google: true, github: true, microsoft: true, apple: true }}
            showRememberMe={false}
            signUpUrl="/register"
          />
        )}

        {/* Footer */}
        <div className="text-center text-xs text-muted-foreground mt-8">
          <p>&copy; {new Date().getFullYear()} Enclii Platform. Built for developers.</p>
        </div>
      </div>
    </div>
  );
}
