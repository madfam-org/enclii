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

    test('should have API health endpoint', async ({ request }) => {
      const response = await request.get('https://status.enclii.dev/api/health');
      console.log('Health endpoint status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        console.log('Health response:', JSON.stringify(data, null, 2));
      }
    });

    test('should have status API endpoint', async ({ request }) => {
      const response = await request.get('https://status.enclii.dev/api/status');
      console.log('Status API status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        console.log('Status response:', JSON.stringify(data, null, 2));
      }
    });

    test('should have timeline API endpoint', async ({ request }) => {
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

    test('should have API health endpoint', async ({ request }) => {
      const response = await request.get('https://status.madfam.io/api/health');
      console.log('Health endpoint status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        console.log('Health response:', JSON.stringify(data, null, 2));
      }
    });

    test('should have status API endpoint', async ({ request }) => {
      const response = await request.get('https://status.madfam.io/api/status');
      console.log('Status API status:', response.status());

      if (response.ok()) {
        const data = await response.json();
        console.log('Status response:', JSON.stringify(data, null, 2));
      }
    });

    test('should have timeline API endpoint', async ({ request }) => {
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
