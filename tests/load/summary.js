// k6 does not include p(99) in its default summary trend statistics.
export const SUMMARY_TREND_STATS = ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'];

function finite(value) {
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}

// Report unavailable observations explicitly. Formatting must not turn a failed
// or empty run into a summary exception, or invent a duration/throughput value.
export function renderSummary(data, title, artifact, scope) {
  const metrics = data.metrics || {};
  const requests = metrics.http_reqs?.values || {};
  const failure = metrics.http_req_failed?.values || {};
  const duration = metrics.http_req_duration?.values || {};
  const count = finite(requests.count);
  const failedRate = finite(failure.rate);
  const report = {
    scope,
    requests: count,
    observed_requests_per_second: finite(requests.rate),
    http_failed_rate: failedRate,
    failed_requests: count !== null && failedRate !== null ? Math.round(count * failedRate) : null,
    duration_ms: Object.fromEntries(SUMMARY_TREND_STATS.map((stat) => [stat, finite(duration[stat])])),
    availability_error_rates: Object.fromEntries(['health_errors', 'api_errors', 'errors']
      .filter((name) => metrics[name])
      .map((name) => [name, finite(metrics[name].values?.rate)])),
    slo_breach_rates: Object.fromEntries(['health_slo_breaches', 'api_slo_breaches']
      .filter((name) => metrics[name])
      .map((name) => [name, finite(metrics[name].values?.rate)])),
    thresholds: Object.fromEntries(Object.entries(metrics)
      .filter(([, metric]) => metric.thresholds && Object.keys(metric.thresholds).length)
      .map(([name, metric]) => [name, metric.thresholds])),
  };
  return {
    stdout: `${title}\n${JSON.stringify(report, null, 2)}\nMissing observations are null; k6 threshold failures still fail the run.\n`,
    [`results/${artifact}-summary.json`]: JSON.stringify(data, null, 2),
  };
}
