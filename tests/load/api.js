/**
 * Enclii API Load Test
 *
 * Tests authenticated API endpoints under load.
 * Requires a valid API token.
 *
 * Usage:
 *   k6 run --env ENCLII_TOKEN=<token> tests/load/api.js
 *   k6 run --env ENCLII_TOKEN=<token> --env TARGET_RPS=1000 tests/load/api.js
 *
 * Get a token:
 *   enclii login && cat ~/.enclii/token
 */

import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import { CONFIG, STAGES } from './config.js';
import { SUMMARY_TREND_STATS, renderSummary } from './summary.js';

// Custom metrics
const apiErrors = new Rate('api_errors');
const apiSloBreaches = new Rate('api_slo_breaches');
const projectsLatency = new Trend('projects_list_latency');
const projectsCount = new Counter('projects_requests');

// Test configuration
export const options = {
  summaryTrendStats: SUMMARY_TREND_STATS,
  scenarios: {
    api_load: {
      executor: 'ramping-vus',
      stages: STAGES.load,
      gracefulRampDown: '30s',
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
    http_req_failed: ['rate<0.01'],
    api_errors: ['rate<0.01'],
    api_slo_breaches: ['rate<0.01'],
    projects_list_latency: ['p(95)<500'],
  },
};

// Validate token at start
export function setup() {
  if (!CONFIG.authToken) {
    console.error('ERROR: ENCLII_TOKEN environment variable not set');
    console.error('Get a token: enclii login && cat ~/.enclii/token');
    throw new Error('Missing authentication token');
  }

  // Validate token works
  const res = http.get(`${CONFIG.baseUrl}/api/v1/projects`, {
    headers: { 'Authorization': `Bearer ${CONFIG.authToken}` },
  });

  if (res.status === 401) {
    throw new Error('Invalid or expired token. Please re-authenticate.');
  }

  console.log(`Authenticated successfully. Running against ${CONFIG.baseUrl}`);
  return { token: CONFIG.authToken };
}

export default function (data) {
  const headers = {
    'Authorization': `Bearer ${data.token}`,
    'Content-Type': 'application/json',
  };

  group('Projects API', function () {
    // List projects
    const listRes = http.get(`${CONFIG.baseUrl}/api/v1/projects`, {
      headers,
      tags: { name: 'list_projects' },
    });

    projectsLatency.add(listRes.timings.duration);
    projectsCount.add(1);

    const listSuccess = check(listRes, {
      'list projects - status 200': (r) => r.status === 200,
      'list projects - valid JSON': (r) => {
        try {
          JSON.parse(r.body);
          return true;
        } catch {
          return false;
        }
      },
    });

    const latencySuccess = check(listRes, { 'list projects - response time < 500ms': (r) => r.timings.duration < 500 });
    apiErrors.add(!listSuccess);
    // Preserve the original union gate: one breach for any availability or
    // pointwise latency failure, including disjoint failures across requests.
    apiSloBreaches.add(!(listSuccess && latencySuccess));
  });

  group('Health Endpoints', function () {
    // Health check (baseline)
    const healthRes = http.get(`${CONFIG.baseUrl}/health`, {
      tags: { name: 'health' },
    });

    check(healthRes, {
      'health - status 200': (r) => r.status === 200,
    });

    // Build status (authenticated)
    const buildStatusRes = http.get(`${CONFIG.baseUrl}/api/v1/build/status`, {
      headers,
      tags: { name: 'build_status' },
    });

    check(buildStatusRes, {
      'build status - status 200 or 404': (r) => r.status === 200 || r.status === 404,
    });
  });

  sleep(Math.random() * 0.5 + 0.5);  // 500-1000ms between iterations
}

export function handleSummary(data) {
  return renderSummary(data, 'ENCLII AUTHENTICATED API LOAD RESULTS', 'api',
    'Authenticated API workload observations; public/deep-health requests are separate checks, not full estate readiness.');
}
