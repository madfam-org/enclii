# MADFAM Ecosystem — Critical Path to Revenue

> **Goal:** $0 → first paying customer across the MADFAM product ecosystem
> **Context:** 31 repos, $55/month infra, $0 revenue, 0 paying customers (as of Feb 25, 2026)
> **Infra:** 2-node k3s cluster (Hetzner + Cloudflare), ArgoCD GitOps, Janua SSO
> **Last updated:** Feb 26, 2026

---

## Table of Contents

- [Business Model](#business-model)
- [Ecosystem Map](#ecosystem-map)
- [Sprint Overview](#sprint-overview)
- [Sprint 1 — Dhanam Billing (Revenue Gate)](#sprint-1--dhanam-billing-revenue-gate)
- [Sprint 2 — Billing Hardening + Janua Config](#sprint-2--billing-hardening--janua-config)
- [Sprint 3 — Billing SDK + Auth for Avala & Digifab](#sprint-3--billing-sdk--auth-for-avala--digifab)
- [Sprint 4 — Monetize Avala + Digifab](#sprint-4--monetize-avala--digifab)
- [Sprint 5 — Auth for Tezca & Yantra4D + Platform Polish](#sprint-5--auth-for-tezca--yantra4d--platform-polish)
- [Deferred (Sprint 7+)](#deferred-sprint-7)
- [Manual Tasks (Human Required)](#manual-tasks-human-required)
- [Verification Plan](#verification-plan)
- [Decision Log](#decision-log)

---

## Business Model

### Core Principles

1. **NO money-back guarantees** — cancel anytime instead
2. **Community tier = self-hosted ONLY** — not a free SaaS option. Users who self-host get community features with their own infrastructure
3. **ALL SaaS users must pay** — Essentials is the entry point, Pro is premium
4. **Shopify-style intro offer** — $0.99/mo for first 3 months, then regular price. Credit card always required upfront
5. **PremiumGate recognizes essentials as a paid tier** — not just pro

### Pricing Summary

| Product | Essentials | Pro | Billing Hub |
|---------|-----------|-----|-------------|
| **Dhanam** (personal finance) | $4.99/mo | $11.99/mo | Direct (Stripe) |
| **Avala** (education) | $9.99/mo | $24.99/mo | Via Dhanam external checkout |
| **Digifab** (quoting) | Per-order | Per-order | Via Dhanam one-time checkout |
| **Tezca** (legal research) | Free (auth-gated features) | — | — |
| **Yantra4D** (3D design) | Free (auth-gated features) | — | — |

All intro pricing: **$0.99/mo for 3 months** via Stripe coupon.

### Billing Architecture

```
                    ┌─────────────┐
                    │   Stripe    │
                    │  (payments) │
                    └──────┬──────┘
                           │ webhooks
                    ┌──────▼──────┐
                    │  Dhanam API │  ← central billing hub
                    │ api.dhan.am │
                    └──────┬──────┘
              ┌────────────┼────────────┐
              │            │            │
        ┌─────▼─────┐ ┌───▼───┐ ┌─────▼──────┐
        │   Avala   │ │Digifab│ │  (future)  │
        │ billing   │ │ order │ │ products   │
        │ via SDK   │ │ pay   │ │            │
        └───────────┘ └───────┘ └────────────┘
```

All products authenticate through **Janua** (auth.madfam.io). All payments route through **Dhanam API** as the billing hub.

---

## Ecosystem Map

### Revenue-Critical Repos

| Repo | Type | Sprint | Role |
|------|------|--------|------|
| **dhanam** | Next.js + NestJS | 1-5 | Billing hub, personal finance app |
| **janua** | Python + React | 2 | SSO/OIDC provider (12 SDK packages published) |
| **avala** | Next.js + NestJS | 3-4 | Education platform (DC-3, SIRCE compliance) |
| **digifab-quoting** | Next.js + NestJS | 3-4 | Digital fabrication quoting tool |
| **enclii** | Go + Next.js | 1 (docs only) | DevOps platform (hosts everything) |

### Ecosystem-Completeness Repos (no direct revenue)

| Repo | Type | Sprint | Role |
|------|------|--------|------|
| **tezca** | Django + React | 5 | Mexican legal research platform |
| **yantra4d** | Astro + Express | 5 | 3D parametric design studio |
| **madfam-site** | Next.js | 5 | Portfolio + blog (SEO) |
| **solarpunk-foundry** | Docs | 5 | Internal infrastructure docs |

### Deferred Repos (no sprint assigned)

| Repo | Reason |
|------|--------|
| ceq | Already has Janua auth. Deploy when ready |
| forgesight | Feeds into digifab later. No users yet |
| sim4d | Merge plan into yantra4d (architecture decision) |
| legal-ops | Merge plan into tezca (architecture decision) |
| stratum-tcg | Hobby project |
| geom-core | Maintenance-only library |
| Auto-Claude | Separate initiative (AI agents virtual office) |

---

## Sprint Overview

```
Sprint 1 ────────→ Sprint 2 ──────────→ Sprint 3 ──────────→ Sprint 4 ──────────→ Sprint 5
Dhanam billing     Billing hardening     Billing SDK           Monetize Avala       Auth for Tezca
(REVENUE GATE)     Janua OAuth clients   Auth for Avala        Monetize Digifab     Auth for Yantra4D
                                         Auth for Digifab      One-time payments    Platform polish
                                                                                    madfam.io site
```

**Rule:** Nothing in Sprint N+1 starts until Sprint N's go/no-go passes.
**Exception:** Sprint 3 auth work (A2.x, Q2.x) only needs J2.3 from Sprint 2, not D2.x.

---

## Sprint 1 — Dhanam Billing (Revenue Gate)

> **Goal:** A real credit card can complete checkout on `app.dhan.am`

### Code Tasks

| # | Task | Status | Repo | Key Files |
|---|------|--------|------|-----------|
| E1.1 | Fix aspirational components in CLAUDE.md | Done | enclii | `CLAUDE.md` |
| E1.2 | Standardize ArgoCD app count to 17 | Done | enclii | `CLAUDE.md`, `llms.txt`, `AI_CONTEXT.md` |
| E1.5 | Update ROADMAP.md | Done | enclii | `ROADMAP.md` |
| D1.3 | Fix PremiumUpsell (dual-tier pricing) | Done | dhanam | `PremiumUpsell.tsx` |
| D1.4 | Billing dashboard page | Done | dhanam | `billing/page.tsx` |
| D1.5 | Upgrade page (2-col, intro offer) | Done | dhanam | `billing/upgrade/page.tsx` |
| D1.6 | Success/cancel pages | Done | dhanam | `billing/success/page.tsx`, `cancel/page.tsx` |
| D1.7 | Plan-based billing service | Done | dhanam | `billing.service.ts` |
| D1.8 | Billing nav link + i18n | Done | dhanam | `dashboard-nav.tsx`, i18n files |
| C1 | Remove community from SaaS pricing | Done | dhanam | `billing/upgrade/page.tsx` |
| C2 | Remove money-back guarantee | Done | dhanam | `PremiumUpsell.tsx` |
| C3 | Community = "Self-hosted tier" | Done | dhanam | `billing/page.tsx` |
| C4 | PremiumGate: essentials = paid | Done | dhanam | `PremiumGate.tsx` |
| C5 | Soft paywall SubscriptionBanner | Done | dhanam | `SubscriptionBanner.tsx`, `layout.tsx` |
| C6 | Stripe intro coupon wiring | Done | dhanam | `stripe.service.ts`, `billing.service.ts` |
| C7 | Billing service JSDoc update | Done | dhanam | `billing.service.ts` |

### Commits

| Repo | SHA | Message |
|------|-----|---------|
| enclii | `373188a` | `docs: fix aspirational components, standardize ArgoCD count, update roadmap` |
| dhanam | `fb5574f` | `feat(billing): add multi-tier subscription with Essentials and Pro plans` |
| dhanam | `ba277c2` | `fix(billing): correct business model — remove free SaaS tier, add intro pricing` |

### Manual Tasks Remaining (Sprint 1)

| # | Task | Owner | Details |
|---|------|-------|---------|
| D1.1 | Stripe Dashboard Setup | Human | Create "Dhanam Essentials" ($4.99/79 MXN), "Dhanam Pro" ($11.99/199 MXN). Create coupon: -$4.00/mo × 3 months (Essentials → $0.99) and -$11.00/mo × 3 months (Pro → $0.99). Webhook → `https://api.dhan.am/billing/webhook` |
| D1.2 | Set K8s secrets | Human | `STRIPE_SECRET_KEY`, `STRIPE_PUBLISHABLE_KEY`, `STRIPE_WEBHOOK_SECRET`, `STRIPE_ESSENTIALS_PRICE_ID`, `STRIPE_PREMIUM_PRICE_ID`, `STRIPE_INTRO_COUPON_ID` |
| D1.9 | End-to-end test | Human | Signup → banner → choose plan → Stripe checkout → paid dashboard |

### Go/No-Go

- [ ] Test card `4242 4242 4242 4242` completes checkout on `app.dhan.am`
- [ ] SubscriptionBanner disappears after payment
- [ ] PremiumGate grants essentials users access to simulation features
- [ ] No "money-back guarantee" text visible anywhere
- [ ] No community/free SaaS option on pricing page

---

## Sprint 2 — Billing Hardening + Janua Config

> **Goal:** Billing is production-safe. Other products can integrate auth.

### Code Tasks

| # | Task | Repo | Key Files | Notes |
|---|------|------|-----------|-------|
| D2.1 | Webhook idempotency | dhanam | `billing.service.ts` | Check `stripeEventId` uniqueness before processing |
| D2.2 | Stripe Customer Portal verification | dhanam | `billing/page.tsx` | Already partially wired (manage button) |
| D2.3 | Payment failure grace period | dhanam | `billing.service.ts`, `schema.prisma` | 7-day grace via `subscriptionGracePeriodEnd` |
| D2.4 | Usage upsell triggers | dhanam | Simulation/ESG pages | Wire `UsageIndicator` into feature pages |

### Manual Tasks

| # | Task | Owner | Details |
|---|------|-------|---------|
| J2.2 | Verify `ENABLE_SIGNUPS=true` | Human | Janua production env — self-service registration must be on |
| J2.3 | Register OAuth clients | Human | Janua admin dashboard: register avala, digifab-quoting, tezca, yantra4d with redirect URIs |

### Go/No-Go

- [ ] Duplicate webhook does not create duplicate billing events
- [ ] Failed payment gives 7-day grace period before downgrade
- [ ] OAuth clients registered for avala + digifab-quoting in Janua

---

## Sprint 3 — Billing SDK + Auth for Avala & Digifab

> **Goal:** External products can authenticate via Janua and prepare for billing integration.

### Code Tasks

| # | Task | Repo | Key Files | Notes |
|---|------|------|-----------|-------|
| D2.5 | Create `@dhanam/billing-sdk` | dhanam | `packages/billing-sdk/` (NEW) | Client + React hooks + `<SubscriptionGate>` + `<UpgradeButton>` |
| D2.6 | Publish billing SDK | dhanam | CI workflow | npm.madfam.io on tag |
| A2.1 | Replace local JWT with Janua OIDC (web) | avala | `apps/web/` | `@janua/react-sdk` + `@janua/nextjs`. Pattern: dhanam's `janua-oauth.ts` |
| A2.2 | Replace local JWT with Janua JWKS (api) | avala | `apps/api/` | Pattern: dhanam's `jwt-auth.guard.ts` |
| Q2.1 | Replace local JWT with Janua OIDC (web) | digifab-quoting | `apps/web/` | Same pattern as A2.1 |
| Q2.2 | Replace local JWT with Janua JWKS (api) | digifab-quoting | `apps/api/` | Same pattern as A2.2 |

**Parallelizable:** A2.x and Q2.x run simultaneously. D2.5-D2.6 can run in parallel with auth work.

### Go/No-Go

- [ ] `pnpm add @dhanam/billing-sdk` installs from npm.madfam.io
- [ ] Avala: "Sign In" redirects to auth.madfam.io and returns
- [ ] Digifab: same Janua auth flow works

---

## Sprint 4 — Monetize Avala + Digifab

> **Goal:** First revenue from non-Dhanam products.

### Code Tasks

| # | Task | Repo | Key Files | Notes |
|---|------|------|-----------|-------|
| D3.1 | One-time payment endpoint | dhanam | `billing.controller.ts` | `GET /billing/checkout/order?user_id=&amount=&currency=&description=&return_url=` |
| A3.2 | Integrate `@dhanam/billing-sdk` | avala | `apps/web/` | `<SubscriptionGate>` around DC-3, SIRCE. `<UpgradeButton>` → Dhanam checkout |
| A3.3 | Create billing pages | avala | `apps/web/src/app/billing/` | Tier + usage view. Upgrade → Dhanam external checkout |
| Q3.1 | Quote payment via Dhanam | digifab-quoting | `apps/web/` | "Accept Quote" → Dhanam one-time checkout |
| Q3.2 | Payment success callback | digifab-quoting | `apps/api/` | Webhook: order → "paid" |

### Manual Tasks

| # | Task | Owner | Details |
|---|------|-------|---------|
| A3.1 | Avala pricing in Stripe | Human | Essentials ($9.99/mo, 50 students, DC-3), Pro ($24.99/mo, unlimited, SIRCE, Open Badges 3.0) |

### Go/No-Go

- [ ] Avala user subscribes to Essentials via Dhanam checkout → DC-3 features unlock
- [ ] Digifab: accept quote → pay → order status "paid"
- [ ] Dhanam admin: billing events from avala + digifab visible

---

## Sprint 5 — Auth for Tezca & Yantra4D + Platform Polish

> **Goal:** Complete the identity fabric. Establish web presence.

### Code Tasks

| # | Task | Repo | Key Files | Notes |
|---|------|------|-----------|-------|
| T4.1 | Janua OIDC auth (Django) | tezca | Settings, middleware | `mozilla-django-oidc`. Anonymous browsing preserved |
| T4.2 | Add CLAUDE.md | tezca | `CLAUDE.md` | Missing from audit |
| T4.3 | Add LICENSE (AGPL-3.0) | tezca | `LICENSE` | Missing from audit |
| T4.4 | Authenticated features | tezca | Web app | Saved searches, tracked legislation, export history |
| Y4.1 | Optional Janua auth in Studio | yantra4d | `apps/studio/` | Guest mode preserved. Login → save projects, share, higher limits |
| Y4.2 | Map Janua roles to tiers | yantra4d | `apps/api/tiers.json` | guest/basic/pro/madfam tier system |
| D5.2 | Stripe MX payment methods | dhanam | `stripe-mx.service.ts` | OXXO + SPEI for Mexican users |
| M5.1 | Product portfolio pages | madfam-site | `app/products/` | Pricing + sign-up links for all products |
| M5.2 | Blog setup | madfam-site | `app/blog/` | SEO for each product vertical |
| S5.1 | Sanitize public docs | solarpunk-foundry | Throughout | Remove internal infra details |
| S5.2 | Update ecosystem inventory | solarpunk-foundry | `README.md` | Reflect post-archive repo count |

### Manual Tasks

| # | Task | Owner | Details |
|---|------|-------|---------|
| D4.1 | admin@dhan.am financial hub | Human | Create Stripe account, link MX bank, configure Belvo |
| D5.1 | Mobile billing tab | Human/Agent | `@stripe/stripe-react-native` payment sheet |

### Go/No-Go

- [ ] Tezca: log in via Janua → save search → log out → log in → search restored
- [ ] Yantra4D: browse as guest → log in → save project → verify persistence
- [ ] madfam.io shows all products with pricing

---

## Deferred (Sprint 7+)

These repos do **not** contribute to the 12-week revenue goal. Park them.

| Repo | Task | Reason |
|------|------|--------|
| **ceq** | Deploy to Enclii | Already has Janua auth. No users yet |
| **forgesight** | Replace local auth, expose pricing API | Feeds into digifab later |
| **sim4d** | Plan merge into Yantra4D | Architecture decision (OCCT WASM + `.bflow.json`) |
| **legal-ops** | Plan merge into Tezca | Architecture decision (LaTeX templates, NOM-151) |
| **legal-ops** | Add LICENSE (AGPL-3.0) | Missing, but repo is deferred |
| **stratum-tcg** | — | Hobby project. Revisit for crowdfunding |
| **geom-core** | — | Maintenance-only library |
| **Auto-Claude** | — | Separate initiative (AI agents virtual office) |

---

## Manual Tasks (Human Required)

These cannot be delegated to code agents.

| Task | Sprint | Why Manual | Status |
|------|--------|-----------|--------|
| Create Stripe products + prices + coupons | 1 | Stripe Dashboard access | Pending |
| Set Stripe env vars in K8s secrets | 1 | `kubectl` access to cluster | Pending |
| End-to-end test (Stripe test mode) | 1 | Browser + test card | Pending |
| Verify `ENABLE_SIGNUPS=true` in Janua | 2 | Janua production config | Pending |
| Register OAuth clients in Janua admin | 2 | Janua admin dashboard | Pending |
| Create Avala pricing in Stripe | 4 | Stripe Dashboard | Pending |
| Create admin@dhan.am email account | 5 | Email provider access | Pending |
| Link Stripe to MX bank account | 5 | Bank credentials | Pending |
| Create Belvo account | 5 | Belvo dashboard access | Pending |

---

## Verification Plan

### Sprint 1

1. Visit `app.dhan.am` → sign up via Janua
2. Land on dashboard with **persistent subscribe banner** at top
3. Dashboard shows demo data. Cannot connect bank accounts or save real data
4. Click "Choose a Plan" → 2-column pricing: Essentials + Pro
5. Intro pricing: "$0.99/mo for 3 months, then $4.99/mo" (strikethrough)
6. **No community/free SaaS option**. No "money-back guarantee" text
7. Click "Subscribe to Essentials" → Stripe Checkout with credit card required + coupon
8. After payment → banner disappears. Dashboard shows "Essentials" badge
9. PremiumGate: essentials user can access simulation features
10. Navigate to `/billing` → subscription status, usage, manage button

### Sprint 2

1. Cancel subscription via Stripe Portal → user downgrades to community
2. Trigger duplicate webhook → verify idempotency
3. Payment failure → 7-day grace period → then downgrade

### Sprint 3

1. `pnpm add @dhanam/billing-sdk` from avala repo → installs correctly
2. Avala: "Sign In" → auth.madfam.io → returns to avala
3. Digifab: same Janua auth flow works

### Sprint 4

1. Avala: subscribe to Essentials via Dhanam checkout → DC-3 unlocks
2. Digifab: accept quote → pay → order status "paid"
3. Dhanam admin: billing events from avala + digifab visible

### Sprint 5

1. Tezca: log in via Janua → save search → log out → log in → restored
2. Yantra4D: browse as guest → log in → save project → persistent
3. admin@dhan.am: Stripe shows all revenue streams

---

## Decision Log

| Date | Decision | Rationale |
|------|----------|-----------|
| 2026-02-25 | No money-back guarantees | Simplifies refund logic. Cancel-anytime is sufficient |
| 2026-02-25 | Community = self-hosted only | SaaS users must pay. No free tier cannibalizing revenue |
| 2026-02-25 | $0.99 intro via Stripe coupon | Simpler than trial_period_days. No custom trial logic |
| 2026-02-25 | Dhanam as central billing hub | Avoids duplicating Stripe integration in every product |
| 2026-02-25 | PremiumGate with `requiredTier` prop | Essentials = paid (general features), Pro = premium (LifeBeat, household) |
| 2026-02-26 | Sprint 1 code corrections shipped | `ba277c2` (dhanam), `373188a` (enclii). Business model aligned in code |

---

## Priority Rules (When Everything Feels Urgent)

1. **Revenue beats perfection.** A working Stripe checkout beats a beautiful SDK nobody can pay through.
2. **The critical path is: Dhanam billing → Stripe setup → deploy → Janua config → everything else.** No other repo can integrate until these are done.
3. **One repo at a time per sprint.** Dhanam (S1) → Janua (S2) → Avala + Digifab in parallel (S3).
4. **Auth-only integrations generate zero revenue.** Tezca and Yantra4D are Sprint 5.
5. **The billing SDK is Sprint 3, not Sprint 1.** Don't build abstractions before the concrete works.
6. **Test in Stripe test mode first.** `sk_test_*` before `sk_live_*`.
7. **Deferred repos stay deferred.** sim4d, legal-ops, forgesight, ceq, stratum-tcg, Auto-Claude — none contribute to the 12-week revenue goal.

---

*This document is the single source of truth for the $0-to-revenue critical path across all MADFAM repos.*
*Managed from the enclii repo because Enclii orchestrates all deployments.*
