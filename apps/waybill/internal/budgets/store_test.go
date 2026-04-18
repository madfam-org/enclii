package budgets

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func TestStoreCreate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	store := NewStore(db)
	projectID := uuid.New()
	now := time.Now().UTC()

	mock.ExpectQuery("INSERT INTO budgets").
		WithArgs(sqlmock.AnyArg(), projectID, int64(50000), "USD", "monthly", pq.Array([]int{50, 80, 100}), true).
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

	b, err := store.Create(context.Background(), projectID, CreateRequest{
		AmountCents: 50000,
		Period:      PeriodMonthly,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if b.AmountCents != 50000 {
		t.Fatalf("expected 50000, got %d", b.AmountCents)
	}
	if b.Currency != "USD" {
		t.Fatalf("expected USD default, got %s", b.Currency)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet: %v", err)
	}
}

func TestStoreCreateAlreadyExists(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	store := NewStore(db)
	mock.ExpectQuery("INSERT INTO budgets").
		WillReturnError(&pq.Error{Code: "23505"})

	_, err = store.Create(context.Background(), uuid.New(), CreateRequest{
		AmountCents: 10000,
		Period:      PeriodMonthly,
	})
	if err != ErrAlreadyExists {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestStoreInsertAlertEventIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()
	store := NewStore(db)

	ev := &AlertEvent{
		BudgetID:    uuid.New(),
		ProjectID:   uuid.New(),
		PeriodStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Threshold:   80,
		ActualCents: 40000,
		BudgetCents: 50000,
	}

	// First insert — row is created.
	id := uuid.New()
	now := time.Now().UTC()
	mock.ExpectQuery("INSERT INTO budget_alert_events").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(id, now))

	saved, created, err := store.InsertAlertEventIfMissing(context.Background(), ev)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true on first insert")
	}
	if saved.ID != id {
		t.Fatalf("id mismatch")
	}

	// Second attempt — INSERT returns an empty rowset (ON CONFLICT DO NOTHING),
	// Scan yields sql.ErrNoRows, store falls back to SELECT.
	mock.ExpectQuery("INSERT INTO budget_alert_events").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}))
	mock.ExpectQuery("SELECT id, budget_id, project_id, period_start").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "budget_id", "project_id", "period_start", "period_end",
			"threshold", "actual_cents", "budget_cents",
			"dispatched_at", "dispatch_attempts", "last_error", "created_at",
		}).AddRow(id, ev.BudgetID, ev.ProjectID, ev.PeriodStart, ev.PeriodEnd,
			ev.Threshold, ev.ActualCents, ev.BudgetCents, nil, 0, nil, now))

	ev2 := *ev
	_, created2, err := store.InsertAlertEventIfMissing(context.Background(), &ev2)
	if err != nil {
		t.Fatalf("insert 2: %v", err)
	}
	if created2 {
		t.Fatalf("expected created=false on duplicate")
	}
}
