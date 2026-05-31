# Enclii + Janua Product Roadmap

> **Vision:** The complete self-hosted alternative to Vercel + Auth0  
> **Status:** Production-running beta → **Commercial GA program active** (~70% scoped GA as of 2026-05-29)

---

## Commercial GA program (May–July 2026)

**Primary objective:** Reach **100% scoped Commercial GA** by ~2026-07-14.

| Track | Readiness | Blocker |
|-------|-----------|---------|
| Stability GA | ~74% | Security sign-off, restore drill, 30-day SLO clock |
| Commercial GA | ~70% | Monetization QA, legal publish, SLO window |
| Engineering (bets A+B+C) | ~90% | Staging proven; prod smoke optional |

**Master plan:** [docs/production/COMMERCIAL_GA_MASTER_PLAN.md](docs/production/COMMERCIAL_GA_MASTER_PLAN.md)  
**Scorecard:** [docs/production/GA_READINESS_SCORECARD.md](docs/production/GA_READINESS_SCORECARD.md)  
**Ops queue:** [docs/production/REMAINING_OPS_GA.md](docs/production/REMAINING_OPS_GA.md)

### Execution waves

| Wave | Target | Outcome |
|------|--------|---------|
| **0** | 1–2 days | Security release + cluster P0 (O-3, O-5, O-6) |
| **1** | 3–5 days | DR, Vault/ESO, Cosign (O-8–O-11) |
| **2** | 3–5 days | Signup/pricing QA, Dhanam checkout |
| **3** | 30 calendar days | 99.95% API SLO → **Stability GA** |
| **4** | 1–2 weeks | SLA, support, privacy/terms → **Commercial GA** |

### Product bets (Commercial GA scope)

| Bet | Feature | Status |
|-----|---------|--------|
| **A** | Preview environments | ✅ Staging proven 2026-05-23 |
| **B** | Custom domains + TLS | ✅ Staging proven 2026-05-23 |
| **C** | Persistent volumes | ✅ Staging proven 2026-05-23 |
| **D** | Managed DB marketplace | Post-GA |
| **E** | Jobs/Timetable GA polish | Post-GA (API exists) |

### Post-GA (do not block Commercial GA)

Multi-region/edge, managed DB marketplace, full sdk-ts UI migration, PostgreSQL HA, PagerDuty, SOC 2 Type II attestation.

---

## Coupler Program — Agent Tool Plane (Jun–Oct 2026)

**Objective:** Sovereign Composio-class capabilities (delegated SaaS auth, tool execute, MCP, sandbox, triggers) in **`madfam-org/coupler`** (AGPL-3.0) — **not** embedded in Enclii or Janua.

| Track | Target | Blocker |
|-------|--------|---------|
| P0 Bootstrap | 2026-06-13 | Public repo + CI |
| P1 Janua Keyring | 2026-07-25 | ConnectedAccount (ADR-002) |
| P2 Coupler core | 2026-09-05 | Staging execute + 2 connectors |
| P3 Dev surface | 2026-10-03 | MCP + SDK + Selva PoC |
| P4 Parity | 2026-11-14 | Sandbox, triggers, 6 connectors |
| P5 Ecosystem GA | 2026-12-12 | Synthetics, runbooks, announce |

**Docs:** [AGENT_TOOL_PLANE.md](docs/strategy/AGENT_TOOL_PLANE.md) · [COUPLER_REMEDIATION_PLAN.md](docs/strategy/COUPLER_REMEDIATION_PLAN.md) · [COUPLER_EXECUTION_CHECKLIST.md](docs/strategy/COUPLER_EXECUTION_CHECKLIST.md)

**Enclii role:** onboard namespace, ops proxy (`madfam.ops.*`), docs — **no** SaaS connectors in switchyard-api.

**Runs parallel to** Commercial GA Gate 4 SLO window; does not reset Enclii GA clock.

---

## Current State (May 2026)

### Enclii (DevOps Platform) — Production-running beta

Platform foundation is live in production. **Commercial GA** is gated on ops proof, SLO evidence, and GTM publish — not net-new core features.

| Feature | Status | Notes |
|---------|--------|-------|
| Control Plane API | ✅ Production | Switchyard API at api.enclii.dev |
| Web Dashboard | ✅ Production | Next.js UI at app.enclii.dev |
| CLI | ✅ Production | `enclii init/up/deploy/logs` |
| Build Pipeline | ✅ Production | Buildpacks, Dockerfile, Roundhouse |
| GitOps | ✅ Production | ArgoCD App-of-Apps with self-heal |
| Storage | ✅ Production | Longhorn CSI; bet C staging-proven |
| Custom Domains | ✅ Production | Cloudflare for SaaS; bet B staging-proven |
| Preview Environments | ✅ Production | Bet A staging-proven |
| OIDC Authentication | ✅ Production | Janua SSO integration |
| GitHub OAuth | ✅ Production | Repo imports, linked accounts |
| Cost Allocation (Waybill) | ✅ Production | Enforce on deploy/build |
| Billing / throttles UI+CLI | ✅ Production | `/projects/:slug/billing`, `enclii billing throttles` |
| Self-serve signup | 🟡 Staged | Disabled in prod until Wave 2 (O-17) |
| SLA / support / legal | 🟡 Draft | Wave 4 publish |
| Drift Detection | ✅ Production | Admin Console module live |
| UI Resilience | ✅ Production | Error/Loading coverage |


### Janua (Auth Platform) — Production (Auth0 parity)

| Feature | Status | Auth0 Equivalent |
|---------|--------|------------------|
| OAuth 2.0 / OIDC | ✅ Production | ✅ |
| Social Login (8 providers) | ✅ Production | ✅ |
| SAML 2.0 SSO | ✅ Production | ✅ |
| SCIM 2.0 Provisioning | ✅ Production | ✅ |
| Magic Links | ✅ Production | ✅ |
| TOTP MFA | ✅ Production | ✅ |
| WebAuthn/Passkeys | ✅ Production | ✅ |
| Backup Codes | ✅ Production | ✅ |
| Multi-tenant Orgs | ✅ Production | ✅ |
| RBAC | ✅ Production | ✅ |

---

## Q1 2026 (January - March)

### 🔐 Janua: Security Hardening

#### SMS MFA Integration
**Priority:** P1 | **Effort:** 1-2 weeks | **Dependencies:** Twilio/MessageBird account

**Scope:**
- Integration with Twilio Verify API (primary)
- MessageBird fallback for EU compliance
- Rate limiting (5 SMS/user/hour)
- Phone number verification flow
- Configurable per-tenant

**Implementation:**
```python
# apps/api/app/services/sms_mfa_service.py
class SMSMFAService:
    providers = ["twilio", "messagebird"]

    async def send_verification(self, phone: str, code: str):
        # Primary: Twilio Verify
        # Fallback: MessageBird
        # Rate limit: 5/hour per user
```

**Environment Variables:**
```bash
TWILIO_ACCOUNT_SID=
TWILIO_AUTH_TOKEN=
TWILIO_VERIFY_SERVICE_SID=
MESSAGEBIRD_API_KEY=  # Fallback
SMS_MFA_RATE_LIMIT=5  # per hour
```

---

#### Adaptive MFA (Risk-Based Authentication)
**Priority:** P1 | **Effort:** 2-3 weeks | **Dependencies:** Redis, GeoIP database

**Scope:**
- Risk scoring engine (0-100 scale)
- Challenge triggers based on risk signals
- Configurable risk thresholds per tenant

**Risk Signals:**
| Signal | Weight | Description |
|--------|--------|-------------|
| New IP Address | +30 | First login from IP |
| New Device | +25 | Unknown device fingerprint |
| Geographic Anomaly | +40 | Login from new country |
| Impossible Travel | +50 | Login from distant location within impossible timeframe |
| Failed Attempts | +20 | Recent failed login attempts |
| Off-Hours Login | +15 | Login outside normal hours |
| TOR/VPN Exit Node | +35 | Known anonymizer IP |

**Behavior:**
```
Risk Score 0-30:   Normal login (no challenge)
Risk Score 31-60:  Soft challenge (email verification)
Risk Score 61-80:  Hard challenge (MFA required)
Risk Score 81-100: Block + admin notification
```

**Implementation:**
```python
# apps/api/app/services/risk_scoring_service.py
class RiskScoringService:
    async def calculate_risk(self, context: LoginContext) -> int:
        score = 0
        score += self._check_ip_history(context.ip)
        score += self._check_device(context.device_fingerprint)
        score += self._check_geolocation(context.ip, context.user)
        score += self._check_impossible_travel(context)
        return min(score, 100)
```

---

#### Breach Detection (HaveIBeenPwned Integration)
**Priority:** P2 | **Effort:** 1 week | **Dependencies:** HIBP API key

**Scope:**
- Password breach check on registration
- Password breach check on login (optional)
- k-Anonymity API (no passwords sent to HIBP)
- Configurable enforcement (warn vs block)

**Implementation:**
```python
# apps/api/app/services/breach_detection_service.py
class BreachDetectionService:
    HIBP_API = "https://api.pwnedpasswords.com/range/"

    async def check_password(self, password: str) -> BreachResult:
        # Use k-Anonymity (send first 5 chars of SHA1 hash)
        sha1_hash = hashlib.sha1(password.encode()).hexdigest().upper()
        prefix, suffix = sha1_hash[:5], sha1_hash[5:]

        response = await self._query_hibp(prefix)
        return self._check_suffix_in_response(suffix, response)
```

**User Experience:**
- Registration: Block compromised passwords with helpful message
- Login: Warn existing users, suggest password change
- Admin: Dashboard showing breach statistics

---

### 🚀 Enclii: Platform Expansion

#### Cron Jobs / Scheduled Tasks (Timetable)
**Priority:** P1 | **Effort:** 2 weeks

**Scope:**
- Kubernetes CronJob generation from service spec
- Timezone support
- Execution history and logs
- Failure notifications

**Service Spec Addition:**
```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: my-service
spec:
  jobs:
    - name: daily-cleanup
      schedule: "0 3 * * *"  # 3 AM daily
      timezone: "America/New_York"
      command: ["node", "scripts/cleanup.js"]
      timeout: 300  # 5 minutes
      retries: 2
```

---

#### Cost Showback & Budget Alerts (Waybill)
**Priority:** P1 | **Status:** ✅ Beta | **Effort:** 2-3 weeks


**Scope:**
- Per-service resource metering (CPU, memory, storage, bandwidth)
- Per-tenant cost aggregation
- Budget threshold alerts (80%, 100%)
- Stripe billing integration (optional)

**Dashboard Features:**
- Real-time cost visualization
- Cost trends (daily/weekly/monthly)
- Per-project breakdown
- Budget vs actual graphs

---

## Q2 2026 (April - June)

### 🎯 Enclii: Commercial GA execution (priority through July 2026)

All GA work is tracked in [COMMERCIAL_GA_MASTER_PLAN.md](docs/production/COMMERCIAL_GA_MASTER_PLAN.md). Q2 feature work below is **post-GA or parallel non-blocking** unless explicitly pulled into scope.

---

### 🌐 Enclii: "Vercel Killer" Features (post-GA / parallel)

#### Sovereign Serverless (Enclii Functions)
**Priority:** P0 | **Effort:** 4-6 weeks | **See:** Architecture Study below

**Goal:** Serverless function experience without vendor lock-in

**Scope:**
- `functions/` directory convention
- Scale-to-zero pods via KEDA
- Cold start < 500ms target
- Go, Python, Node.js, Rust support
- Edge middleware via Nginx/Lua

#### Multilingual Support (i18n)
**Priority:** P1 | **Status:** 🏗️ In Progress
**Goal:** Spanish-first (default), English, and Portuguese support.
- Hybrid URL strategy (cookie-based + query shareability)
- `next-intl` infrastructure planned
- Blocker: Registry access for `@janua/ui`


---

#### Preview Environment Enhancements
**Priority:** P1 | **Effort:** 2 weeks | **GA note:** Bet A shipped; enhancements post-GA

**Scope:**
- Automatic cleanup (TTL-based)
- Environment cloning
- Branch protection rules integration
- Preview comments on PRs

---

#### Multi-Region Deployments
**Priority:** P2 | **Effort:** 4-6 weeks

**Scope:**
- Region selector in service spec
- Cross-region database replication (PostgreSQL)
- Global load balancing via Cloudflare
- Regional failover automation

---

### 🔐 Janua: Enterprise Features

#### SSO Connections Marketplace
**Priority:** P2 | **Effort:** 3-4 weeks

**Scope:**
- Pre-configured SSO templates:
  - Okta
  - Azure AD
  - Google Workspace
  - OneLogin
  - PingIdentity
- One-click setup wizards
- Automatic metadata exchange

---

#### Session Management Dashboard
**Priority:** P2 | **Effort:** 2 weeks

**Scope:**
- Active session viewer (per user, per org)
- Remote session termination
- Session analytics (device, location, duration)
- Concurrent session limits

---

## Q3-Q4 2026 (Future Roadmap)

### Platform Maturity

| Feature | Quarter | Priority |
|---------|---------|----------|
| SOC 2 Type II Preparation | Q3 | P2 |
| Managed SaaS Option | Q3 | P3 |
| GraphQL API (Janua) | Q3 | P3 |
| Mobile SDKs Polish | Q3 | P2 |
| Enterprise Support Tier | Q4 | P3 |
| AI-Powered Anomaly Detection | Q4 | P3 |

---

## Feature Request Process

1. **Submit:** Open GitHub issue with `[Feature Request]` prefix
2. **Discuss:** Community + maintainer discussion
3. **Prioritize:** Quarterly roadmap review
4. **Implement:** PRs welcome for approved features

---

## Changelog

| Date | Change |
|------|--------|
| 2026-01-15 | Initial roadmap created |
| 2026-01-15 | SMS MFA, Adaptive MFA, Breach Detection added to Q1 |
| 2026-01-15 | Sovereign Serverless study initiated |
| 2026-02-25 | Q1 progress update: ArgoCD remediation (17 apps stable), npm-registry operational, Longhorn single-replica, monitoring exporters deployed. Identity rebranded from "Railway-style PaaS" to "open source DevOps platform". Waybill, Timetable, Junctions, Signal, Lockbox remain planned/unimplemented |
| 2026-05-01 | Platform Hardening Sweep: 100% error/loading boundary coverage across Switchyard and Dispatch. Waybill (Costs) and Drift modules launched in Admin Console. Multilingual (i18n) roadmap established (Spanish-first). |
| 2026-05-09 | Stability Remediation Plan Waves 0 & 1: Created master 5-wave remediation roadmap. Fixed digifab-quoting NestJS probe paths (/health instead of /api/health) and added 5s timeout. Re-enabled Prometheus platform and WAL rules, added custom pgBackRest health alerting rules. Created the reusable Production-Readiness Ratchet reusable CI pipeline and detailed runbooks (F1, F2, F5). |
| 2026-05-14 | Quote-flow verification roadmap added for Selva -> Yantra4D -> Cotiza -> ForgeSight, including Enclii-first operational checks and emergency-only direct production access. |
| 2026-05-22 | **Codebase audit remediation:** Phases 0–2 on `main`; security release checklist pending prod sign-off. |
| 2026-05-29 | **Commercial GA master plan:** [COMMERCIAL_GA_MASTER_PLAN.md](docs/production/COMMERCIAL_GA_MASTER_PLAN.md) — waves 0–4, target announce ~2026-07-14. Supersedes “95% ready” until Gate 5. Staging proofs A/B/C green 2026-05-23. |
| 2026-05-30 | **Coupler Program:** Agent Tool Plane — separate AGPL repo, execution plan + checklist; Janua Keyring blocker documented. |


---

*Roadmap is subject to change based on community feedback and strategic priorities.*  
*Last updated: May 30, 2026*

---

## Cross-Ecosystem Quote Flow Verification (May-June 2026)

Enclii is the default control plane for operating and verifying the Tablaco quote flow. Direct `kubectl`, Helm, or container access is reserved for confirmed incidents or break-glass recovery.

### Scope

- [x] Add a quote-flow doctor that checks Selva worker readiness, Yantra4D project availability, Cotiza quote import readiness, ForgeSight verified pricing readiness, and auth/token presence.
- [ ] Add an authenticated smoke command for the Tablaco quote flow once safe test credentials are available.
- [ ] Surface ExternalSecret health for ForgeSight and other quote-path dependencies.
- [ ] Report whether the flow is client-ready, review-only, blocked by auth, blocked by missing market data, or blocked by unhealthy infrastructure.
- [ ] Store the runbook in ecosystem docs with exact Enclii commands and emergency escalation rules.

### Acceptance Gate

`enclii quote-flow verify --project tablaco --agent selva --require-market-verified` or the equivalent Enclii operation must produce a reproducible pass/fail report without requiring direct production container access.
