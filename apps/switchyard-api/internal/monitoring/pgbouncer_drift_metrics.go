package monitoring

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// These three metrics exist to close the gap enclii#436 called out explicitly:
// "This method is the detector the outage lacked; wire it into a periodic
// check that pages when HasMissing() is true." (internal/provisioning
// /userlist_reconcile.go). The 2026-08-24 outage — a hand-applied pgbouncer
// userlist silently dropped four login roles, taking fortuna/bloom-scroll/ceq
// hard-down for days — was invisible because nothing ever ran the detector on
// a schedule and nothing exported its result. These gauges/counter are what a
// Prometheus alert rule pages on.
var (
	pgbouncerUserlistMissingUsers = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "enclii_pgbouncer_userlist_missing_users",
			Help: "Count of Postgres login roles absent from the pgbouncer userlist (unroutable through the pooler). Zero means in sync.",
		},
	)
	pgbouncerUserlistCheckErrorsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "enclii_pgbouncer_userlist_check_errors_total",
			Help: "Total pgbouncer userlist drift check runs that errored (DB or k8s access failure) rather than completing a comparison.",
		},
	)
	pgbouncerUserlistLastCheckTimestamp = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "enclii_pgbouncer_userlist_last_check_timestamp_seconds",
			Help: "Unix timestamp of the last SUCCESSFUL pgbouncer userlist drift check. A stale value is the dead-man signal that the check itself has stopped running, per the green-lies doctrine — never inferred from missing_users staying at zero.",
		},
	)
)

// PgbouncerDriftMetricsCollectors returns Prometheus collectors for the custom registry.
func PgbouncerDriftMetricsCollectors() []prometheus.Collector {
	return []prometheus.Collector{
		pgbouncerUserlistMissingUsers,
		pgbouncerUserlistCheckErrorsTotal,
		pgbouncerUserlistLastCheckTimestamp,
	}
}

// RecordPgbouncerUserlistCheckSuccess sets the missing-user gauge from a
// completed drift comparison and stamps the last-check timestamp to now.
// Call this ONLY when the check actually completed — a failed check must go
// through RecordPgbouncerUserlistCheckError instead, so the timestamp gauge
// stays a true dead-man for the checker, not a duration since the last
// attempt.
func RecordPgbouncerUserlistCheckSuccess(missingCount int) {
	pgbouncerUserlistMissingUsers.Set(float64(missingCount))
	pgbouncerUserlistLastCheckTimestamp.Set(float64(time.Now().Unix()))
}

// RecordPgbouncerUserlistCheckError increments the error counter for a check
// run that could not complete. It deliberately does NOT touch the
// missing-users gauge (stale-but-last-known is more useful than resetting to
// zero) or the last-check timestamp (a run that errored is not a successful
// check).
func RecordPgbouncerUserlistCheckError() {
	pgbouncerUserlistCheckErrorsTotal.Inc()
}
