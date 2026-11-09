/**
 * Enclii Stress Test
 *
 * Tests API performance under extreme load to find breaking points.
 * Target: 1,000 RPS
 *
 * Usage:
 *   k6 run --env ENCLII_TOKEN=<token> tests/load/stress.js
 *
 * WARNING: Only run against staging/test environments unless authorized.
 */

import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import { CONFIG, STAGES } from './config.js';
import { SUMMARY_TREND_STATS, renderSummary } from './summary.js';

// Custom metrics
const errorRate = new Rate('errors');
const throughput = new Counter('throughput');
const p99Trend = new Trend('custom_p99');

// Stress test stages - ramping up to 1000 RPS
export const options = {
  summaryTrendStats: SUMMARY_TREND_STATS,
  scenarios: {
    stress_test: {
      executor: 'ramping-arrival-rate',
      startRate: 10,
      timeUnit: '1s',
      preAllocatedVUs: 500,
      maxVUs: 2000,
      stages: [
        { duration: '1m', target: 100 },   // Warm up
        { duration: '2m', target: 250 },   // Moderate load
        { duration: '2m', target: 500 },   // High load
        { duration: '3m', target: 1000 },  // Target: 1000 RPS
        { duration: '2m', target: 1000 },  // Sustain peak
        { duration: '1m', target: 0 },     // Cool down
      ],
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<1000', 'p(99)<2000'],  // More lenient for stress
    http_req_failed: ['rate<0.05'],                   // 5% error rate acceptable under stress
    errors: ['rate<0.05'],
  },
};

export function setup() {
  if (!CONFIG.authToken) {
    // For stress test, health endpoint doesn't require auth
    console.log('No token provided - testing unauthenticated endpoints only');
    return { authenticated: false };
  }

  // Validate token
  const res = http.get(`${CONFIG.baseUrl}/api/v1/projects`, {
    headers: { 'Authorization': `Bearer ${CONFIG.authToken}` },
  });

  if (res.status === 401) {
    console.log('Token invalid - testing unauthenticated endpoints only');
    return { authenticated: false };
  }

  console.log('Token valid - testing authenticated endpoints');
  return { authenticated: true, token: CONFIG.authToken };
}

export default function (data) {
  const baseUrl = CONFIG.baseUrl;

  // Mix of endpoints to simulate realistic traffic
  const rand = Math.random();

  if (rand < 0.6) {
    // 60% - Health checks (unauthenticated, fast)
    const res = http.get(`${baseUrl}/health`, {
      tags: { name: 'health', type: 'read' },
    });
    throughput.add(1);
    p99Trend.add(res.timings.duration);

    const success = check(res, {
      'health: status 200': (r) => r.status === 200,
    });
    errorRate.add(!success);

  } else if (rand < 0.9 && data.authenticated) {
    // 30% - List projects (authenticated, medium cost)
    const res = http.get(`${baseUrl}/api/v1/projects`, {
      headers: { 'Authorization': `Bearer ${data.token}` },
      tags: { name: 'projects', type: 'read' },
    });
    throughput.add(1);
    p99Trend.add(res.timings.duration);

    const success = check(res, {
      'projects: status 200': (r) => r.status === 200,
    });
    errorRate.add(!success);

  } else {
    // 10% - Build status (authenticated, light)
    if (data.authenticated) {
      const res = http.get(`${baseUrl}/api/v1/build/status`, {
        headers: { 'Authorization': `Bearer ${data.token}` },
        tags: { name: 'build_status', type: 'read' },
      });
      throughput.add(1);
      p99Trend.add(res.timings.duration);

      check(res, {
        'build_status: valid response': (r) => r.status === 200 || r.status === 404,
      });
    } else {
      // Fallback to health if not authenticated
      const res = http.get(`${baseUrl}/health`, {
        tags: { name: 'health', type: 'read' },
      });
      throughput.add(1);
      p99Trend.add(res.timings.duration);
    }
  }

  // No sleep - maximum throughput for stress test
}

export function handleSummary(data) {
  return renderSummary(data, 'ENCLII STRESS TEST RESULTS', 'stress',
    'Controlled stress observations; achieved generator request rate is not proof of sustained platform capacity.');
}
