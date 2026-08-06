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

// budgetRows returns a rowset shaped like the SELECT in Store.Get.
func budgetRows(id, projectID uuid.UUID, amount int64, thresholds pq.Int64Array, hard bool, now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "project_id", "amount_cents", "currency", "period",
		"alert_thresholds", "hard_throttle", "created_at", "updated_at",
	}).AddRow(id, projectID, amount, "USD", "monthly", thresholds, hard, now, now)
}

func TestStoreUpdatePartialBindsNullsForUnsetFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	store := NewStore(db)
	budgetID := uuid.New()
	projectID := uuid.New()
	now := time.Now().UTC()
	amount := int64(75000)

	// Only amount_cents is set — the other two parameters must bind as
	// NULL so COALESCE leaves them unchanged. The query text is a single
	// constant: id first, then the three optional fields.
	mock.ExpectExec("UPDATE budgets SET").
		WithArgs(budgetID, amount, nil, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, project_id, amount_cents").
		WithArgs(budgetID).
		WillReturnRows(budgetRows(budgetID, projectID, amount, pq.Int64Array{50, 80, 100}, true, now))

	b, err := store.Update(context.Background(), budgetID, UpdateRequest{AmountCents: &amount})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if b.AmountCents != amount {
		t.Fatalf("expected %d, got %d", amount, b.AmountCents)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet: %v", err)
	}
}

func TestStoreUpdateNormalizesThresholds(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	store := NewStore(db)
	budgetID := uuid.New()
	projectID := uuid.New()
	now := time.Now().UTC()

	// Duplicates and unsorted input must reach the driver deduped+sorted;
	// amount and hard_throttle stay NULL.
	mock.ExpectExec("UPDATE budgets SET").
		WithArgs(budgetID, nil, pq.Array([]int{50, 100}), nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, project_id, amount_cents").
		WithArgs(budgetID).
		WillReturnRows(budgetRows(budgetID, projectID, 50000, pq.Int64Array{50, 100}, true, now))

	b, err := store.Update(context.Background(), budgetID, UpdateRequest{
		AlertThresholds: []int{100, 50, 50},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(b.AlertThresholds) != 2 || b.AlertThresholds[0] != 50 || b.AlertThresholds[1] != 100 {
		t.Fatalf("expected normalized [50 100], got %v", b.AlertThresholds)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet: %v", err)
	}
}

func TestStoreUpdateNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	store := NewStore(db)
	hard := false
	mock.ExpectExec("UPDATE budgets SET").
		WillReturnResult(sqlmock.NewResult(0, 0))

	_, err = store.Update(context.Background(), uuid.New(), UpdateRequest{HardThrottle: &hard})
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStoreUpdateNoFieldsIsReadOnly(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	store := NewStore(db)
	budgetID := uuid.New()
	projectID := uuid.New()
	now := time.Now().UTC()

	// All-nil request must not issue an UPDATE (updated_at untouched) —
	// only the Get SELECT.
	mock.ExpectQuery("SELECT id, project_id, amount_cents").
		WithArgs(budgetID).
		WillReturnRows(budgetRows(budgetID, projectID, 10000, pq.Int64Array{50, 80, 100}, true, now))

	b, err := store.Update(context.Background(), budgetID, UpdateRequest{})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if b.AmountCents != 10000 {
		t.Fatalf("expected passthrough Get, got %+v", b)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet: %v", err)
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
