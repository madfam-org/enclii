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
        expect(data.windowMinutes).toBe(15);
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

          const authEcosystem = data.services.find(
            (s: { service: string }) => s.service === 'Auth (Ecosystem)'
          );
          if (authEcosystem) {
            expect(authEcosystem.url).toContain('/health');
            expect(authEcosystem.href).toBe('https://auth.madfam.io');
            console.log('Auth (Ecosystem) href correctly set:', authEcosystem.href);
          }
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
        expect(data.windowMinutes).toBe(15);
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
  });
});
