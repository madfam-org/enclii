const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');

const directory = __dirname;
const clone = (value) => JSON.parse(JSON.stringify(value));
const publicBody = { ok: true, service: 'switchyard-api', version: 'fixture', time: 'fixture' };
const response = (body = publicBody, duration = 100, status = 200) => ({
  status, body: typeof body === 'string' ? body : JSON.stringify(body), timings: { duration },
});

// Evaluate the real ES modules. Only synthetic k6 adapters are linkable; no
// socket, process, require, provider SDK, or production request is available.
async function script(filename, env = {}, replies = []) {
  const samples = new Map();
  const requests = [];
  const checks = [];
  const math = Object.create(Math);
  math.random = () => 0.1;
  const context = vm.createContext({
    __ENV: { ENCLII_BASE_URL: 'https://offline.example.test', ...env },
    console: { log() {}, error() {} }, Math: math,
    fetch() { throw new Error('Network is forbidden in offline fixtures'); },
  });
  class Metric {
    constructor(name) { this.name = name; samples.set(name, []); }
    add(value) { samples.get(this.name).push(value); }
  }
  const stubs = {
    'k6/http': { default: { get(url, options) {
      requests.push({ url, options });
      assert.ok(replies.length, 'unexpected request in synthetic workload');
      return replies.shift();
    } } },
    k6: {
      check(value, predicates) {
        const outcomes = Object.entries(predicates).map(([name, predicate]) => {
          const ok = predicate(value); checks.push({ name, ok }); return ok;
        });
        return outcomes.every(Boolean);
      },
      sleep() {}, group(_name, work) { work(); },
    },
    'k6/metrics': { Rate: Metric, Trend: Metric, Counter: Metric },
  };
  const modules = new Map();
  function load(identifier) {
    if (modules.has(identifier)) return modules.get(identifier);
    let module;
    if (stubs[identifier]) {
      const exports = stubs[identifier];
      module = new vm.SyntheticModule(Object.keys(exports), function () {
        for (const [name, value] of Object.entries(exports)) this.setExport(name, value);
      }, { context, identifier });
    } else {
      assert.equal(path.dirname(identifier), directory, 'only reviewed load modules may be imported');
      module = new vm.SourceTextModule(fs.readFileSync(identifier, 'utf8'), { context, identifier });
    }
    modules.set(identifier, module);
    return module;
  }
  const module = load(path.join(directory, filename));
  await module.link((name, referencing) => load(name.startsWith('.') ?
    path.resolve(path.dirname(referencing.identifier), name) : name));
  await module.evaluate();
  return { exports: module.namespace, samples, requests, checks };
}

test('default smoke is public liveness with the existing two-minute/five-user profile', async () => {
  const run = await script('health.js', {}, [response()]);
  assert.deepEqual(clone(run.exports.options.scenarios.health_check.stages), [
    { duration: '30s', target: 5 }, { duration: '1m', target: 5 }, { duration: '30s', target: 0 },
  ]);
  run.exports.default();
  assert.equal(run.requests[0].url, 'https://offline.example.test/health/public');
  assert.deepEqual(run.samples.get('health_errors'), [false]);
});

for (const env of [
  { HEALTH_PROFILE: 'deep-diagnostic' },
  { HEALTH_PROFILE: 'deep-diagnostic', ENCLII_ALLOW_DEEP_HEALTH_DIAGNOSTIC: 'false' },
  { HEALTH_PROFILE: 'unknown', ENCLII_ALLOW_DEEP_HEALTH_DIAGNOSTIC: 'true' },
]) {
  test(`invalid or unapproved diagnostic profile refuses before any HTTP: ${JSON.stringify(env)}`, async () => {
    await assert.rejects(script('health.js', env), /explicit ENCLII_ALLOW_DEEP_HEALTH_DIAGNOSTIC=true/);
  });
}

test('explicit deep diagnostic retains the old load profile and real deep-health body contract', async () => {
  const run = await script('health.js', { HEALTH_PROFILE: 'deep-diagnostic', ENCLII_ALLOW_DEEP_HEALTH_DIAGNOSTIC: 'true' },
    [response({ status: 'healthy', service: 'switchyard-api' })]);
  assert.deepEqual(clone(run.exports.options.scenarios.health_check.stages), [
    { duration: '2m', target: 50 }, { duration: '5m', target: 50 }, { duration: '2m', target: 100 },
    { duration: '5m', target: 100 }, { duration: '2m', target: 0 },
  ]);
  run.exports.default();
  assert.equal(run.requests[0].url, 'https://offline.example.test/health');
  assert.deepEqual(run.samples.get('health_errors'), [false]);
});

for (const body of [{ status: 'unhealthy', service: 'switchyard-api' }, publicBody]) {
  test(`deep diagnostic does not accept unhealthy or liveness-only body: ${JSON.stringify(body)}`, async () => {
    const run = await script('health.js', { HEALTH_PROFILE: 'deep-diagnostic', ENCLII_ALLOW_DEEP_HEALTH_DIAGNOSTIC: 'true' }, [response(body)]);
    run.exports.default();
    assert.deepEqual(run.samples.get('health_errors'), [true]);
  });
}

for (const [name, reply] of [
  ['non-200', response(publicBody, 100, 503)], ['malformed JSON', response('not-json')],
  ['wrong service', response({ ok: true, service: 'another-service' })],
  ['false liveness', response({ ok: false, service: 'switchyard-api' })],
  ['string liveness', response({ ok: 'true', service: 'switchyard-api' })],
  ['deep response on public endpoint', response({ status: 'healthy', service: 'switchyard-api' })],
]) {
  test(`smoke availability fails on ${name}`, async () => {
    const run = await script('health.js', {}, [reply]); run.exports.default();
    assert.deepEqual(run.samples.get('health_errors'), [true]);
  });
}

test('slow healthy smoke remains a failed latency check without an availability error', async () => {
  const run = await script('health.js', {}, [response(publicBody, 400)]); run.exports.default();
  assert.deepEqual(run.samples.get('health_errors'), [false]);
  assert.deepEqual(run.samples.get('health_slo_breaches'), [true]);
  assert.deepEqual(run.samples.get('health_duration'), [400]);
  assert.equal(run.checks.find((check) => check.name === 'response time < 200ms').ok, false);
  assert.deepEqual(clone(run.exports.options.thresholds), {
    http_req_duration: ['p(95)<200', 'p(99)<500'], http_req_failed: ['rate<0.001'],
    health_errors: ['rate<0.001'], health_slo_breaches: ['rate<0.001'], health_duration: ['p(95)<200'],
  });
});

test('slow valid API response keeps availability and latency separate with original thresholds', async () => {
  const run = await script('api.js', {}, [response({ projects: [] }, 800), response(), response({}, 100, 404)]);
  run.exports.default({ token: 'synthetic-fixture-only' });
  assert.deepEqual(run.samples.get('api_errors'), [false]);
  assert.deepEqual(run.samples.get('api_slo_breaches'), [true]);
  assert.equal(run.checks.find((check) => check.name === 'list projects - response time < 500ms').ok, false);
  assert.deepEqual(clone(run.exports.options.thresholds), {
    http_req_duration: ['p(95)<500', 'p(99)<1000'], http_req_failed: ['rate<0.01'],
    api_errors: ['rate<0.01'], api_slo_breaches: ['rate<0.01'], projects_list_latency: ['p(95)<500'],
  });
});

const sampleRate = (samples) => samples.filter(Boolean).length / samples.length;
const percentile = (samples, fraction) => [...samples].sort((a, b) => a - b)[Math.ceil(samples.length * fraction) - 1];

async function workload(filename, samples) {
  const api = filename === 'api.js';
  const replies = samples.flatMap((sample) => api ? [sample, response(), response({}, 100, 404)] : [sample]);
  const run = await script(filename, {}, replies);
  for (const _sample of samples) run.exports.default({ token: 'synthetic-fixture-only' });
  return run;
}

for (const profile of [
  { file: 'api.js', prefix: 'api', body: { projects: [] }, slow: 600, limit: 0.01, boundary: 200 },
  { file: 'health.js', prefix: 'health', body: publicBody, slow: 300, limit: 0.001, boundary: 2000 },
]) {
  test(`${profile.file}: four slow healthy requests per100 preserve the old failing rate gate`, async () => {
    const replies = Array.from({ length: 100 }, (_, index) => response(profile.body, index < 4 ? profile.slow : 100));
    const run = await workload(profile.file, replies);
    assert.equal(sampleRate(run.samples.get(`${profile.prefix}_errors`)), 0);
    assert.equal(sampleRate(run.samples.get(`${profile.prefix}_slo_breaches`)), 0.04);
    const durations = replies.map((reply) => reply.timings.duration);
    assert.ok(percentile(durations, 0.95) < (profile.prefix === 'api' ? 500 : 200));
    assert.ok(percentile(durations, 0.99) < (profile.prefix === 'api' ? 1000 : 500));
    assert.equal(run.exports.options.thresholds[`${profile.prefix}_slo_breaches`][0], `rate<${profile.limit}`);
    assert.equal(sampleRate(run.samples.get(`${profile.prefix}_slo_breaches`)) < profile.limit, false);
  });

  test(`${profile.file}: disjoint availability and latency breaches fail at the original strict union boundary`, async () => {
    const replies = Array.from({ length: profile.boundary }, () => response(profile.body));
    replies[0] = response(profile.body, 100, 503);
    replies[1] = response(profile.body, profile.slow);
    const run = await workload(profile.file, replies);
    assert.ok(sampleRate(run.samples.get(`${profile.prefix}_errors`)) < profile.limit);
    const latencyBreaches = replies.map((reply) => reply.timings.duration >= (profile.prefix === 'api' ? 500 : 200));
    assert.ok(sampleRate(latencyBreaches) < profile.limit);
    assert.equal(sampleRate(run.samples.get(`${profile.prefix}_slo_breaches`)), profile.limit);
    assert.equal(sampleRate(run.samples.get(`${profile.prefix}_slo_breaches`)) < profile.limit, false);
  });

  test(`${profile.file}: overlapping failures count once, matching the old combined predicate`, async () => {
    const replies = Array.from({ length: profile.boundary }, () => response(profile.body));
    replies[0] = response(profile.body, profile.slow, 503);
    const run = await workload(profile.file, replies);
    const breaches = run.samples.get(`${profile.prefix}_slo_breaches`);
    assert.equal(breaches.length, replies.length);
    assert.equal(breaches.filter(Boolean).length, 1);
    assert.ok(sampleRate(breaches) < profile.limit);
  });
}

for (const [name, reply] of [['HTTP failure', response({}, 100, 503)], ['malformed body', response('not-json')]]) {
  test(`API availability still fails on ${name}`, async () => {
    const run = await script('api.js', {}, [reply, response(), response({}, 100, 404)]);
    run.exports.default({ token: 'synthetic-fixture-only' });
    assert.deepEqual(run.samples.get('api_errors'), [true]);
  });
}

for (const filename of ['health.js', 'api.js', 'stress.js']) {
  test(`${filename} explicitly requests p99 and keeps empty/default/failed summaries publishable`, async () => {
    const run = await script(filename);
    assert.ok(run.exports.options.summaryTrendStats.includes('p(99)'));
    for (const data of [{}, { metrics: {} }, { metrics: {
      http_reqs: { values: { count: 10, rate: 2 } },
      http_req_failed: { values: { rate: 0.2 }, thresholds: { 'rate<0.01': { ok: false } } },
      // Mirrors the actual failed job: default summary has p95 but no p99.
      http_req_duration: { values: { min: 100, avg: 500, med: 600, max: 1200, 'p(95)': 1100 },
        thresholds: { 'p(95)<200': { ok: false } } },
    } }]) {
      const output = clone(run.exports.handleSummary(data));
      const artifact = `results/${filename === 'health.js' ? 'health' : filename.replace('.js', '')}-summary.json`;
      assert.deepEqual(JSON.parse(output[artifact]), data);
      assert.match(output.stdout, /"p\(99\)": null/);
      assert.doesNotMatch(output.stdout, /NaN|Infinity|Capacity appears adequate/);
      if (data.metrics?.http_reqs) {
        assert.match(output.stdout, /"failed_requests": 2/);
        assert.match(output.stdout, /"ok": false/);
      } else {
        assert.match(output.stdout, /"requests": null/);
      }
    }
  });
}

test('summaries retain failed union thresholds and expose missing SLO rates as null', async () => {
  const run = await script('api.js');
  const output = clone(run.exports.handleSummary({ metrics: {
    api_errors: { values: { rate: 0.005 } },
    api_slo_breaches: { values: { rate: 0.01 }, thresholds: { 'rate<0.01': { ok: false } } },
    health_slo_breaches: { values: {} },
  } }));
  assert.match(output.stdout, /"api_slo_breaches": 0.01/);
  assert.match(output.stdout, /"health_slo_breaches": null/);
  assert.match(output.stdout, /"ok": false/);
  assert.doesNotMatch(output.stdout, /NaN|Infinity/);
});

test('stress duration/error thresholds and arrival-rate profile are unchanged', async () => {
  const run = await script('stress.js');
  assert.deepEqual(clone(run.exports.options.thresholds), {
    http_req_duration: ['p(95)<1000', 'p(99)<2000'], http_req_failed: ['rate<0.05'], errors: ['rate<0.05'],
  });
  assert.equal(run.exports.options.scenarios.stress_test.executor, 'ramping-arrival-rate');
  assert.equal(run.exports.options.scenarios.stress_test.maxVUs, 2000);
});

test('workflow PRs stay offline, scheduled authenticated coverage remains, diagnostic is explicit manual opt-in', () => {
  const workflow = fs.readFileSync(path.resolve(directory, '../../.github/workflows/load-test.yml'), 'utf8');
  function condition(job) {
    const block = workflow.split(`\n  ${job}:\n`)[1]?.split(/\n  [a-z][a-z-]*:\n/)[0];
    assert.ok(block, `missing ${job}`);
    return block.match(/^    if: (.+)$/m)?.[1];
  }
  function allowed(job, event, level) {
    return vm.runInNewContext(condition(job), { github: { event_name: event }, inputs: { test_level: level } });
  }
  for (const job of ['smoke-test', 'load-test', 'stress-test', 'deep-health-diagnostic']) {
    assert.equal(allowed(job, 'pull_request', 'stress'), false, `${job} must not request production from PRs`);
  }
  assert.equal(allowed('load-test', 'schedule'), true);
  assert.equal(allowed('stress-test', 'schedule'), false);
  assert.equal(allowed('deep-health-diagnostic', 'schedule'), false);
  assert.equal(allowed('load-test', 'workflow_dispatch', 'load'), true);
  assert.equal(allowed('load-test', 'workflow_dispatch', 'stress'), true);
  assert.equal(allowed('load-test', 'workflow_dispatch', 'deep-health'), false);
  assert.equal(allowed('stress-test', 'workflow_dispatch', 'stress'), true);
  assert.equal(allowed('deep-health-diagnostic', 'workflow_dispatch', 'deep-health'), true);
  assert.match(workflow, /run: test "\$ALLOW_DEEP_HEALTH" = 'true'/);
  assert.match(workflow, /ENCLII_ALLOW_DEEP_HEALTH_DIAGNOSTIC: \$\{\{ inputs\.allow_deep_health_diagnostic }}/);
  assert.match(workflow, /node --experimental-vm-modules --test tests\/load\/harness\.test\.cjs/);
});
