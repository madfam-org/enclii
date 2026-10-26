/**
 * Enclii public liveness smoke / explicit deep-health diagnostic
 *
 * The default low-load smoke checks only dependency-free /health/public.
 * Deep /health calls live dependencies and requires explicit operator opt-in.
 *
 * Usage:
 *   k6 run tests/load/health.js
 *   k6 run --env ENCLII_BASE_URL=http://localhost:8080 tests/load/health.js
 */

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import { CONFIG, STAGES } from './config.js';
import { healthProfile, healthyResponse } from './health-contract.js';
import { SUMMARY_TREND_STATS, renderSummary } from './summary.js';

const profile = healthProfile(__ENV, STAGES);

// Custom metrics
const healthErrors = new Rate('health_errors');
const healthSloBreaches = new Rate('health_slo_breaches');
const healthDuration = new Trend('health_duration');

// Test configuration
export const options = {
  summaryTrendStats: SUMMARY_TREND_STATS,
  scenarios: {
    health_check: {
      executor: 'ramping-vus',
      stages: profile.stages,
      gracefulRampDown: '30s',
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<200', 'p(99)<500'],  // Health should be fast
    http_req_failed: ['rate<0.001'],                // 0.1% error rate
    health_errors: ['rate<0.001'],
    health_slo_breaches: ['rate<0.001'],
    health_duration: ['p(95)<200'],
  },
};

export default function () {
  const url = `${CONFIG.baseUrl}${profile.path}`;

  const res = http.get(url, {
    tags: { name: 'health_check' },
  });

  // Record custom metrics
  healthDuration.add(res.timings.duration);

  const success = check(res, {
    'status is 200': (r) => r.status === 200,
    'body matches health contract': (r) => healthyResponse(r, profile.path === '/health'),
  });
  const latencySuccess = check(res, { 'response time < 200ms': (r) => r.timings.duration < 200 });
  healthErrors.add(!success);
  // Count the union once per request, as the original combined check did.
  healthSloBreaches.add(!(success && latencySuccess));

  sleep(0.1);  // 100ms between requests per VU
}

export function handleSummary(data) {
  return renderSummary(data, `ENCLII ${profile.name.toUpperCase()}`, 'health',
    profile.path === '/health' ? 'Deep dependency health under controlled load; does not locate a latency bottleneck.' :
      'Anonymous public liveness at low load; does not establish dependency readiness or API capacity.');
}
