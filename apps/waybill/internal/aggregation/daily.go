package aggregation

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
)

// dailyRollupQuery rolls one UTC day of hourly_usage into daily_usage.
//
// Every hourly_usage value is an amount for its hour: GB-hours, minutes, GB,
// requests, or (custom_domains) a net change. So the day is the SUM of its
// hours for every metric type, with no per-type exceptions to drift out of
// date as types are added.
//
// Idempotent by construction: the UNIQUE (project_id, metric_type, date)
// constraint plus DO UPDATE means a rerun, or a late hourly row followed by a
// rerun, replaces the day's value instead of adding to it. A day with no
// hourly rows writes nothing; absence is "nothing metered", never a zero.
const dailyRollupQuery = `
	INSERT INTO daily_usage (id, project_id, metric_type, value, date, created_at)
	SELECT gen_random_uuid(), project_id, metric_type, SUM(value), $1::date, now()
	FROM hourly_usage
	WHERE hour >= $2 AND hour < $3
	GROUP BY project_id, metric_type
	ON CONFLICT (project_id, metric_type, date)
	DO UPDATE SET value = EXCLUDED.value
`

// DailyRollup writes daily_usage from hourly_usage.
type DailyRollup struct {
	db     *sql.DB
	logger *zap.Logger
}

// NewDailyRollup creates a daily rollup over the given database.
func NewDailyRollup(db *sql.DB, logger *zap.Logger) *DailyRollup {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &DailyRollup{db: db, logger: logger}
}

// Run rolls up the UTC day that contains `day`. It returns the number of
// (project, metric) rows written.
func (d *DailyRollup) Run(ctx context.Context, day time.Time) (int64, error) {
	start := time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)

	ctx, span := tracer.Start(ctx, "aggregation.daily")
	defer span.End()
	span.SetAttributes(attribute.String("aggregation.day", start.Format("2006-01-02")))

	res, err := d.db.ExecContext(ctx, dailyRollupQuery, start.Format("2006-01-02"), start, end)
	if err != nil {
		span.SetStatus(codes.Error, "daily rollup")
		span.RecordError(err)
		return 0, fmt.Errorf("daily rollup for %s: %w", start.Format("2006-01-02"), err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		rows = -1
	}
	span.SetAttributes(attribute.Int64("aggregation.rows", rows))
	d.logger.Info("daily rollup complete",
		zap.String("day", start.Format("2006-01-02")),
		zap.Int64("rows", rows),
	)
	return rows, nil
}
