"use client";

import { useEffect, useState, Suspense, useRef } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useAuth } from "@/contexts/AuthContext";
import { Spinner } from "@/components/ui/spinner";

/**
 * OAuth Callback Content Component
 * Separated to allow Suspense boundary for useSearchParams
 */
function AuthCallbackContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { handleOAuthCallback, storeTokensFromRedirect } = useAuth();

  const [status, setStatus] = useState<"processing" | "success" | "error">(
    "processing",
  );
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  // Guard against duplicate callback processing (React 18 Strict Mode, etc.)
  const hasProcessedRef = useRef(false);

  useEffect(() => {
    async function processCallback() {
      // Prevent duplicate processing of OAuth callback
      // OAuth codes are single-use; processing twice causes errors
      if (hasProcessedRef.current) {
        return;
      }
      hasProcessedRef.current = true;

      // Check for error from OIDC provider
      const error = searchParams.get("error");
      const errorDescription = searchParams.get("error_description");

      if (error) {
        console.error("OAuth error:", error, errorDescription);
        setStatus("error");
        setErrorMessage(errorDescription || `Authentication failed: ${error}`);
        return;
      }

      // Check for tokens in query params (backend redirect flow)
      // This happens when the API callback redirects to UI with tokens
      const accessToken = searchParams.get("access_token");
      const refreshToken = searchParams.get("refresh_token");

      if (accessToken && refreshToken) {
        // Tokens provided directly via redirect - store them
        const expiresAt = searchParams.get("expires_at");
        const tokenType = searchParams.get("token_type");
        const idpToken = searchParams.get("idp_token");
        const idpTokenExpiresAt = searchParams.get("idp_token_expires_at");

        try {
          await storeTokensFromRedirect({
            accessToken,
            refreshToken,
            expiresAt: expiresAt ? new Date(parseInt(expiresAt) * 1000) : new Date(Date.now() + 15 * 60 * 1000),
            tokenType: tokenType || "Bearer",
            idpToken: idpToken || undefined,
            idpTokenExpiresAt: idpTokenExpiresAt ? new Date(parseInt(idpTokenExpiresAt) * 1000) : undefined,
          });

          setStatus("success");

          // Redirect to dashboard after short delay
          setTimeout(() => {
            const returnUrl = localStorage.getItem("auth_return_url") || "/";
            localStorage.removeItem("auth_return_url");
            router.push(returnUrl);
          }, 1500);
        } catch (err) {
          console.error("Failed to store tokens from redirect:", err);
          setStatus("error");
          setErrorMessage("Failed to complete authentication");
        }
        return;
      }

      // Get authorization code (old flow - UI calls API callback)
      const code = searchParams.get("code");
      const state = searchParams.get("state");

      if (!code) {
        setStatus("error");
        setErrorMessage("No authorization code received from provider");
        return;
      }

      try {
        // Exchange code for tokens via backend
        await handleOAuthCallback(code, state || undefined);

        setStatus("success");

        // Redirect to dashboard after short delay
        setTimeout(() => {
          const returnUrl = localStorage.getItem("auth_return_url") || "/";
          localStorage.removeItem("auth_return_url");
          router.push(returnUrl);
        }, 1500);
      } catch (err) {
        console.error("OAuth callback error:", err);
        setStatus("error");
        setErrorMessage(
          err instanceof Error
            ? err.message
            : "Failed to complete authentication",
        );
      }
    }

    processCallback();
  }, [searchParams, handleOAuthCallback, storeTokensFromRedirect, router]);

  return (
    <div className="text-center">
      {/* Enclii Logo */}
      <h1 className="text-3xl font-bold text-enclii-blue mb-2">🚂 Enclii</h1>
      <p className="text-muted-foreground text-sm mb-8">Switchyard Platform</p>

      {status === "processing" && (
        <div className="space-y-4">
          <Spinner size="lg" className="mx-auto" />
          <p className="text-muted-foreground">Completing authentication...</p>
          <p className="text-muted-foreground text-sm">
            Please wait while we verify your credentials
          </p>
        </div>
      )}

      {status === "success" && (
        <div className="space-y-4">
          <div className="rounded-full h-12 w-12 bg-status-success-muted mx-auto flex items-center justify-center">
            <svg
              aria-hidden="true"
              className="h-6 w-6 text-status-success"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M5 13l4 4L19 7"
              />
            </svg>
          </div>
          <p className="text-status-success font-medium">
            Authentication successful!
          </p>
          <p className="text-muted-foreground text-sm">Redirecting to dashboard...</p>
        </div>
      )}

      {status === "error" && (
        <div className="space-y-4">
          <div className="rounded-full h-12 w-12 bg-status-error-muted mx-auto flex items-center justify-center">
            <svg
              aria-hidden="true"
              className="h-6 w-6 text-status-error"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M6 18L18 6M6 6l12 12"
              />
            </svg>
          </div>
          <p className="text-status-error font-medium">Authentication failed</p>
          <p className="text-muted-foreground text-sm">{errorMessage}</p>

          <div className="pt-4 space-y-2">
            <button
              onClick={() => router.push("/auth/login")}
              className="w-full bg-enclii-blue text-white py-2 px-4 rounded-md hover:bg-blue-700 transition-colors"
            >
              Try again
            </button>
            <button
              onClick={() => router.push("/")}
              className="w-full bg-muted text-foreground py-2 px-4 rounded-md hover:bg-accent transition-colors"
            >
              Return to home
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

/**
 * Loading fallback for Suspense
 */
function AuthCallbackLoading() {
  return (
    <div className="text-center">
      <h1 className="text-3xl font-bold text-enclii-blue mb-2">🚂 Enclii</h1>
      <p className="text-muted-foreground text-sm mb-8">Switchyard Platform</p>
      <div className="space-y-4">
        <Spinner size="lg" className="mx-auto" />
        <p className="text-muted-foreground">Loading...</p>
      </div>
    </div>
  );
}

/**
 * OAuth Callback Page
 *
 * Handles the redirect from the OIDC provider (Janua/Janua) after authentication.
 * Exchanges the authorization code for tokens and establishes the session.
 */
export default function AuthCallbackPage() {
  return (
    <div className="min-h-screen flex items-center justify-center bg-muted/50">
      <div className="max-w-md w-full space-y-8 p-8">
        <Suspense fallback={<AuthCallbackLoading />}>
          <AuthCallbackContent />
        </Suspense>
      </div>
    </div>
  );
}
