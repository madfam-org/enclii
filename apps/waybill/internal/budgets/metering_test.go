package budgets

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

var meteringCols = []string{"last_heartbeat", "last_hour", "project_rows", "unaggregated"}

func newMeteringReader(t *testing.T) (*CostReader, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewCostReader(db, PricingDollars{}), mock
}

func meteringNow() time.Time { return time.Date(2026, 10, 17, 20, 30, 0, 0, time.UTC) }

func TestMeteringNotRunningWhenAggregatorNeverRan(t *testing.T) {
	// Today's production shape: no evaluator run row, no hourly_usage rows.
	r, mock := newMeteringReader(t)
	now := meteringNow()
	start := now.AddDate(0, 0, -30)
	mock.ExpectQuery("FROM budget_evaluator_runs").
		WithArgs(sqlmock.AnyArg(), start, now, time.Date(2026, 10, 17, 19, 0, 0, 0, time.UTC), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(meteringCols).AddRow(nil, nil, 0, 0))

	m, err := r.Metering(context.Background(), uuid.New(), start, now, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Status != MeteringNotRunning || m.SpendKnown() {
		t.Fatalf("expected not_running and spend unknown, got %+v", m)
	}
	if m.LastHeartbeatAt != nil || m.LastAggregatedHour != nil {
		t.Fatalf("nil signals must stay nil, got %+v", m)
	}
	if m.StaleAfterSeconds != int64(DefaultMeteringStaleAfter/time.Second) {
		t.Fatalf("stale_after_seconds = %d", m.StaleAfterSeconds)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMeteringStaleWhenHeartbeatOld(t *testing.T) {
	r, mock := newMeteringReader(t)
	now := meteringNow()
	beat := now.Add(-3 * time.Hour)
	mock.ExpectQuery("FROM budget_evaluator_runs").
		WillReturnRows(sqlmock.NewRows(meteringCols).AddRow(beat, beat.Truncate(time.Hour), 12, 0))

	m, err := r.Metering(context.Background(), uuid.New(), now.AddDate(0, 0, -30), now, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Status != MeteringStale || m.SpendKnown() {
		t.Fatalf("expected stale, got %+v", m)
	}
	if m.LastHeartbeatAt == nil || !m.LastHeartbeatAt.Equal(beat) {
		t.Fatalf("last heartbeat must be reported, got %v", m.LastHeartbeatAt)
	}
	if !strings.Contains(m.Detail, "3h0m0s ago") {
		t.Fatalf("detail should say how old the heartbeat is, got %q", m.Detail)
	}
}

func TestMeteringStaleWhenEventsNeverAggregated(t *testing.T) {
	r, mock := newMeteringReader(t)
	now := meteringNow()
	mock.ExpectQuery("FROM budget_evaluator_runs").
		WillReturnRows(sqlmock.NewRows(meteringCols).AddRow(now.Add(-10*time.Minute), now.Add(-2*time.Hour), 4, 7))

	m, err := r.Metering(context.Background(), uuid.New(), now.AddDate(0, 0, -30), now, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Status != MeteringStale || m.UnaggregatedEvents != 7 {
		t.Fatalf("expected stale with 7 unaggregated events, got %+v", m)
	}
}

func TestMeteringOKOnRealZero(t *testing.T) {
	// Aggregator alive, nothing overdue, project simply has no usage: a real
	// $0.00 that may be shown as a fact.
	r, mock := newMeteringReader(t)
	now := meteringNow()
	mock.ExpectQuery("FROM budget_evaluator_runs").
		WillReturnRows(sqlmock.NewRows(meteringCols).AddRow(now.Add(-5*time.Minute), now.Add(-2*time.Hour), 0, 0))

	m, err := r.Metering(context.Background(), uuid.New(), now.AddDate(0, 0, -30), now, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Status != MeteringOK || !m.SpendKnown() {
		t.Fatalf("expected ok, got %+v", m)
	}
}

func TestMeteringUnknownOnReadError(t *testing.T) {
	r, mock := newMeteringReader(t)
	now := meteringNow()
	mock.ExpectQuery("FROM budget_evaluator_runs").WillReturnError(errors.New("connection refused"))

	m, err := r.Metering(context.Background(), uuid.New(), now.AddDate(0, 0, -30), now, now)
	if err == nil {
		t.Fatalf("expected the read error to be returned")
	}
	if m.Status != MeteringUnknown || m.SpendKnown() {
		t.Fatalf("a failed read must never be ok, got %+v", m)
	}
}

func TestMeteringStaleAfterOverride(t *testing.T) {
	r, mock := newMeteringReader(t)
	r.SetMeteringStaleAfter(4 * time.Hour)
	r.SetMeteringStaleAfter(0) // ignored
	now := meteringNow()
	mock.ExpectQuery("FROM budget_evaluator_runs").
		WillReturnRows(sqlmock.NewRows(meteringCols).AddRow(now.Add(-3*time.Hour), nil, 0, 0))

	m, err := r.Metering(context.Background(), uuid.New(), now.AddDate(0, 0, -30), now, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Status != MeteringOK || m.StaleAfterSeconds != int64(4*time.Hour/time.Second) {
		t.Fatalf("a 3h-old heartbeat is within a 4h window, got %+v", m)
	}
}
