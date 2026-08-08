package budgets

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CostReader queries aggregated cost totals from hourly_usage plus the
// waybill pricing table. The implementation leans on hourly_usage.metric_type
// and the per-unit prices passed in at construction time.
type CostReader struct {
	db      *sql.DB
	pricing PricingCents
}

// PricingCents holds per-unit prices expressed in cents (USD). Using cents
// keeps the totals integer-exact under realistic volumes.
type PricingCents struct {
	ComputePerGBHour  float64 // cents / GB-hour
	BuildPerMinute    float64 // cents / minute
	StoragePerGBMonth float64 // cents / GB-month
	BandwidthPerGB    float64 // cents / GB
}

// NewCostReader builds a reader from dollar-denominated pricing.
func NewCostReader(db *sql.DB, p PricingDollars) *CostReader {
	return &CostReader{db: db, pricing: p.ToCents()}
}

// PricingDollars is the inbound form of the config.
type PricingDollars struct {
	ComputePerGBHour  float64
	BuildPerMinute    float64
	StoragePerGBMonth float64
	BandwidthPerGB    float64
}

// ToCents converts dollar prices to cents (1 USD = 100 cents).
func (p PricingDollars) ToCents() PricingCents {
	return PricingCents{
		ComputePerGBHour:  p.ComputePerGBHour * 100,
		BuildPerMinute:    p.BuildPerMinute * 100,
		StoragePerGBMonth: p.StoragePerGBMonth * 100,
		BandwidthPerGB:    p.BandwidthPerGB * 100,
	}
}

// ProjectCost returns total spend in cents for project `projectID` over [start, end).
func (r *CostReader) ProjectCost(ctx context.Context, projectID uuid.UUID, start, end time.Time) (int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT metric_type, COALESCE(SUM(value), 0) as total
		FROM hourly_usage
		WHERE project_id = $1 AND hour >= $2 AND hour < $3
		GROUP BY metric_type
	`, projectID, start, end)
	if err != nil {
		return 0, fmt.Errorf("query project cost: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var totalCents float64
	for rows.Next() {
		var metric string
		var v float64
		if err := rows.Scan(&metric, &v); err != nil {
			return 0, err
		}
		totalCents += r.priceCents(metric, v, start, end)
	}
	return int64(totalCents + 0.5), rows.Err()
}

// TimeSeriesPoint is a single bucket in a time-series response.
type TimeSeriesPoint struct {
	Bucket    time.Time `json:"bucket"`
	CostCents int64     `json:"cost_cents"`
	// ByMetric holds metric_type → cost in cents inside this bucket.
	ByMetric map[string]int64 `json:"by_metric,omitempty"`
}

// ServiceCost returns approximate cost in cents for a single service over
// [start, end). Hourly_usage is project-scoped, so we fall back to summing
// raw event cost for build/bandwidth and omitting compute (which is
// aggregated only at the project level).
func (r *CostReader) ServiceCost(ctx context.Context, serviceID uuid.UUID, start, end time.Time) (int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT event_type, metrics
		FROM usage_events
		WHERE resource_id = $1 AND timestamp >= $2 AND timestamp < $3
	`, serviceID, start, end)
	if err != nil {
		return 0, fmt.Errorf("query service cost: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var total float64
	for rows.Next() {
		var eventType string
		var metricsJSON []byte
		if err := rows.Scan(&eventType, &metricsJSON); err != nil {
			return 0, err
		}
		total += approximateEventCentsJSON(eventType, metricsJSON, r.pricing)
	}
	return int64(total + 0.5), rows.Err()
}

// ProjectSeries returns a daily time-series of costs for a project.
func (r *CostReader) ProjectSeries(ctx context.Context, projectID uuid.UUID, start, end time.Time) ([]TimeSeriesPoint, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT date_trunc('day', hour) as day, metric_type, COALESCE(SUM(value),0) as total
		FROM hourly_usage
		WHERE project_id = $1 AND hour >= $2 AND hour < $3
		GROUP BY day, metric_type
		ORDER BY day ASC
	`, projectID, start, end)
	if err != nil {
		return nil, fmt.Errorf("query project series: %w", err)
	}
	defer func() { _ = rows.Close() }()

	buckets := map[time.Time]*TimeSeriesPoint{}
	for rows.Next() {
		var day time.Time
		var metric string
		var v float64
		if err := rows.Scan(&day, &metric, &v); err != nil {
			return nil, err
		}
		cents := int64(r.priceCents(metric, v, start, end) + 0.5)
		p := buckets[day]
		if p == nil {
			p = &TimeSeriesPoint{Bucket: day, ByMetric: map[string]int64{}}
			buckets[day] = p
		}
		p.CostCents += cents
		p.ByMetric[metric] += cents
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Emit a dense series (zero-filled days) so the UI can draw a stable axis.
	var out []TimeSeriesPoint
	cur := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	for !cur.After(endDay) {
		if p, ok := buckets[cur]; ok {
			out = append(out, *p)
		} else {
			out = append(out, TimeSeriesPoint{Bucket: cur, CostCents: 0, ByMetric: map[string]int64{}})
		}
		cur = cur.AddDate(0, 0, 1)
	}
	return out, nil
}

// BreakdownEntry is a row in a per-metric / per-service / per-env breakdown.
type BreakdownEntry struct {
	Key       string `json:"key"`
	CostCents int64  `json:"cost_cents"`
}

// ProjectBreakdown returns grouped costs. `groupBy` must be one of:
//   - "service" — groups by the `resource_id` / `resource_name` of deployment events
//   - "env"     — groups by metadata.env on the source events
//   - "metric"  — groups by metric_type (default / fallback)
func (r *CostReader) ProjectBreakdown(ctx context.Context, projectID uuid.UUID, start, end time.Time, groupBy string) ([]BreakdownEntry, error) {
	switch strings.ToLower(groupBy) {
	case "service":
		return r.breakdownByService(ctx, projectID, start, end)
	case "env", "environment":
		return r.breakdownByEnv(ctx, projectID, start, end)
	default:
		return r.breakdownByMetric(ctx, projectID, start, end)
	}
}

func (r *CostReader) breakdownByMetric(ctx context.Context, projectID uuid.UUID, start, end time.Time) ([]BreakdownEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT metric_type, COALESCE(SUM(value),0) as total
		FROM hourly_usage
		WHERE project_id = $1 AND hour >= $2 AND hour < $3
		GROUP BY metric_type
		ORDER BY total DESC
	`, projectID, start, end)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []BreakdownEntry
	for rows.Next() {
		var metric string
		var v float64
		if err := rows.Scan(&metric, &v); err != nil {
			return nil, err
		}
		cents := int64(r.priceCents(metric, v, start, end) + 0.5)
		if cents == 0 {
			continue
		}
		out = append(out, BreakdownEntry{Key: metric, CostCents: cents})
	}
	return out, rows.Err()
}

func (r *CostReader) breakdownByService(ctx context.Context, projectID uuid.UUID, start, end time.Time) ([]BreakdownEntry, error) {
	// We derive per-service cost from raw usage_events, since hourly_usage is
	// project-scoped only. This is a rough approximation — sufficient for a
	// top-5 driver list.
	rows, err := r.db.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(resource_name, ''), resource_id::text) AS key,
		       event_type,
		       metrics
		FROM usage_events
		WHERE project_id = $1 AND timestamp >= $2 AND timestamp < $3
	`, projectID, start, end)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	totals := map[string]float64{}
	for rows.Next() {
		var key, eventType string
		var metricsJSON []byte
		if err := rows.Scan(&key, &eventType, &metricsJSON); err != nil {
			return nil, err
		}
		totals[key] += approximateEventCentsJSON(eventType, metricsJSON, r.pricing)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]BreakdownEntry, 0, len(totals))
	for k, v := range totals {
		cents := int64(v + 0.5)
		if cents == 0 {
			continue
		}
		out = append(out, BreakdownEntry{Key: k, CostCents: cents})
	}
	sortDescByCost(out)
	return out, nil
}

func (r *CostReader) breakdownByEnv(ctx context.Context, projectID uuid.UUID, start, end time.Time) ([]BreakdownEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT COALESCE(metadata->>'env', 'unknown') AS env,
		       event_type,
		       metrics
		FROM usage_events
		WHERE project_id = $1 AND timestamp >= $2 AND timestamp < $3
	`, projectID, start, end)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	totals := map[string]float64{}
	for rows.Next() {
		var env, eventType string
		var metricsJSON []byte
		if err := rows.Scan(&env, &eventType, &metricsJSON); err != nil {
			return nil, err
		}
		totals[env] += approximateEventCentsJSON(eventType, metricsJSON, r.pricing)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]BreakdownEntry, 0, len(totals))
	for k, v := range totals {
		cents := int64(v + 0.5)
		if cents == 0 {
			continue
		}
		out = append(out, BreakdownEntry{Key: k, CostCents: cents})
	}
	sortDescByCost(out)
	return out, nil
}

// priceCents returns the price in cents for `total` units of `metric` across
// [start,end). The formula mirrors metering.Calculator.calculateCost but in cents.
func (r *CostReader) priceCents(metric string, total float64, start, end time.Time) float64 {
	switch metric {
	case "compute_gb_hours":
		return total * r.pricing.ComputePerGBHour
	case "build_minutes":
		return total * r.pricing.BuildPerMinute
	case "storage_gb_hours":
		// The stored total is already gb-hours, so it is rescaled to gb-months
		// by the hours in a 30-day month. NOTE: the [start,end) window does not
		// affect this price. That was already true -- the previous code derived
		// `hours` from the window, multiplied by (hours / hours) which is 1,
		// then overwrote the result with this same expression. Behaviour is
		// unchanged; only the dead computation is gone.
		gbMonths := total / (24 * 30.0)
		return gbMonths * r.pricing.StoragePerGBMonth
	case "bandwidth_gb":
		return total * r.pricing.BandwidthPerGB
	case "custom_domains":
		return 0
	}
	return 0
}

func sortDescByCost(out []BreakdownEntry) {
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CostCents > out[i].CostCents {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
}

// approximateEventCentsJSON is a best-effort per-event cost estimator used for
// service / env breakdowns where we cannot reuse the hourly aggregate.
func approximateEventCentsJSON(eventType string, metricsJSON []byte, p PricingCents) float64 {
	m := decodeMetricsJSON(metricsJSON)
	switch eventType {
	case "build.completed":
		return (m["duration_seconds"] / 60.0) * p.BuildPerMinute
	case "bandwidth.usage":
		return m["egress_gb"] * p.BandwidthPerGB
	}
	// Deployment events are tracked via hourly_usage aggregates; we don't
	// attempt to double-count them here.
	return 0
}
