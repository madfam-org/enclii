package budgets

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// MeteringStatus says whether a spend total read from hourly_usage can be
// trusted. Spend is the sum of what the aggregator wrote, so an empty
// hourly_usage table reads as $0.00 whether the project spent nothing or the
// aggregator never ran. The status is what tells those two apart: "I read
// nothing" must never look like "all clear".
type MeteringStatus string

const (
	// MeteringOK: the aggregator process is alive and every usage event that
	// should have been rolled up by now has been. The total is real.
	MeteringOK MeteringStatus = "ok"
	// MeteringNotRunning: the aggregator process has never recorded a run
	// against this database. The total is not a measurement.
	MeteringNotRunning MeteringStatus = "not_running"
	// MeteringStale: the aggregator ran once but has not reported within the
	// staleness window, or usage events exist that were never rolled up. The
	// total is an undercount of unknown size.
	MeteringStale MeteringStatus = "stale"
	// MeteringUnknown: the status read itself failed.
	MeteringUnknown MeteringStatus = "unknown"
)

// DefaultMeteringStaleAfter is how long the aggregator may go without a
// heartbeat before metering reads as stale. The aggregator's budget evaluator
// writes a budget_evaluator_runs row every WAYBILL_ALERT_INTERVAL (default
// 15m), so one hour is four missed ticks. Any override must stay above the
// evaluator interval or a healthy aggregator reads as stale.
const DefaultMeteringStaleAfter = time.Hour

// Metering is the read-proof block attached to every spend total.
type Metering struct {
	Status MeteringStatus `json:"status"`
	// Detail is a one-line, human-readable reason for the status.
	Detail string `json:"detail"`
	// LastHeartbeatAt is the newest budget_evaluator_runs.started_at. The
	// evaluator runs inside the waybill-aggregator process, so this is the
	// aggregator's liveness signal. Nil means it never ran.
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at"`
	// LastAggregatedHour is the newest hourly_usage.hour across all projects.
	LastAggregatedHour *time.Time `json:"last_aggregated_hour"`
	// ProjectUsageRows counts hourly_usage rows for this project in the window.
	ProjectUsageRows int64 `json:"project_usage_rows"`
	// UnaggregatedEvents counts this project's metered usage events in the
	// window whose hour is overdue and has no hourly_usage row: rolled up late
	// or never.
	UnaggregatedEvents int64 `json:"unaggregated_events"`
	// StaleAfterSeconds is the heartbeat window this status was judged by.
	StaleAfterSeconds int64 `json:"stale_after_seconds"`
	// CheckedAt is when the status was computed.
	CheckedAt time.Time `json:"checked_at"`
}

// SpendKnown reports whether the spend total can be presented as a fact.
func (m Metering) SpendKnown() bool { return m.Status == MeteringOK }

// SetMeteringStaleAfter overrides DefaultMeteringStaleAfter. Non-positive
// values are ignored.
func (r *CostReader) SetMeteringStaleAfter(d time.Duration) {
	if d > 0 {
		r.staleAfter = d
	}
}

func (r *CostReader) meteringStaleAfter() time.Duration {
	if r.staleAfter > 0 {
		return r.staleAfter
	}
	return DefaultMeteringStaleAfter
}

// meteredEventTypes are the usage events that, on their own, always make the
// aggregator write an hourly_usage row for their hour (see
// aggregation.HourlyAggregator.calculateMetrics). Events that can net to zero
// alone (build.started, build.failed, deployment.stopped without its start,
// volume.deleted, addon.destroyed, domain.removed) are left out so a quiet
// hour never reads as a gap.
var meteredEventTypes = []string{
	"deployment.started",
	"deployment.scaled",
	"build.completed",
	"volume.created",
	"volume.resized",
	"bandwidth.usage",
	"addon.ready",
	"addon.plan.changed",
	"domain.added",
}

// meteringQuery reads every signal in one round trip.
//
//	$1 project_id  $2 window start  $3 window end  $4 settle cutoff
//	$5 metered event types
//
// The aggregator rolls hour [H, H+1h) up at H+1h+5m and stamps its rows with
// hour = H (UTC hour start, Go's time.Truncate). An event is overdue once its
// timestamp is behind the settle cutoff (start of the current hour minus one
// hour); an overdue metered event whose hour has no hourly_usage row for its
// project was never rolled up. The hour is derived from the epoch so the
// session TimeZone cannot shift it. The aggregator does not backfill missed
// hours, so this also catches gaps left by an outage, not only lag.
//
// Index use: budget_evaluator_runs PK, idx_hourly_usage_hour for MAX(hour),
// idx_hourly_usage_lookup (project_id, hour, …) for the counts and the
// per-event probe, idx_usage_events_project for the event scan.
const meteringQuery = `
	SELECT
		(SELECT started_at FROM budget_evaluator_runs ORDER BY id DESC LIMIT 1) AS last_heartbeat,
		(SELECT MAX(hour) FROM hourly_usage) AS last_hour,
		(SELECT COUNT(*) FROM hourly_usage
		  WHERE project_id = $1 AND hour >= $2 AND hour < $3) AS project_rows,
		(SELECT COUNT(*) FROM usage_events e
		  WHERE e.project_id = $1
		    AND e.timestamp >= $2 AND e.timestamp < LEAST($3::timestamptz, $4::timestamptz)
		    AND e.event_type = ANY($5)
		    AND NOT EXISTS (
		      SELECT 1 FROM hourly_usage h
		       WHERE h.project_id = e.project_id
		         AND h.hour = to_timestamp(floor(extract(epoch FROM e.timestamp) / 3600) * 3600)
		    )) AS unaggregated
`

// Metering reports whether ProjectCost over [start, end) is a measurement.
// It never returns MeteringOK on a read error: the error is returned and the
// status is MeteringUnknown, so a caller that ignores the error still gets a
// non-OK status.
func (r *CostReader) Metering(ctx context.Context, projectID uuid.UUID, start, end, now time.Time) (Metering, error) {
	staleAfter := r.meteringStaleAfter()
	m := Metering{
		Status:            MeteringUnknown,
		Detail:            "metering status could not be read",
		StaleAfterSeconds: int64(staleAfter / time.Second),
		CheckedAt:         now.UTC(),
	}
	cutoff := now.UTC().Truncate(time.Hour).Add(-time.Hour)

	var heartbeat, lastHour sql.NullTime
	err := r.db.QueryRowContext(ctx, meteringQuery, projectID, start, end, cutoff, pq.Array(meteredEventTypes)).
		Scan(&heartbeat, &lastHour, &m.ProjectUsageRows, &m.UnaggregatedEvents)
	if err != nil {
		return m, fmt.Errorf("query metering status: %w", err)
	}
	if heartbeat.Valid {
		t := heartbeat.Time.UTC()
		m.LastHeartbeatAt = &t
	}
	if lastHour.Valid {
		t := lastHour.Time.UTC()
		m.LastAggregatedHour = &t
	}

	m.Status, m.Detail = classifyMetering(m, now.UTC(), staleAfter)
	return m, nil
}

func classifyMetering(m Metering, now time.Time, staleAfter time.Duration) (MeteringStatus, string) {
	if m.LastHeartbeatAt == nil {
		return MeteringNotRunning, "the waybill aggregator has never run against this database; spend is unknown"
	}
	if age := now.Sub(*m.LastHeartbeatAt); age > staleAfter {
		return MeteringStale, fmt.Sprintf(
			"the waybill aggregator last reported %s ago (limit %s); spend is unknown",
			age.Truncate(time.Minute), staleAfter)
	}
	if m.UnaggregatedEvents > 0 {
		return MeteringStale, fmt.Sprintf(
			"%d usage event(s) in this window were never aggregated; spend is undercounted", m.UnaggregatedEvents)
	}
	return MeteringOK, "aggregator alive and up to date"
}
