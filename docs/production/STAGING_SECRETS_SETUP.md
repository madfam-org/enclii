# Staging secrets setup — Commercial GA E2E

> **Use with:** [COMMERCIAL_GA_STAGING_PROOF.md](./COMMERCIAL_GA_STAGING_PROOF.md) · workflow `commercial-ga-staging-proof.yml`

Configure secrets **once** before running lifecycle proofs (bets A/B/C).

---

## 1. Create staging credentials

| Step | Action |
|------|--------|
| 1 | Log in to staging/production Enclii as a **developer** on a throwaway project |
| 2 | Create API token: **Settings → Tokens** (or `enclii tokens create`) |
| 3 | Note **service UUIDs** and **environment UUID** for domain proof |
| 4 | For storage deploy proof: note a **ready release UUID** on the test service |

Do not use production-critical customer services or platform `enclii/*` services (deploy returns 403). Prefer an internal project with a **ready**, **cosign-signed** release and **`development`** environment (no PR-approval gate). Example: `blueprint-harvester/blueprint-harvester-api` with `STORAGE_E2E_ENVIRONMENT_NAME=development`. Avoid Next.js frontends whose default probe path is `/` not `/health` unless health is configured.

---

## 2. GitHub repository secrets

**Settings → Secrets and variables → Actions → Repository secrets**

| Secret name | Required for | Example source |
|-------------|--------------|----------------|
| `PREVIEW_E2E_TOKEN` | Bet A | API token |
| `PREVIEW_E2E_SERVICE_ID` | Bet A | Service UUID |
| `DOMAIN_E2E_TOKEN` | Bet B | Same or separate token |
| `DOMAIN_E2E_SERVICE_ID` | Bet B | Service UUID |
| `DOMAIN_E2E_ENVIRONMENT_ID` | Bet B | Environment UUID |
| `DOMAIN_E2E_DOMAIN` | Bet B optional | FQDN you control |
| `STORAGE_E2E_TOKEN` | Bet C | API token |
| `STORAGE_E2E_SERVICE_ID` | Bet C | Service UUID |
| `STORAGE_E2E_RELEASE_ID` | Bet C deploy slice | Ready release UUID |
| `STORAGE_E2E_ENVIRONMENT_NAME` | Bet C deploy slice | **`development`** (avoids production PR-approval gate; use throwaway service on internal project) |

Create GitHub environment **`commercial-ga-staging`** and add secrets (environment shell can be bootstrapped with `./scripts/setup-commercial-ga-staging-env.sh`):

```bash
./scripts/setup-commercial-ga-staging-env.sh
gh secret set PREVIEW_E2E_TOKEN --env commercial-ga-staging
# ... see table below
```

---

## 3. Run proofs

**Actions:** **Commercial GA staging proof** → Run workflow → `bets: all`

**Local:** export secrets, then:

```bash
cd tests/e2e-ecosystem
npx playwright test --project=preview-lifecycle
npx playwright test --project=domains-lifecycle
npx playwright test --project=storage-smoke
```

---

## 4. Record results

Update [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md) bet rows with date + operator initials when each bet passes.

---

## Troubleshooting

| Issue | Fix |
|-------|-----|
| All opt-in tests skipped | Secret names must match exactly (case-sensitive) |
| 403 on deploy | Budget throttle or approval policy — check `enclii billing throttles --active` |
| Domain verify fails | DNS not propagated; use `DOMAIN_E2E_DOMAIN` only when TXT/CNAME are live |
| Storage deploy 422 | Prior failed deploy for same release+environment — test rotates ready releases; pick a service with signed images and working `/health` probes |
