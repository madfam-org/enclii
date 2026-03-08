# E2E Test Users Setup Guide

**Last Updated:** 2026-01-13
**Status:** Required for authenticated E2E tests in CI

---

## Overview

Enclii's E2E tests require authenticated users to test protected routes and functionality. This guide covers setting up test users in Janua SSO and configuring CI secrets.

### Test User Roles

| Email | Role | Purpose |
|-------|------|---------|
| test-admin@madfam.io | Admin | Test admin functionality, user management |
| test-dev@madfam.io | Developer | Test standard developer workflows |
| test-viewer@madfam.io | Viewer | Test read-only access patterns |

---

## Step 1: Create Users in Janua

### Via Janua Admin UI

1. Navigate to https://auth.madfam.io/admin
2. Log in with admin credentials
3. Go to **Users** → **Create User**

For each test user:

```yaml
# test-admin@madfam.io
Email: test-admin@madfam.io
Name: Test Admin
Role: admin
Organization: enclii-test
Enabled: true

# test-dev@madfam.io
Email: test-dev@madfam.io
Name: Test Developer
Role: developer
Organization: enclii-test
Enabled: true

# test-viewer@madfam.io
Email: test-viewer@madfam.io
Name: Test Viewer
Role: viewer
Organization: enclii-test
Enabled: true
```

4. Set secure passwords for each user
5. Store passwords securely (1Password, Vault, etc.)

### Via Janua CLI (if available)

```bash
# Create test users
janua user create --email test-admin@madfam.io --name "Test Admin" --role admin --org enclii-test
janua user create --email test-dev@madfam.io --name "Test Developer" --role developer --org enclii-test
janua user create --email test-viewer@madfam.io --name "Test Viewer" --role viewer --org enclii-test

# Set passwords
janua user set-password test-admin@madfam.io
janua user set-password test-dev@madfam.io
janua user set-password test-viewer@madfam.io
```

---

## Step 2: Configure GitHub Actions Secrets

Add the following secrets to your GitHub repository:

1. Go to **Settings** → **Secrets and variables** → **Actions**
2. Add new repository secrets:

| Secret Name | Value |
|-------------|-------|
| `TEST_USER_EMAIL` | `test-dev@madfam.io` |
| `TEST_USER_PASSWORD` | `<secure-password>` |
| `TEST_ADMIN_EMAIL` | `test-admin@madfam.io` |
| `TEST_ADMIN_PASSWORD` | `<secure-password>` |

### Via GitHub CLI

```bash
# Set secrets
gh secret set TEST_USER_EMAIL --body "test-dev@madfam.io"
gh secret set TEST_USER_PASSWORD --body "<secure-password>"
gh secret set TEST_ADMIN_EMAIL --body "test-admin@madfam.io"
gh secret set TEST_ADMIN_PASSWORD --body "<secure-password>"
```

---

## Step 3: Update E2E Workflow

The `.github/workflows/e2e-tests.yml` already supports these environment variables:

```yaml
env:
  TEST_USER_EMAIL: ${{ secrets.TEST_USER_EMAIL }}
  TEST_USER_PASSWORD: ${{ secrets.TEST_USER_PASSWORD }}
```

Once secrets are configured, authenticated tests will automatically run instead of being skipped.

---

## Step 4: Local Testing

For local E2E testing with authentication:

1. Create `.env.test.local` (gitignored):

```bash
# .env.test.local - Local E2E test credentials
# DO NOT COMMIT THIS FILE

TEST_USER_EMAIL=test-dev@madfam.io
TEST_USER_PASSWORD=your-test-password

# Optional: Admin credentials for admin-specific tests
TEST_ADMIN_EMAIL=test-admin@madfam.io
TEST_ADMIN_PASSWORD=your-admin-password

# Base URL for E2E tests
BASE_URL=http://localhost:3000
```

2. Run tests with credentials:

```bash
# Load env and run tests
source .env.test.local && pnpm test:e2e

# Or use dotenv
pnpm dotenv -e .env.test.local -- pnpm test:e2e
```

---

## Test User Isolation

### Best Practices

1. **Dedicated test organization**: Create `enclii-test` org in Janua
2. **Test data isolation**: Tests should create/cleanup their own data
3. **Unique identifiers**: Use timestamps or UUIDs in test data names
4. **Cleanup hooks**: Implement afterEach/afterAll cleanup

### Example Test Isolation

```typescript
// e2e/utils/test-data.ts
export function generateTestProject() {
  return {
    name: `test-project-${Date.now()}`,
    description: 'E2E test project - safe to delete',
  };
}

// In test
test('create project', async ({ page }) => {
  const project = generateTestProject();

  // Create
  await page.goto('/projects/new');
  await page.fill('[name="name"]', project.name);
  await page.click('button[type="submit"]');

  // Verify
  await expect(page.getByText(project.name)).toBeVisible();

  // Cleanup happens automatically via test org reset
});
```

---

## Security Considerations

### Password Requirements

Test user passwords should:
- Be unique (not used elsewhere)
- Meet Janua password policy
- Be rotated quarterly
- Be stored in secure secret manager

### Access Controls

- Test users have minimal required permissions
- Test org is isolated from production data
- Test users cannot access production resources
- Audit logs track test user activity

### CI Security

- Secrets are encrypted at rest in GitHub
- Secrets are masked in workflow logs
- Fork PRs do not have access to secrets
- Regular secret rotation recommended

---

## Troubleshooting

### Tests Skipping Authentication

**Symptom:** Tests log "TEST_USER_PASSWORD not set - skipping authentication"

**Solution:**
1. Verify secrets are set in GitHub Actions
2. Check workflow passes secrets to job environment
3. For local: ensure `.env.test.local` is sourced

### Login Fails in CI

**Symptom:** SSO redirect hangs or times out

**Possible causes:**
1. Janua not accessible from GitHub Actions
2. Test user account locked
3. Password expired

**Debug steps:**
```bash
# Check Janua accessibility
curl -I https://auth.madfam.io/health

# Verify user exists and is enabled in Janua admin
```

### Session Not Persisting

**Symptom:** Tests log in but subsequent pages require re-auth

**Solution:**
- Ensure Playwright preserves storage state
- Check cookie settings in Janua client config

---

## Verification Checklist

Before relying on authenticated E2E tests:

- [ ] Test users created in Janua
- [ ] Users assigned to correct roles
- [ ] Users in `enclii-test` organization
- [ ] Passwords set and documented securely
- [ ] GitHub Actions secrets configured
- [ ] Local `.env.test.local` created (for dev)
- [ ] Run E2E tests locally with auth
- [ ] Verify CI runs authenticated tests
- [ ] Document password rotation schedule

---

## Related Documentation

- [E2E Testing Overview](./E2E_TESTING_REQUIREMENTS.md)
- [SSO Deployment](./sso-deployment.md) — Janua SSO integration
