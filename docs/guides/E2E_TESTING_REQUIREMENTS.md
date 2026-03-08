# E2E Testing Requirements

**Date**: 2026-01-12
**Status**: Planning
**Priority**: High (required for production confidence)

---

## Overview

This document outlines the automated E2E testing requirements for Enclii's Switchyard UI, based on manual testing performed during the UI/UX modernization effort.

## Testing Stack

### Recommended Tools
- **Playwright** - Primary browser automation (already integrated via MCP)
- **@playwright/test** - Test runner with assertions
- **Allure** or **HTML Reporter** - Test reporting

### Why Playwright
- Cross-browser support (Chromium, Firefox, WebKit)
- Network interception for API mocking
- Screenshot/video capture for debugging
- Accessibility testing built-in
- Already validated during manual testing of this project

---

## Test Categories

### 1. Authentication Tests (Critical)

| Test Case | Description | Priority |
|-----------|-------------|----------|
| `auth/sso-login` | Complete SSO login via Janua | P0 |
| `auth/sso-logout` | RP-initiated logout terminates session | P0 |
| `auth/session-persistence` | Session survives page refresh | P0 |
| `auth/token-refresh` | Silent token refresh before expiry | P1 |
| `auth/unauthorized-redirect` | Protected routes redirect to login | P0 |
| `auth/api-key-auth` | API key authentication for CI/CD | P1 |

**Example Test (Playwright)**:
```typescript
test('SSO login via Janua', async ({ page }) => {
  await page.goto('https://app.enclii.dev/login');

  // Click SSO login button
  await page.getByRole('button', { name: /sign in with janua/i }).click();

  // Complete Janua authentication (may need test credentials)
  await page.waitForURL('https://auth.madfam.io/**');
  await page.fill('[name="email"]', process.env.TEST_USER_EMAIL);
  await page.fill('[name="password"]', process.env.TEST_USER_PASSWORD);
  await page.getByRole('button', { name: /sign in/i }).click();

  // Verify redirect back to dashboard
  await page.waitForURL('https://app.enclii.dev/');
  await expect(page.getByRole('heading', { level: 1 })).toContainText(/Projects/);
});
```

### 2. Dashboard Tests (Project-Centric)

| Test Case | Description | Priority |
|-----------|-------------|----------|
| `dashboard/project-grid` | Project cards load in responsive grid | P0 |
| `dashboard/search-filter` | Search input filters projects by name | P1 |
| `dashboard/sort-dropdown` | Sort dropdown reorders project cards | P1 |
| `dashboard/scope-title` | Page title reflects current scope | P1 |
| `dashboard/skeleton-loading` | Loading state shows card skeletons | P2 |
| `dashboard/empty-state` | Empty state with CTA when no projects | P2 |
| `dashboard/show-more` | Show More button loads additional cards | P2 |

### 3. Theme Tests

| Test Case | Description | Priority |
|-----------|-------------|----------|
| `theme/dark-mode-toggle` | Dark mode applies correct colors | P1 |
| `theme/light-mode-toggle` | Light mode applies correct colors | P1 |
| `theme/system-preference` | Respects system color scheme | P2 |
| `theme/persistence` | Theme choice persists across sessions | P2 |
| `theme/no-flash` | No theme flash on page load | P2 |

**Example Test**:
```typescript
test('dark mode toggle', async ({ page }) => {
  await page.goto('https://app.enclii.dev/');

  // Toggle to dark mode
  await page.getByRole('button', { name: 'Toggle theme' }).click();
  await page.getByRole('menuitem', { name: 'Dark' }).click();

  // Verify dark mode applied
  const html = page.locator('html');
  await expect(html).toHaveClass(/dark/);

  // Verify no white backgrounds on project cards
  const projectCard = page.locator('[class*="Card"]').first();
  await expect(projectCard).not.toHaveCSS('background-color', 'rgb(255, 255, 255)');
});
```

### 4. Responsive Design Tests

| Test Case | Description | Priority |
|-----------|-------------|----------|
| `responsive/mobile-375px` | Layout works at 375px width | P0 |
| `responsive/tablet-768px` | Layout works at 768px width | P1 |
| `responsive/desktop-1280px` | Layout works at 1280px width | P1 |
| `responsive/hamburger-menu` | Mobile hamburger menu opens/closes | P0 |
| `responsive/nav-overflow` | No horizontal overflow on any viewport | P0 |

**Example Test**:
```typescript
test('mobile hamburger menu', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('https://app.enclii.dev/');

  // Hamburger menu should be visible
  const hamburger = page.getByRole('button', { name: /menu/i });
  await expect(hamburger).toBeVisible();

  // Desktop nav should be hidden
  const desktopNav = page.locator('[data-testid="desktop-nav"]');
  await expect(desktopNav).toBeHidden();

  // Open mobile menu
  await hamburger.click();
  const mobileNav = page.locator('[data-testid="mobile-nav"]');
  await expect(mobileNav).toBeVisible();

  // Navigate via mobile menu
  await page.getByRole('link', { name: 'Projects' }).click();
  await page.waitForURL('**/projects');
});
```

### 5. Service Management Tests

| Test Case | Description | Priority |
|-----------|-------------|----------|
| `services/list-view` | Services list displays all services | P0 |
| `services/detail-view` | Service detail page loads | P1 |
| `services/builds-tab` | Build history displays | P1 |
| `services/logs-tab` | Logs stream correctly | P2 |
| `services/deployments-tab` | Deployment history shows | P1 |

### 6. Project Management Tests

| Test Case | Description | Priority |
|-----------|-------------|----------|
| `projects/list-view` | Projects list with cards | P0 |
| `projects/create-project` | Create new project flow | P1 |
| `projects/project-detail` | Project detail page | P1 |
| `projects/environment-switching` | Switch between environments | P1 |

---

## Test Data Requirements

### Test Users
```yaml
test_users:
  admin:
    email: test-admin@madfam.io
    role: admin
    permissions: [all]

  developer:
    email: test-dev@madfam.io
    role: developer
    permissions: [read, deploy]

  viewer:
    email: test-viewer@madfam.io
    role: viewer
    permissions: [read]
```

### Test Projects
- Create dedicated test project(s) in Enclii
- Isolate test data from production data
- Reset test state before each test run

---

## CI/CD Integration

### GitHub Actions Workflow
```yaml
name: E2E Tests

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Setup Node.js
        uses: actions/setup-node@v4
        with:
          node-version: '20'

      - name: Install dependencies
        run: pnpm install

      - name: Install Playwright browsers
        run: pnpm exec playwright install --with-deps

      - name: Run E2E tests
        run: pnpm exec playwright test
        env:
          TEST_USER_EMAIL: ${{ secrets.TEST_USER_EMAIL }}
          TEST_USER_PASSWORD: ${{ secrets.TEST_USER_PASSWORD }}
          BASE_URL: https://app.enclii.dev

      - name: Upload test results
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: playwright-report
          path: playwright-report/
          retention-days: 7
```

---

## Visual Regression Testing

### Recommended Approach
- Use Playwright's built-in screenshot comparison
- Store baseline screenshots in repo
- Compare against baselines on each PR
- Update baselines when intentional UI changes merge

### Key Screenshots
```
screenshots/
├── auth/
│   ├── login-page.png
│   └── login-page-dark.png
├── dashboard/
│   ├── dashboard-desktop.png
│   ├── dashboard-tablet.png
│   ├── dashboard-mobile.png
│   ├── dashboard-dark.png
│   └── dashboard-loading.png
├── services/
│   └── service-detail.png
└── projects/
    └── project-list.png
```

---

## Accessibility Testing

### WCAG 2.1 AA Compliance
- Color contrast ratios (4.5:1 minimum)
- Keyboard navigation
- Screen reader compatibility
- Focus indicators

### Playwright Accessibility Checks
```typescript
test('dashboard accessibility', async ({ page }) => {
  await page.goto('https://app.enclii.dev/');

  const accessibilitySnapshot = await page.accessibility.snapshot();
  expect(accessibilitySnapshot).toBeTruthy();

  // Check for ARIA labels on interactive elements
  const buttons = page.getByRole('button');
  for (const button of await buttons.all()) {
    const name = await button.getAttribute('aria-label') || await button.textContent();
    expect(name).toBeTruthy();
  }
});
```

---

## Implementation Phases

### Phase 1: Foundation (Week 1)
- [ ] Set up Playwright in switchyard-ui
- [ ] Configure test environment variables
- [ ] Create test user accounts in Janua
- [ ] Implement auth tests (SSO login/logout)

### Phase 2: Core Flows (Week 2)
- [ ] Dashboard tests
- [ ] Theme tests
- [ ] Responsive design tests
- [ ] Service list/detail tests

### Phase 3: Coverage Expansion (Week 3)
- [ ] Project management tests
- [ ] Build/deployment tests
- [ ] Visual regression baselines
- [ ] Accessibility tests

### Phase 4: CI Integration (Week 4)
- [ ] GitHub Actions workflow
- [ ] Test reporting
- [ ] PR status checks
- [ ] Documentation

---

## Success Metrics

| Metric | Target |
|--------|--------|
| Test coverage (critical paths) | 100% |
| Test coverage (overall) | > 80% |
| E2E test run time | < 5 minutes |
| Flaky test rate | < 2% |
| Visual regression detection | > 95% |

---

## References

- [Playwright Documentation](https://playwright.dev/docs/intro)
- Enclii development guide: `CLAUDE.md` (repo root)
- [shadcn/ui Documentation](https://ui.shadcn.com/docs)
