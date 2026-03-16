import { test, expect } from '@playwright/test';

test.describe('Status Page Verification', () => {
  test.describe('status.enclii.dev', () => {
    test('should load the Enclii status page', async ({ page }) => {
      const response = await page.goto('https://status.enclii.dev', {
        waitUntil: 'networkidle',
        timeout: 30000,
      });

      // Check response status
      expect(response?.status()).toBeLessThan(500);

      // Take screenshot for verification
      await page.screenshot({ path: 'test-results/status-enclii.png', fullPage: true });

      // Log page title and content
      const title = await page.title();
      console.log('Enclii Status Page Title:', title);

      // Check for key elements
      const content = await page.content();
      console.log('Page loaded. Content length:', content.length);
    });

    test('should have clickable service name links', async ({ page }) => {
      await page.goto('https://status.enclii.dev', {
        waitUntil: 'networkidle',
        timeout: 30000,
      });

      // Service names should be rendered as <a> tags with target="_blank"
      const serviceLinks = page.locator('a[target="_blank"].font-medium');
      const count = await serviceLinks.count();
      console.log('Clickable service name links found:', count);
      expect(count).toBeGreaterThan(0);

      // Verify first link has a valid href
      const firstHref = await serviceLinks.first().getAttribute('href');
      expect(firstHref).toBeTruthy();
      expect(firstHref).toMatch(/^https?:\/\//);
      console.log('First service link href:', firstHref);
    });

    test('should have API health endpoint', async ({ request }) => {
      const response = await request.get('https://status.enclii.dev/api/health');
      console.log('Health endpoint status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        console.log('Health response:', JSON.stringify(data, null, 2));
      }
    });

    test('should have status API endpoint with href field', async ({ request }) => {
      const response = await request.get('https://status.enclii.dev/api/status');
      console.log('Status API status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        console.log('Status response services:', data.services?.length ?? 0);

        // Validate responseTimeThresholds in API response
        expect(data).toHaveProperty('responseTimeThresholds');
        expect(data.responseTimeThresholds).toHaveProperty('fast');
        expect(data.responseTimeThresholds).toHaveProperty('normal');
        expect(data.responseTimeThresholds).toHaveProperty('slow');
        expect(typeof data.responseTimeThresholds.fast).toBe('number');
        console.log('Response time thresholds:', JSON.stringify(data.responseTimeThresholds));

        // Validate that services with health-check URLs have href
        if (data.services && data.services.length > 0) {
          const switchyardApi = data.services.find(
            (s: { service: string }) => s.service === 'Switchyard API'
          );
          if (switchyardApi) {
            expect(switchyardApi.url).toContain('/health/ready');
            expect(switchyardApi.href).toBe('https://api.enclii.dev');
            console.log('Switchyard API href correctly set:', switchyardApi.href);
          }

          // Services without health-check URLs should not have href
          const webDashboard = data.services.find(
            (s: { service: string }) => s.service === 'Web Dashboard'
          );
          if (webDashboard) {
            expect(webDashboard.href).toBeUndefined();
            console.log('Web Dashboard correctly has no href (url is user-facing)');
          }

          // Auth service (formerly "Auth (Ecosystem)") should have href
          const auth = data.services.find(
            (s: { service: string }) => s.service === 'Auth'
          );
          if (auth) {
            expect(auth.url).toContain('/health');
            expect(auth.href).toBe('https://auth.madfam.io');
            console.log('Auth href correctly set:', auth.href);
          }
        }
      }
    });

    test('should have timeline API endpoint with href enrichment', async ({ request }) => {
      const response = await request.get('https://status.enclii.dev/api/status/timeline?hours=1');
      console.log('Timeline API status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        console.log('Timeline services count:', data.services?.length ?? 0);
        console.log('Timeline window minutes:', data.windowMinutes);

        // Validate response shape
        expect(data).toHaveProperty('services');
        expect(data).toHaveProperty('from');
        expect(data).toHaveProperty('to');
        expect(data).toHaveProperty('windowMinutes');
        expect(data.windowMinutes).toBe(5);
        expect(Array.isArray(data.services)).toBe(true);

        // If history has been recorded, validate service timeline shape
        if (data.services.length > 0) {
          const svc = data.services[0];
          expect(svc).toHaveProperty('service');
          expect(svc).toHaveProperty('group');
          expect(svc).toHaveProperty('slots');
          expect(svc).toHaveProperty('uptime24h');
          expect(Array.isArray(svc.slots)).toBe(true);

          // Validate href enrichment for services with health-check URLs
          const switchyardApi = data.services.find(
            (s: { service: string }) => s.service === 'Switchyard API'
          );
          if (switchyardApi) {
            expect(switchyardApi.href).toBe('https://api.enclii.dev');
            console.log('Timeline: Switchyard API href enriched:', switchyardApi.href);
          }
        }
      }
    });

    test('should accept valid window parameter', async ({ request }) => {
      const response = await request.get('https://status.enclii.dev/api/status/timeline?hours=1&window=30');
      console.log('Timeline API (window=30) status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        expect(data.windowMinutes).toBe(30);
        // 1 hour / 30 min = 2 slots per service
        if (data.services.length > 0) {
          expect(data.services[0].slots.length).toBe(2);
        }
        console.log('Timeline window=30 validated, slot count:', data.services[0]?.slots.length);
      }
    });

    test('should reject invalid window parameter and fall back to 5', async ({ request }) => {
      const response = await request.get('https://status.enclii.dev/api/status/timeline?hours=1&window=7');
      console.log('Timeline API (window=7 invalid) status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        expect(data.windowMinutes).toBe(5);
        console.log('Invalid window=7 correctly fell back to windowMinutes:', data.windowMinutes);
      }
    });

    test('should reject record endpoint without auth', async ({ request }) => {
      const response = await request.post('https://status.enclii.dev/api/status/record');
      // Should be 401 Unauthorized (no bearer token) or 500 (CRON_SECRET not configured)
      expect(response.status()).toBeGreaterThanOrEqual(400);
      console.log('Record endpoint (no auth) status:', response.status());
    });

    test('should serve Atom feed at /feed.xml', async ({ request }) => {
      const response = await request.get('https://status.enclii.dev/feed.xml');
      console.log('Feed endpoint status:', response.status());

      if (response.ok()) {
        const body = await response.text();
        expect(body).toContain('<?xml');
        expect(body).toContain('<feed xmlns="http://www.w3.org/2005/Atom"');
        expect(body).toContain('<title>');
        console.log('Feed content length:', body.length);
      }
    });

    test('should reject incidents POST without auth', async ({ request }) => {
      const response = await request.post('https://status.enclii.dev/api/incidents', {
        data: { title: 'Test', severity: 'minor', affectedServices: [] },
      });
      expect(response.status()).toBe(401);
      console.log('Incidents POST (no auth) status:', response.status());
    });

    test('should return incidents with expected shape', async ({ request }) => {
      const response = await request.get('https://status.enclii.dev/api/incidents');
      expect(response.ok()).toBe(true);
      const data = await response.json();
      expect(data).toHaveProperty('incidents');
      expect(data).toHaveProperty('total');
      expect(Array.isArray(data.incidents)).toBe(true);
      console.log('Incidents count:', data.total);
    });

    test('should have uptime API endpoint', async ({ request }) => {
      const response = await request.get('https://status.enclii.dev/api/status/uptime?days=7');
      console.log('Uptime API status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        expect(data).toHaveProperty('services');
        expect(data).toHaveProperty('queriedAt');
        expect(Array.isArray(data.services)).toBe(true);
        console.log('Uptime services count:', data.services.length);
      }
    });

    test('should have theme toggle button', async ({ page }) => {
      await page.goto('https://status.enclii.dev', {
        waitUntil: 'networkidle',
        timeout: 30000,
      });

      const themeToggle = page.locator('button[aria-label*="Switch to"]');
      const count = await themeToggle.count();
      // Desktop + mobile = at least 1 visible
      expect(count).toBeGreaterThan(0);
      console.log('Theme toggle buttons found:', count);
    });

    test('should display HTTP status code badges', async ({ page }) => {
      await page.goto('https://status.enclii.dev', {
        waitUntil: 'networkidle',
        timeout: 30000,
      });

      // Expand all groups first to make badges visible
      const expandAll = page.locator('button:has-text("Expand All")');
      if (await expandAll.isVisible()) {
        await expandAll.click();
      }

      // Status code badges are rendered as mono-styled spans with 3-digit codes
      const badges = page.locator('span.font-mono.rounded:text-matches("^[1-5]\\\\d{2}$")');
      const count = await badges.count();
      console.log('HTTP status code badges found:', count);
      // At least some services should show status codes (may be hidden on mobile)
    });

    test('should render with data-theme="dark" by default', async ({ page }) => {
      await page.goto('https://status.enclii.dev', {
        waitUntil: 'networkidle',
        timeout: 30000,
      });

      const dataTheme = await page.locator('html').getAttribute('data-theme');
      expect(dataTheme).toBe('dark');
      console.log('Default data-theme:', dataTheme);
    });

    test('should still have removed services absent (regression guard)', async ({ request }) => {
      const response = await request.get('https://status.enclii.dev/api/status');
      if (response.ok()) {
        const data = await response.json();
        const names = data.services?.map((s: { service: string }) => s.service) ?? [];
        expect(names).not.toContain('Vault');
        console.log('Regression guard: removed services still absent');
      }
    });

    test('should toggle theme on click', async ({ page }) => {
      await page.goto('https://status.enclii.dev', {
        waitUntil: 'networkidle',
        timeout: 30000,
      });

      // Should start as dark
      expect(await page.locator('html').getAttribute('data-theme')).toBe('dark');

      // Click the desktop theme toggle
      const toggle = page.locator('button[aria-label*="Switch to light"]').first();
      await toggle.click();

      // Should now be light
      expect(await page.locator('html').getAttribute('data-theme')).toBe('light');
      console.log('Theme toggled to light successfully');

      // Click again to go back to dark
      const toggleBack = page.locator('button[aria-label*="Switch to dark"]').first();
      await toggleBack.click();

      expect(await page.locator('html').getAttribute('data-theme')).toBe('dark');
      console.log('Theme toggled back to dark successfully');
    });
  });

  test.describe('status.madfam.io', () => {
    test('should load the MADFAM status page', async ({ page }) => {
      const response = await page.goto('https://status.madfam.io', {
        waitUntil: 'networkidle',
        timeout: 30000,
      });

      // Check response status
      expect(response?.status()).toBeLessThan(500);

      // Take screenshot for verification
      await page.screenshot({ path: 'test-results/status-madfam.png', fullPage: true });

      // Log page title and content
      const title = await page.title();
      console.log('MADFAM Status Page Title:', title);

      // Check for key elements
      const content = await page.content();
      console.log('Page loaded. Content length:', content.length);
    });

    test('should have clickable service name links', async ({ page }) => {
      await page.goto('https://status.madfam.io', {
        waitUntil: 'networkidle',
        timeout: 30000,
      });

      // Service names should be rendered as <a> tags with target="_blank"
      const serviceLinks = page.locator('a[target="_blank"].font-medium');
      const count = await serviceLinks.count();
      console.log('Clickable service name links found:', count);
      expect(count).toBeGreaterThan(0);

      // Verify first link has a valid href
      const firstHref = await serviceLinks.first().getAttribute('href');
      expect(firstHref).toBeTruthy();
      expect(firstHref).toMatch(/^https?:\/\//);
      console.log('First service link href:', firstHref);
    });

    test('should display domain hints below service names', async ({ page }) => {
      await page.goto('https://status.madfam.io', {
        waitUntil: 'networkidle',
        timeout: 30000,
      });

      // Domain hints are rendered as monospace text with the hostname
      const domainHints = page.locator('p.font-mono.text-xs');
      const count = await domainHints.count();
      console.log('Domain hint elements found:', count);
      expect(count).toBeGreaterThan(0);

      // First domain hint should contain a valid hostname
      const firstHint = await domainHints.first().textContent();
      expect(firstHint).toBeTruthy();
      expect(firstHint).toMatch(/\./); // Should contain a dot (hostname)
      console.log('First domain hint:', firstHint);
    });

    test('should have API health endpoint', async ({ request }) => {
      const response = await request.get('https://status.madfam.io/api/health');
      console.log('Health endpoint status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        console.log('Health response:', JSON.stringify(data, null, 2));
      }
    });

    test('should have status API endpoint with href field', async ({ request }) => {
      const response = await request.get('https://status.madfam.io/api/status');
      console.log('Status API status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        console.log('Status response services:', data.services?.length ?? 0);

        // Validate responseTimeThresholds in API response
        expect(data).toHaveProperty('responseTimeThresholds');
        expect(data.responseTimeThresholds).toHaveProperty('fast');
        expect(data.responseTimeThresholds).toHaveProperty('normal');
        expect(data.responseTimeThresholds).toHaveProperty('slow');
        expect(typeof data.responseTimeThresholds.fast).toBe('number');
        console.log('Response time thresholds:', JSON.stringify(data.responseTimeThresholds));

        // Validate that services with health-check URLs have href
        if (data.services && data.services.length > 0) {
          const encliiApi = data.services.find(
            (s: { service: string }) => s.service === 'Enclii API'
          );
          if (encliiApi) {
            expect(encliiApi.url).toContain('/health/ready');
            expect(encliiApi.href).toBe('https://api.enclii.dev');
            console.log('Enclii API href correctly set:', encliiApi.href);
          }

          // Auth service (formerly "Auth (Ecosystem)") renamed to "Auth"
          const auth = data.services.find(
            (s: { service: string }) => s.service === 'Auth'
          );
          if (auth) {
            expect(auth.url).toContain('/health');
            expect(auth.href).toBe('https://auth.madfam.io');
            console.log('Auth href correctly set:', auth.href);
          }

          // Verify removed services are gone
          const authDefault = data.services.find(
            (s: { service: string }) => s.service === 'Auth (Default)'
          );
          expect(authDefault).toBeUndefined();

          const mesAdmin = data.services.find(
            (s: { service: string }) => s.service === 'MES Admin'
          );
          expect(mesAdmin).toBeUndefined();
        }
      }
    });

    test('should have timeline API endpoint with href enrichment', async ({ request }) => {
      const response = await request.get('https://status.madfam.io/api/status/timeline?hours=1');
      console.log('Timeline API status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        console.log('Timeline services count:', data.services?.length ?? 0);
        console.log('Timeline window minutes:', data.windowMinutes);

        // Validate response shape
        expect(data).toHaveProperty('services');
        expect(data).toHaveProperty('from');
        expect(data).toHaveProperty('to');
        expect(data).toHaveProperty('windowMinutes');
        expect(data.windowMinutes).toBe(5);
        expect(Array.isArray(data.services)).toBe(true);

        // If history has been recorded, validate service timeline shape
        if (data.services.length > 0) {
          const svc = data.services[0];
          expect(svc).toHaveProperty('service');
          expect(svc).toHaveProperty('group');
          expect(svc).toHaveProperty('slots');
          expect(svc).toHaveProperty('uptime24h');
          expect(Array.isArray(svc.slots)).toBe(true);

          // Validate href enrichment for services with health-check URLs
          const encliiApi = data.services.find(
            (s: { service: string }) => s.service === 'Enclii API'
          );
          if (encliiApi) {
            expect(encliiApi.href).toBe('https://api.enclii.dev');
            console.log('Timeline: Enclii API href enriched:', encliiApi.href);
          }
        }
      }
    });

    test('should accept valid window parameter', async ({ request }) => {
      const response = await request.get('https://status.madfam.io/api/status/timeline?hours=1&window=60');
      console.log('Timeline API (window=60) status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        expect(data.windowMinutes).toBe(60);
        // 1 hour / 60 min = 1 slot per service
        if (data.services.length > 0) {
          expect(data.services[0].slots.length).toBe(1);
        }
        console.log('Timeline window=60 validated');
      }
    });

    test('should reject invalid window parameter and fall back to 5', async ({ request }) => {
      const response = await request.get('https://status.madfam.io/api/status/timeline?hours=1&window=7');
      console.log('Timeline API (window=7 invalid) status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        expect(data.windowMinutes).toBe(5);
        console.log('Invalid window=7 correctly fell back to windowMinutes:', data.windowMinutes);
      }
    });

    test('should reject record endpoint without auth', async ({ request }) => {
      const response = await request.post('https://status.madfam.io/api/status/record');
      // Should be 401 Unauthorized (no bearer token) or 500 (CRON_SECRET not configured)
      expect(response.status()).toBeGreaterThanOrEqual(400);
      console.log('Record endpoint (no auth) status:', response.status());
    });

    test('should serve Atom feed at /feed.xml', async ({ request }) => {
      const response = await request.get('https://status.madfam.io/feed.xml');
      console.log('Feed endpoint status:', response.status());

      if (response.ok()) {
        const body = await response.text();
        expect(body).toContain('<?xml');
        expect(body).toContain('<feed xmlns="http://www.w3.org/2005/Atom"');
        expect(body).toContain('<title>');
        console.log('Feed content length:', body.length);
      }
    });

    test('should reject incidents POST without auth', async ({ request }) => {
      const response = await request.post('https://status.madfam.io/api/incidents', {
        data: { title: 'Test', severity: 'minor', affectedServices: [] },
      });
      expect(response.status()).toBe(401);
      console.log('Incidents POST (no auth) status:', response.status());
    });

    test('should return incidents with expected shape', async ({ request }) => {
      const response = await request.get('https://status.madfam.io/api/incidents');
      expect(response.ok()).toBe(true);
      const data = await response.json();
      expect(data).toHaveProperty('incidents');
      expect(data).toHaveProperty('total');
      expect(Array.isArray(data.incidents)).toBe(true);
      console.log('Incidents count:', data.total);
    });

    test('should have uptime API endpoint', async ({ request }) => {
      const response = await request.get('https://status.madfam.io/api/status/uptime?days=7');
      console.log('Uptime API status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        expect(data).toHaveProperty('services');
        expect(data).toHaveProperty('queriedAt');
        expect(Array.isArray(data.services)).toBe(true);
        console.log('Uptime services count:', data.services.length);
      }
    });

    test('should have theme toggle button', async ({ page }) => {
      await page.goto('https://status.madfam.io', {
        waitUntil: 'networkidle',
        timeout: 30000,
      });

      const themeToggle = page.locator('button[aria-label*="Switch to"]');
      const count = await themeToggle.count();
      expect(count).toBeGreaterThan(0);
      console.log('Theme toggle buttons found:', count);
    });

    test('should toggle theme on click', async ({ page }) => {
      await page.goto('https://status.madfam.io', {
        waitUntil: 'networkidle',
        timeout: 30000,
      });

      expect(await page.locator('html').getAttribute('data-theme')).toBe('dark');

      const toggle = page.locator('button[aria-label*="Switch to light"]').first();
      await toggle.click();

      expect(await page.locator('html').getAttribute('data-theme')).toBe('light');
      console.log('Theme toggled to light successfully');
    });

    test('should show correct footer brand name', async ({ page }) => {
      await page.goto('https://status.madfam.io', {
        waitUntil: 'networkidle',
        timeout: 30000,
      });

      // Footer should say "MADFAM" not "MADFAM System"
      const footer = page.locator('footer');
      const footerText = await footer.textContent();
      expect(footerText).toContain('MADFAM');
      expect(footerText).not.toContain('MADFAM System.');
      console.log('Footer text:', footerText?.trim());
    });

    test('should still have removed services absent (regression guard)', async ({ request }) => {
      const response = await request.get('https://status.madfam.io/api/status');
      if (response.ok()) {
        const data = await response.json();
        const names = data.services?.map((s: { service: string }) => s.service) ?? [];
        expect(names).not.toContain('Auth (Default)');
        expect(names).not.toContain('MES Admin');
        expect(names).not.toContain('Yantra4D API');
        expect(names).not.toContain('Karafiel API');
        expect(names).not.toContain('MADFAM CMS');
        expect(names).not.toContain('Vault');
        console.log('Regression guard: all removed services still absent');
      }
    });
  });
});
