"use client";

/**
 * P3.2 Sprint 1 — Self-serve signup wizard.
 *
 * Three-step flow:
 *   1. Email + optional company name (POST /v1/signup)
 *   2. Email verification check-in screen (user clicks the link in their
 *      inbox, which lands on /signup/verify → that page posts the token
 *      and redirects back here)
 *   3. "Connect GitHub" CTA → hits /v1/signup/:id/github/authorize which
 *      returns the upstream URL the browser navigates to. Post-return,
 *      we call POST /v1/signup/:id/provision and ship the user to their
 *      new project.
 *
 * State is reflected in the URL (?signup_id=...) so a refresh or a fresh
 * browser tab resumes the wizard rather than losing place.
 *
 * The feature is gated server-side by ENCLII_SIGNUP_ENABLED; if it's off
 * the API returns 404 and this page shows a friendly "not yet available"
 * message with a link back to /login.
 */

import { useEffect, useState, useCallback } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { API_BASE_URL } from "@/lib/constants";
import { Button } from "@enclii/ui-components/button";
import { Input } from "@enclii/ui-components/input";
import { Label } from "@enclii/ui-components/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Spinner } from "@/components/ui/spinner";

type SignupStatus =
  | "pending_verification"
  | "verified"
  | "github_linked"
  | "provisioning"
  | "ready"
  | "failed";

type SignupState = {
  signup_id: string;
  email: string;
  status: SignupStatus;
  next_step: string;
  project_id?: string;
  github_username?: string;
  error_message?: string;
};

type WizardStep = 1 | 2 | 3 | "done" | "error";

function stepFromStatus(status: SignupStatus | null): WizardStep {
  switch (status) {
    case null:
    case undefined:
      return 1;
    case "pending_verification":
      return 2;
    case "verified":
      return 3;
    case "github_linked":
    case "provisioning":
      return 3;
    case "ready":
      return "done";
    case "failed":
      return "error";
    default:
      return 1;
  }
}

export default function SignupPage() {
  const router = useRouter();
  const search = useSearchParams();
  const initialSignupID = search.get("signup_id") ?? "";
  const oauthError = search.get("error");

  const [signupID, setSignupID] = useState<string>(initialSignupID);
  const [state, setState] = useState<SignupState | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Form state (step 1)
  const [email, setEmail] = useState("");
  const [companyName, setCompanyName] = useState("");
  const [acceptTerms, setAcceptTerms] = useState(false);

  const step: WizardStep = state ? stepFromStatus(state.status) : 1;

  // Poll status when we have a signup ID but haven't reached terminal.
  const refreshStatus = useCallback(async () => {
    if (!signupID) return;
    try {
      const res = await fetch(`${API_BASE_URL}/v1/signup/${signupID}/status`);
      if (res.status === 404) {
        setError("Signup is not available right now. Try again later.");
        return;
      }
      if (!res.ok) return;
      const data = (await res.json()) as SignupState;
      setState(data);

      // Terminal transitions.
      if (data.status === "github_linked") {
        // Automatically kick off provisioning.
        provision(data.signup_id).catch(() => {});
      } else if (data.status === "ready" && data.project_id) {
        // Small delay so the user reads "provisioned" before we bounce.
        setTimeout(() => router.push(`/projects/${data.project_id}`), 1200);
      }
    } catch (e) {
      // Network errors during polling aren't fatal; try again on the next tick.
    }
  }, [signupID, router]);

  useEffect(() => {
    if (!signupID) return;
    refreshStatus();
    const t = setInterval(refreshStatus, 3000);
    return () => clearInterval(t);
  }, [signupID, refreshStatus]);

  useEffect(() => {
    if (oauthError) {
      const msg =
        oauthError === "oauth_denied"
          ? "You declined GitHub access. Try again to continue."
          : "GitHub connection failed. Try again.";
      setError(msg);
    }
  }, [oauthError]);

  async function handleInitiate(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE_URL}/v1/signup`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, company_name: companyName || undefined }),
      });
      if (res.status === 404) {
        setError("Signup isn't open yet. Join the waitlist at enclii.dev.");
        return;
      }
      if (!res.ok) {
        const body = await safeJson(res);
        setError(body?.error ?? "Could not create signup. Check your email and try again.");
        return;
      }
      const data = await res.json();
      setSignupID(data.signup_id);
      // Reflect in URL so refresh resumes.
      router.replace(`/signup?signup_id=${data.signup_id}`);
    } finally {
      setLoading(false);
    }
  }

  async function connectGithub() {
    if (!state || !signupID) return;
    setError(null);
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE_URL}/v1/signup/${signupID}/github/authorize`);
      if (!res.ok) {
        setError("Could not start GitHub connection. Try again.");
        return;
      }
      const data = (await res.json()) as { authorization_url: string };
      window.location.href = data.authorization_url;
    } finally {
      setLoading(false);
    }
  }

  async function provision(id: string) {
    const res = await fetch(`${API_BASE_URL}/v1/signup/${id}/provision`, {
      method: "POST",
    });
    if (!res.ok) {
      const body = await safeJson(res);
      setError(body?.error ?? "Provisioning failed. Please try again.");
    }
  }

  // --- Rendering --------------------------------------------------------

  return (
    <div className="min-h-screen flex flex-col bg-background">
      <header className="w-full border-b border-border">
        <div className="max-w-4xl mx-auto px-4 py-4 flex items-center justify-between">
          <h1 className="text-2xl font-bold text-enclii-blue">Enclii</h1>
          <a href="/login" className="text-sm text-muted-foreground hover:text-foreground">
            Already have an account? Sign in
          </a>
        </div>
      </header>

      <main className="flex-1 flex items-center justify-center px-4 py-12">
        <div className="w-full max-w-md space-y-8">
          <Stepper step={step} />

          {error && <ErrorBanner message={error} />}

          {step === 1 && (
            <Step1Email
              email={email}
              companyName={companyName}
              acceptTerms={acceptTerms}
              loading={loading}
              setEmail={setEmail}
              setCompanyName={setCompanyName}
              setAcceptTerms={setAcceptTerms}
              onSubmit={handleInitiate}
            />
          )}

          {step === 2 && state && <Step2VerifyEmail email={state.email} onResend={() => setSignupID(state.signup_id)} />}

          {step === 3 && state && (
            <Step3ConnectGithub
              state={state}
              loading={loading}
              onConnect={connectGithub}
            />
          )}

          {step === "done" && <DoneScreen />}

          {step === "error" && (
            <FailureScreen message={state?.error_message ?? "Something went wrong."} onRetry={() => router.replace("/signup")} />
          )}
        </div>
      </main>
    </div>
  );
}

// ---------- Stepper ---------------------------------------------------------

function Stepper({ step }: { step: WizardStep }) {
  const steps = [
    { n: 1, label: "Your email" },
    { n: 2, label: "Verify" },
    { n: 3, label: "Connect GitHub" },
  ];
  const active = typeof step === "number" ? step : step === "done" ? 3 : 1;
  return (
    <ol className="flex items-center justify-between">
      {steps.map((s, i) => (
        <li key={s.n} className="flex-1 flex items-center">
          <div
            className={`w-8 h-8 rounded-full flex items-center justify-center text-sm font-medium ${
              active >= s.n ? "bg-enclii-blue text-white" : "bg-muted text-muted-foreground"
            }`}
            aria-current={active === s.n ? "step" : undefined}
          >
            {s.n}
          </div>
          <span className={`ml-2 text-sm ${active >= s.n ? "text-foreground" : "text-muted-foreground"}`}>{s.label}</span>
          {i < steps.length - 1 && <div className={`flex-1 h-px mx-3 ${active > s.n ? "bg-enclii-blue" : "bg-border"}`} />}
        </li>
      ))}
    </ol>
  );
}

// ---------- Step 1: email + company ----------------------------------------

function Step1Email(props: {
  email: string;
  companyName: string;
  acceptTerms: boolean;
  loading: boolean;
  setEmail: (v: string) => void;
  setCompanyName: (v: string) => void;
  setAcceptTerms: (v: boolean) => void;
  onSubmit: (e: React.FormEvent) => void;
}) {
  return (
    <form onSubmit={props.onSubmit} className="space-y-5">
      <div>
        <h2 className="text-xl font-semibold">Create your Enclii account</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          We&apos;ll email you a verification link. No credit card required.
        </p>
      </div>

      <div className="space-y-2">
        <Label htmlFor="email">Work email</Label>
        <Input
          id="email"
          type="email"
          required
          autoComplete="email"
          placeholder="you@company.com"
          value={props.email}
          onChange={(e) => props.setEmail(e.target.value)}
        />
      </div>

      <div className="space-y-2">
        <Label htmlFor="company">Company (optional)</Label>
        <Input
          id="company"
          type="text"
          placeholder="Acme Inc."
          value={props.companyName}
          onChange={(e) => props.setCompanyName(e.target.value)}
        />
      </div>

      <div className="flex items-start gap-2">
        <Checkbox
          id="terms"
          checked={props.acceptTerms}
          onCheckedChange={(v) => props.setAcceptTerms(v === true)}
        />
        <Label htmlFor="terms" className="text-sm text-muted-foreground leading-tight">
          I agree to the{" "}
          <a href="/terms" className="underline">
            Terms of Service
          </a>{" "}
          and{" "}
          <a href="/privacy" className="underline">
            Privacy Policy
          </a>
          .
        </Label>
      </div>

      <Button type="submit" disabled={!props.acceptTerms || props.loading || !props.email} className="w-full">
        {props.loading ? <Spinner size="sm" /> : "Continue"}
      </Button>
    </form>
  );
}

// ---------- Step 2: check email --------------------------------------------

function Step2VerifyEmail({ email, onResend }: { email: string; onResend: () => void }) {
  return (
    <div className="text-center space-y-4">
      <div className="mx-auto w-16 h-16 rounded-full bg-enclii-blue/10 flex items-center justify-center">
        <svg className="w-8 h-8 text-enclii-blue" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
        </svg>
      </div>
      <h2 className="text-xl font-semibold">Check your email</h2>
      <p className="text-sm text-muted-foreground">
        We sent a verification link to <strong className="text-foreground">{email}</strong>. Click it to continue.
      </p>
      <p className="text-xs text-muted-foreground">
        Didn&apos;t get it? Check spam — or{" "}
        <button type="button" onClick={onResend} className="underline">
          resend
        </button>
        .
      </p>
    </div>
  );
}

// ---------- Step 3: connect GitHub -----------------------------------------

function Step3ConnectGithub({
  state,
  loading,
  onConnect,
}: {
  state: SignupState;
  loading: boolean;
  onConnect: () => void;
}) {
  const linked = state.status === "github_linked" || state.status === "provisioning";
  return (
    <div className="space-y-5">
      <div>
        <h2 className="text-xl font-semibold">Connect GitHub</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          We&apos;ll use GitHub to import a repo for your first deploy. You can revoke access any time from GitHub settings.
        </p>
      </div>

      {linked ? (
        <div className="rounded-md border border-border p-4 bg-muted/30 text-center">
          <Spinner size="md" className="mx-auto" />
          <p className="mt-3 text-sm">Creating your first project&hellip;</p>
          {state.github_username && (
            <p className="text-xs text-muted-foreground mt-1">Connected as {state.github_username}</p>
          )}
        </div>
      ) : (
        <Button onClick={onConnect} disabled={loading} className="w-full">
          <svg className="w-4 h-4 mr-2" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
            <path d="M12 .296C5.372.296 0 5.668 0 12.296c0 5.302 3.438 9.8 8.205 11.387.6.11.82-.26.82-.58 0-.286-.01-1.043-.015-2.048-3.338.724-4.042-1.61-4.042-1.61-.546-1.385-1.333-1.755-1.333-1.755-1.09-.745.083-.73.083-.73 1.205.085 1.838 1.236 1.838 1.236 1.07 1.835 2.807 1.305 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.466-1.333-5.466-5.93 0-1.31.467-2.38 1.235-3.22-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.3 1.23A11.5 11.5 0 0112 6.08c1.02.005 2.047.138 3.005.404 2.29-1.552 3.298-1.23 3.298-1.23.653 1.652.242 2.873.118 3.176.77.84 1.233 1.91 1.233 3.22 0 4.61-2.805 5.62-5.475 5.92.43.37.814 1.096.814 2.21 0 1.596-.014 2.883-.014 3.276 0 .322.216.697.824.578C20.565 22.092 24 17.594 24 12.296 24 5.668 18.627.296 12 .296z" />
          </svg>
          {loading ? <Spinner size="sm" /> : "Connect GitHub"}
        </Button>
      )}
    </div>
  );
}

// ---------- Terminal screens -----------------------------------------------

function DoneScreen() {
  return (
    <div className="text-center space-y-4">
      <div className="mx-auto w-16 h-16 rounded-full bg-status-success/10 flex items-center justify-center">
        <svg className="w-8 h-8 text-status-success" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
        </svg>
      </div>
      <h2 className="text-xl font-semibold">You&apos;re in. Redirecting&hellip;</h2>
      <Spinner size="md" className="mx-auto" />
    </div>
  );
}

function FailureScreen({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="space-y-4">
      <h2 className="text-xl font-semibold text-status-error">Signup failed</h2>
      <p className="text-sm text-muted-foreground">{message}</p>
      <Button onClick={onRetry} variant="outline" className="w-full">
        Start over
      </Button>
    </div>
  );
}

function ErrorBanner({ message }: { message: string }) {
  return (
    <div className="rounded-md border border-status-error/30 bg-status-error-muted p-3 text-sm text-status-error-foreground">
      {message}
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
