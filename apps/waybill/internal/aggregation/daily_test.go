package aggregation

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDailyRollup_RollsTheWholeUTCDay(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 23:30 in UTC-6 is 05:30 UTC the next day: the rollup must use the UTC day.
	loc := time.FixedZone("UTC-6", -6*3600)
	in := time.Date(2026, 11, 3, 23, 30, 0, 0, loc)
	wantStart := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO daily_usage")).
		WithArgs("2026-11-04", wantStart, wantEnd).
		WillReturnResult(sqlmock.NewResult(0, 7))

	rows, err := NewDailyRollup(db, nil).Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rows != 7 {
		t.Fatalf("rows = %d, want 7", rows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDailyRollup_ReturnsTheError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO daily_usage")).WillReturnError(errors.New("boom"))

	if _, err := NewDailyRollup(db, nil).Run(context.Background(), time.Now()); err == nil {
		t.Fatal("expected an error")
	}
}

// The query must replace a day's value on rerun (never add to it), sum every
// metric type the same way, and write nothing for a day without hourly rows.
func TestDailyRollupQuery_IsIdempotentAndSumsEveryType(t *testing.T) {
	q := strings.Join(strings.Fields(dailyRollupQuery), " ")
	for _, want := range []string{
		"SUM(value)",
		"FROM hourly_usage",
		"WHERE hour >= $2 AND hour < $3",
		"GROUP BY project_id, metric_type",
		"ON CONFLICT (project_id, metric_type, date) DO UPDATE SET value = EXCLUDED.value",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q:\n%s", want, q)
		}
	}
	if strings.Contains(q, "value = daily_usage.value +") {
		t.Error("a rerun must replace the day's value, not add to it")
	}
	if strings.Contains(q, "COALESCE") {
		t.Error("a day without hourly rows must write nothing, not a zero")
	}
}
