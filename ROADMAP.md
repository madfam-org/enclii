# Enclii + Janua Product Roadmap

> **Vision:** The complete self-hosted alternative to Vercel + Auth0
> **Status:** Production-ready foundation, expanding capabilities

---

## Current State (January 2026)

### Enclii (DevOps Platform) - ✅ 95% Production Ready

| Feature | Status | Notes |
|---------|--------|-------|
| Control Plane API | ✅ Production | Switchyard API at api.enclii.dev |
| Web Dashboard | ✅ Production | Next.js UI at app.enclii.dev |
| CLI | ✅ Production | `enclii init/up/deploy/logs` |
| Build Pipeline | ✅ Production | Buildpacks, Dockerfile, Kaniko |
| GitOps | ✅ Production | ArgoCD App-of-Apps with self-heal |
| Storage | ✅ Production | Longhorn CSI (multi-node ready) |
| Custom Domains | ✅ Production | Cloudflare for SaaS (100 free) |
| OIDC Authentication | ✅ Production | Janua SSO integration |
| GitHub OAuth | ✅ Production | Repo imports, linked accounts |
| Cost Allocation (Waybill) | ✅ Beta | Admin Console module live |
| Drift Detection | ✅ Production | Admin Console module live |
| UI Resilience | ✅ Production | 100% Error/Loading coverage |


### Janua (Auth Platform) - ✅ 95% Auth0 Parity

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

### 🌐 Enclii: "Vercel Killer" Features

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
**Priority:** P1 | **Effort:** 2 weeks

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


---

*Roadmap is subject to change based on community feedback and strategic priorities.*
*Last updated: May 1, 2026*
