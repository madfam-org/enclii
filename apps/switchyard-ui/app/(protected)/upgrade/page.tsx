'use client';

import { useEffect, useMemo, useRef } from 'react';
import { useSearchParams } from 'next/navigation';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Spinner } from '@/components/ui/spinner';
import { Button } from '@enclii/ui-components/button';
import { TIER_CONFIG, getDhanamCheckoutUrl, getUpgradeReturnUrl } from '@/lib/tiers';

// Sourced from TIER_CONFIG (lib/tiers.ts) — the canonical customer-facing
// catalog that also drives PricingModal and the API's tier-limit copy.
// Don't hardcode the tier name/price here separately; that's how this page
// drifted out of sync before (see docs/faq/billing.md for the ratified copy).
const proTier = TIER_CONFIG.pro;

/**
 * Upgrade page — the enclii-owned funnel entrypoint for tier upgrades.
 *
 * It sends the browser to Dhanam's hosted checkout
 * (`app.dhan.am/checkout?plan=enclii_pro&product=enclii&return_url=…`), the
 * link pattern Karafiel, Forgesight and Tezca use. The buyer signs in with
 * Janua there and Dhanam decides who the purchase entitles, so nothing about
 * the user rides in the URL.
 *
 * It used to POST /v1/billing/checkout (switchyard's federation relay). That
 * relay sends the Janua subject as checkout metadata, which Dhanam's
 * federation contract refuses with 400, and names no organization to
 * entitle, which it refuses with 422: no buyer could reach a payment page.
 */
export default function UpgradePage() {
  const searchParams = useSearchParams();
  const canceled = searchParams?.get('canceled') === '1';
  const requestedReturn = searchParams?.get('return_url');
  const startedRef = useRef(false);

  const checkoutUrl = useMemo(
    () => getDhanamCheckoutUrl(getUpgradeReturnUrl(requestedReturn)),
    [requestedReturn],
  );

  useEffect(() => {
    // A canceled checkout lands back here: offer the link instead of looping.
    if (canceled || startedRef.current) return;
    startedRef.current = true;
    window.location.assign(checkoutUrl);
  }, [canceled, checkoutUrl]);

  return (
    <div className="mx-auto flex min-h-[60vh] max-w-lg items-center justify-center p-6">
      <Card className="w-full">
        <CardHeader>
          <CardTitle>Upgrade to {proTier.name}</CardTitle>
          <CardDescription>
            {canceled
              ? 'Your previous checkout was canceled. You can start again below.'
              : `Unlock higher project and service limits on the ${proTier.name} plan (${proTier.price}).`}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {canceled ? (
            <div className="space-y-4 py-2">
              <Button onClick={() => window.location.assign(checkoutUrl)}>Start checkout</Button>
            </div>
          ) : (
            <div className="flex items-center gap-3 py-4" role="status" aria-live="polite">
              <Spinner size="md" />
              <span>Redirecting you to secure checkout…</span>
            </div>
          )}
          <p className="text-muted-foreground pt-2 text-sm">
            Checkout runs on Dhanam, MADFAM&apos;s billing service.{' '}
            <a className="underline" href={checkoutUrl}>
              Continue to checkout
            </a>
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
