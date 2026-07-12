'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Spinner } from '@/components/ui/spinner';
import { Button } from '@enclii/ui-components/button';
import { apiFetchResponse } from '@/lib/api';

type CheckoutState = 'starting' | 'redirecting' | 'unconfigured' | 'error';

/**
 * Upgrade page — the enclii-owned funnel entrypoint for tier upgrades.
 *
 * On load it POSTs to /v1/billing/checkout (the Dhanam federation relay,
 * mirrors janua#453). The backend resolves the caller's Dhanam billing
 * customer, opens a hosted checkout for `enclii_pro`, and returns the real
 * Dhanam-hosted checkout URL, which we redirect the browser to.
 *
 * This replaces the previous dead-end where tier-blocked users were sent to
 * https://dhanam.madfam.io/checkout — an NXDOMAIN host with no checkout route.
 *
 * 503 is handled honestly: the relay fails closed until the operator provisions
 * FEDERATION_API_TOKEN, and we say so rather than spinning forever.
 */
export default function UpgradePage() {
  const [state, setState] = useState<CheckoutState>('starting');
  const [errorMessage, setErrorMessage] = useState<string>('');
  const searchParams = useSearchParams();
  const canceled = searchParams?.get('canceled') === '1';
  // Guard against React strict-mode double-invocation firing two checkouts.
  const startedRef = useRef(false);

  const startCheckout = useCallback(async () => {
    try {
      const res = await apiFetchResponse('/v1/billing/checkout', {
        method: 'POST',
        body: JSON.stringify({ plan: 'pro' }),
      });

      if (res.ok) {
        const data = (await res.json()) as { checkout_url?: string };
        if (data.checkout_url) {
          setState('redirecting');
          window.location.href = data.checkout_url;
          return;
        }
        setErrorMessage('Checkout did not return a payment URL. Please try again.');
        setState('error');
        return;
      }

      if (res.status === 503) {
        setState('unconfigured');
        return;
      }

      const body = (await res.json().catch(() => ({}))) as { message?: string; error?: string };
      setErrorMessage(
        body.message || body.error || `Could not start checkout (HTTP ${res.status}).`,
      );
      setState('error');
    } catch (e) {
      setErrorMessage(
        e instanceof Error ? e.message : 'Could not reach the billing service. Please try again.',
      );
      setState('error');
    }
  }, []);

  useEffect(() => {
    if (startedRef.current) return;
    startedRef.current = true;
    // Intentionally trigger the checkout (an external system) on mount — the
    // setState calls happen only after the awaited fetch resolves, not
    // synchronously in the effect body.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void startCheckout();
  }, [startCheckout]);

  const retry = useCallback(() => {
    startedRef.current = true;
    setState('starting');
    setErrorMessage('');
    void startCheckout();
  }, [startCheckout]);

  return (
    <div className="mx-auto flex min-h-[60vh] max-w-lg items-center justify-center p-6">
      <Card className="w-full">
        <CardHeader>
          <CardTitle>Upgrade to Sovereign</CardTitle>
          <CardDescription>
            {canceled
              ? 'Your previous checkout was canceled. You can start again below.'
              : 'Unlock higher project and service limits on the Sovereign plan.'}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {(state === 'starting' || state === 'redirecting') && (
            <div className="flex items-center gap-3 py-4" role="status" aria-live="polite">
              <Spinner size="md" />
              <span>
                {state === 'redirecting'
                  ? 'Redirecting you to secure checkout…'
                  : 'Preparing your secure checkout…'}
              </span>
            </div>
          )}

          {state === 'unconfigured' && (
            <div className="space-y-4 py-2">
              <p className="text-sm">
                Billing isn&apos;t configured yet, so we can&apos;t start checkout right now. This
                is on our side — no action is needed from you.
              </p>
              <p className="text-muted-foreground text-sm">
                Please contact support and we&apos;ll get your upgrade sorted.
              </p>
              <Button variant="secondary" onClick={retry}>
                Try again
              </Button>
            </div>
          )}

          {state === 'error' && (
            <div className="space-y-4 py-2">
              <p className="text-destructive text-sm">{errorMessage}</p>
              <Button onClick={retry}>Try again</Button>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
