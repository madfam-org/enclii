"use client";

/**
 * Landing page for the verification link embedded in the email.
 *
 * URL shape:
 *   /signup/verify?signup_id=<uuid>&token=<hex>
 *
 * We POST the token to the API, then bounce the user back to the
 * wizard — which will re-read status and move them to step 3.
 *
 * We deliberately don't render any sensitive state on this page — if
 * someone shares the verification URL, the token is single-use and
 * expires in 24h anyway.
 */

import { useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { apiFetchResponse } from "@/lib/api";
import { Spinner } from "@/components/ui/spinner";

export default function VerifyEmailPage() {
  const router = useRouter();
  const search = useSearchParams();
  const signupID = search.get("signup_id");
  const token = search.get("token");
  const [state, setState] = useState<"verifying" | "error" | "success">("verifying");
  const [message, setMessage] = useState<string>("");

  useEffect(() => {
    if (!signupID || !token) {
      setState("error");
      setMessage("This verification link is missing parameters.");
      return;
    }
    (async () => {
      try {
        const res = await apiFetchResponse(`/v1/signup/${signupID}/verify`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ token }),
        });
        if (res.ok) {
          setState("success");
          // Brief pause for visual confirmation, then bounce back to the wizard.
          setTimeout(() => router.replace(`/signup?signup_id=${signupID}`), 800);
          return;
        }
        const body = await safeJson(res);
        if (res.status === 409) {
          setState("error");
          setMessage("This email is already registered. Try signing in instead.");
          return;
        }
        setState("error");
        setMessage(body?.error ?? "Verification failed. The link may have expired.");
      } catch {
        setState("error");
        setMessage("Network error. Try again in a moment.");
      }
    })();
  }, [signupID, token, router]);

  return (
    <div className="min-h-screen flex items-center justify-center bg-background px-4">
      <div className="max-w-md w-full text-center space-y-4">
        <h1 className="text-2xl font-bold text-enclii-blue">Enclii</h1>
        {state === "verifying" && (
          <>
            <Spinner size="lg" className="mx-auto" />
            <p className="text-sm text-muted-foreground">Verifying your email&hellip;</p>
          </>
        )}
        {state === "success" && (
          <p className="text-sm">Verified. Redirecting&hellip;</p>
        )}
        {state === "error" && (
          <>
            <p className="text-status-error text-sm">{message}</p>
            <a href="/signup" className="inline-block text-sm underline">
              Back to signup
            </a>
          </>
        )}
      </div>
    </div>
  );
}

async function safeJson(res: Response): Promise<any> {
  try {
    return await res.json();
  } catch {
    return null;
  }
}
