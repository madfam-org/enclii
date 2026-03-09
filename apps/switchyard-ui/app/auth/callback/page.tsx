"use client";

import { useEffect, useState, Suspense, useRef } from "react";
import { useSearchParams } from "next/navigation";
import { Spinner } from "@/components/ui/spinner";

/**
 * OAuth Callback — Direct PKCE code exchange
 *
 * Exchanges the authorization code for tokens via Janua's OAuth token endpoint,
 * then calls /auth/me with Bearer header to get user data, and stores session
 * via server-side /api/auth/session route.
 *
 * Uses window.location.href (not router.push) for redirect to ensure browser
 * processes Set-Cookie headers before the next page load.
 */

const JANUA_URL = process.env.NEXT_PUBLIC_JANUA_URL || "https://auth.madfam.io";
const OAUTH_CLIENT_ID = process.env.NEXT_PUBLIC_OAUTH_CLIENT_ID || "jnc_RqeHy54KYGjVr8yQiBeUncMhnQFhS2NA";

function AuthCallbackContent() {
  const searchParams = useSearchParams();
  const [status, setStatus] = useState<"processing" | "success" | "error">("processing");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const hasProcessedRef = useRef(false);

  useEffect(() => {
    async function processCallback() {
      if (hasProcessedRef.current) return;
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

      const code = searchParams.get("code");
      if (!code) {
        setStatus("error");
        setErrorMessage("No authorization code received from provider");
        return;
      }

      // Retrieve PKCE code_verifier from session storage
      const codeVerifier = sessionStorage.getItem("enclii_code_verifier");
      if (!codeVerifier) {
        setStatus("error");
        setErrorMessage("PKCE verification failed — no code verifier found. Please try logging in again.");
        return;
      }
      sessionStorage.removeItem("enclii_code_verifier");

      try {
        // Step 1: Exchange code for token via Janua OAuth token endpoint (PKCE)
        const tokenResponse = await fetch(`${JANUA_URL}/api/v1/oauth/token`, {
          method: "POST",
          headers: { "Content-Type": "application/x-www-form-urlencoded" },
          body: new URLSearchParams({
            grant_type: "authorization_code",
            code,
            redirect_uri: `${window.location.origin}/auth/callback`,
            client_id: OAUTH_CLIENT_ID,
            code_verifier: codeVerifier,
          }),
        });

        if (!tokenResponse.ok) {
          const errorData = await tokenResponse.json().catch(() => ({}));
          throw new Error(errorData.detail || errorData.error_description || "Failed to exchange authorization code");
        }

        const { access_token } = await tokenResponse.json();

        // Step 2: Get user data with Bearer header (not cookie-based)
        const meResponse = await fetch(`${JANUA_URL}/api/v1/auth/me`, {
          headers: { Authorization: `Bearer ${access_token}` },
        });

        if (!meResponse.ok) {
          throw new Error("Failed to verify user identity");
        }

        const userData = await meResponse.json();

        // Step 3: Set cookies via server-side API route (avoids client-side race conditions)
        const sessionResponse = await fetch("/api/auth/session", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            token: access_token,
            email: userData.email,
          }),
        });

        if (!sessionResponse.ok) {
          throw new Error("Failed to establish session");
        }

        setStatus("success");

        // Full page navigation ensures browser processes Set-Cookie headers
        setTimeout(() => {
          const returnUrl = localStorage.getItem("auth_return_url") || "/";
          localStorage.removeItem("auth_return_url");
          window.location.href = returnUrl;
        }, 500);
      } catch (err) {
        console.error("OAuth callback error:", err);
        setStatus("error");
        setErrorMessage(
          err instanceof Error ? err.message : "Failed to complete authentication"
        );
      }
    }

    processCallback();
  }, [searchParams]);

  return (
    <div className="text-center">
      <h1 className="text-3xl font-bold text-enclii-blue mb-2">Enclii</h1>
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
              onClick={() => { window.location.href = "/login"; }}
              className="w-full bg-enclii-blue text-white py-2 px-4 rounded-md hover:bg-blue-700 transition-colors"
            >
              Try again
            </button>
            <button
              onClick={() => { window.location.href = "/"; }}
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

function AuthCallbackLoading() {
  return (
    <div className="text-center">
      <h1 className="text-3xl font-bold text-enclii-blue mb-2">Enclii</h1>
      <p className="text-muted-foreground text-sm mb-8">Switchyard Platform</p>
      <div className="space-y-4">
        <Spinner size="lg" className="mx-auto" />
        <p className="text-muted-foreground">Loading...</p>
      </div>
    </div>
  );
}

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
