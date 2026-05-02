'use client';

/**
 * Operational status view for the Janua SSO integration.
 *
 * Replaces the prior marketing-copy page (audit finding IG-1) which
 * rendered pricing tiers + "Deploy Now" CTAs inside the *protected*
 * app surface — misplaced for self-hosted master-admin operators who
 * already have Janua wired in. This page now answers the questions an
 * operator actually has when they land here:
 *
 *   1. Is Janua reachable from the browser? (live `/api/v1/auth/me` probe)
 *   2. Which OAuth client is this deployment configured against?
 *   3. What roles does my own session carry?
 *
 * Reachability is best-effort. If the probe is blocked by CORS/CSP we
 * fall back to "unknown" and surface the configured issuer URL — never
 * fabricate a green check.
 */

import { useEffect, useState } from 'react';
import Link from 'next/link';
import {
  Shield,
  CheckCircle2,
  AlertTriangle,
  HelpCircle,
  Code,
  ArrowUpRight,
} from 'lucide-react';
import { useAuth } from '@/contexts/AuthContext';

// Mirrors AuthContext defaults so the page renders the same issuer the
// auth flow actually uses. Kept inline (not imported) so this page stays
// independent of AuthContext internals.
const JANUA_BASE_URL =
  process.env.NEXT_PUBLIC_JANUA_URL || 'https://auth.madfam.io';
const OAUTH_CLIENT_ID =
  process.env.NEXT_PUBLIC_OAUTH_CLIENT_ID ||
  process.env.NEXT_PUBLIC_OIDC_CLIENT_ID ||
  '(not set)';

type Reachability = 'checking' | 'reachable' | 'unreachable' | 'unknown';

export default function JanuaIntegrationPage() {
  const { user, isAuthenticated, getAccessToken } = useAuth();
  const [reachability, setReachability] = useState<Reachability>('checking');
  const [reachabilityNote, setReachabilityNote] = useState<string>('');

  // Best-effort reachability probe. Hits the same `/api/v1/auth/me`
  // endpoint AuthContext uses; if Janua is up and the token is valid
  // we get a 200, anything else (network error, 5xx) is "unreachable".
  // 401 still means Janua is *up*, so we treat that as reachable too.
  useEffect(() => {
    const token = getAccessToken();
    if (!token) {
      setReachability('unknown');
      setReachabilityNote('No access token in this session — skipping live probe.');
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const r = await fetch(`${JANUA_BASE_URL}/api/v1/auth/me`, {
          headers: { Authorization: `Bearer ${token}` },
        });
        if (cancelled) return;
        if (r.ok || r.status === 401) {
          setReachability('reachable');
          setReachabilityNote(
            r.ok
              ? `HTTP ${r.status} — session validated.`
              : `HTTP ${r.status} — Janua is reachable but the token was rejected.`,
          );
        } else {
          setReachability('unreachable');
          setReachabilityNote(`HTTP ${r.status} from ${JANUA_BASE_URL}.`);
        }
      } catch (e) {
        if (cancelled) return;
        setReachability('unreachable');
        setReachabilityNote(
          e instanceof Error ? e.message : 'Network error during probe.',
        );
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [getAccessToken]);

  const roles = user?.roles ?? [];

  return (
    <div className="min-h-screen bg-muted/50">
      {/* Header — kept for breadcrumb continuity with the rest of the app. */}
      <section className="bg-card border-b border-border py-8">
        <div className="max-w-5xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex items-center gap-2 text-muted-foreground text-sm mb-3">
            <Link href="/integrations" className="hover:text-foreground">
              Integrations
            </Link>
            <span>/</span>
            <span className="text-foreground">Janua Authentication</span>
          </div>
          <div className="flex items-start justify-between gap-4">
            <div className="flex items-start gap-3">
              <div className="w-10 h-10 rounded-lg bg-primary/10 flex items-center justify-center shrink-0">
                <Shield className="w-5 h-5 text-primary" />
              </div>
              <div>
                <h1 className="text-2xl font-bold text-foreground">
                  Janua Authentication
                </h1>
                <p className="text-sm text-muted-foreground mt-1">
                  SSO provider for this Enclii deployment. Status &amp; current
                  configuration.
                </p>
              </div>
            </div>
            <Link
              href="https://janua.io/docs"
              target="_blank"
              rel="noopener noreferrer"
              className="hidden sm:inline-flex items-center gap-1.5 px-4 py-2 bg-card text-foreground text-sm font-medium rounded-md border border-border hover:bg-accent transition-colors"
            >
              <Code className="w-4 h-4" />
              View Docs
              <ArrowUpRight className="w-3.5 h-3.5" />
            </Link>
          </div>
        </div>
      </section>

      {/* Status grid */}
      <section className="py-8">
        <div className="max-w-5xl mx-auto px-4 sm:px-6 lg:px-8 space-y-6">
          {/* Reachability */}
          <div className="bg-card rounded-lg border border-border p-5">
            <div className="flex items-center justify-between gap-3 mb-3">
              <h2 className="text-sm font-semibold text-foreground uppercase tracking-wide">
                Reachability
              </h2>
              <ReachabilityBadge state={reachability} />
            </div>
            <dl className="grid sm:grid-cols-2 gap-x-6 gap-y-2 text-sm">
              <KV
                label="Issuer"
                value={
                  <code className="font-mono text-xs break-all">
                    {JANUA_BASE_URL}
                  </code>
                }
              />
              <KV
                label="Probe"
                value={
                  <span className="text-muted-foreground">
                    {reachabilityNote || `GET ${JANUA_BASE_URL}/api/v1/auth/me`}
                  </span>
                }
              />
            </dl>
          </div>

          {/* OAuth client */}
          <div className="bg-card rounded-lg border border-border p-5">
            <h2 className="text-sm font-semibold text-foreground uppercase tracking-wide mb-3">
              OAuth Client
            </h2>
            <dl className="grid sm:grid-cols-2 gap-x-6 gap-y-2 text-sm">
              <KV
                label="Client ID"
                value={
                  <code className="font-mono text-xs break-all">
                    {OAUTH_CLIENT_ID}
                  </code>
                }
              />
              <KV
                label="Source"
                value={
                  <span className="text-muted-foreground">
                    NEXT_PUBLIC_OAUTH_CLIENT_ID
                  </span>
                }
              />
            </dl>
          </div>

          {/* Current session */}
          <div className="bg-card rounded-lg border border-border p-5">
            <h2 className="text-sm font-semibold text-foreground uppercase tracking-wide mb-3">
              Current Session
            </h2>
            {!isAuthenticated || !user ? (
              <p className="text-sm text-muted-foreground">
                No authenticated session.
              </p>
            ) : (
              <dl className="grid sm:grid-cols-2 gap-x-6 gap-y-2 text-sm">
                <KV
                  label="Email"
                  value={
                    <span className="text-foreground">{user.email}</span>
                  }
                />
                <KV
                  label="Name"
                  value={
                    <span className="text-foreground">
                      {user.name || (
                        <span className="text-muted-foreground italic">
                          (not set)
                        </span>
                      )}
                    </span>
                  }
                />
                <KV
                  label="User ID"
                  value={
                    <code className="font-mono text-xs break-all">
                      {user.id}
                    </code>
                  }
                />
                <KV
                  label="Roles"
                  value={
                    roles.length === 0 ? (
                      <span className="text-muted-foreground italic">
                        (none)
                      </span>
                    ) : (
                      <span className="flex flex-wrap gap-1">
                        {roles.map((r) => (
                          <span
                            key={r}
                            className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-primary/10 text-primary"
                          >
                            {r}
                          </span>
                        ))}
                      </span>
                    )
                  }
                />
              </dl>
            )}
          </div>

          {/* Operator note */}
          <div className="bg-muted/50 rounded-lg border border-border p-4 text-sm text-muted-foreground">
            Janua is the SSO provider for this Enclii deployment. Operator
            runbook:{' '}
            <code className="font-mono text-xs">
              internal-devops/runbooks/janua-bootstrap.md
            </code>
            .
          </div>
        </div>
      </section>
    </div>
  );
}

function KV({
  label,
  value,
}: {
  label: string;
  value: React.ReactNode;
}) {
  return (
    <div className="flex items-baseline gap-3 min-w-0">
      <dt className="text-xs uppercase tracking-wide text-muted-foreground shrink-0 w-20">
        {label}
      </dt>
      <dd className="min-w-0 flex-1">{value}</dd>
    </div>
  );
}

function ReachabilityBadge({ state }: { state: Reachability }) {
  switch (state) {
    case 'reachable':
      return (
        <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-status-success-muted text-status-success-foreground">
          <CheckCircle2 className="w-3.5 h-3.5" />
          Reachable
        </span>
      );
    case 'unreachable':
      return (
        <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-status-error-muted text-status-error-foreground">
          <AlertTriangle className="w-3.5 h-3.5" />
          Unreachable
        </span>
      );
    case 'unknown':
      return (
        <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-muted text-muted-foreground">
          <HelpCircle className="w-3.5 h-3.5" />
          Unknown
        </span>
      );
    case 'checking':
    default:
      return (
        <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-muted text-muted-foreground">
          <span className="w-2 h-2 rounded-full bg-status-info animate-pulse" />
          Checking…
        </span>
      );
  }
}
