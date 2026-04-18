package budgets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

var (
	// ErrNotFound is returned when a budget lookup has no row.
	ErrNotFound = errors.New("budget not found")
	// ErrAlreadyExists is returned when a project already has a budget for the same period.
	ErrAlreadyExists = errors.New("budget already exists for this project+period")
)

// Store persists budgets, alert events, and throttle records.
type Store struct {
	db *sql.DB
}

// NewStore returns a new Store bound to db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Create inserts a new budget. Returns ErrAlreadyExists on (project, period) collision.
func (s *Store) Create(ctx context.Context, projectID uuid.UUID, req CreateRequest) (*Budget, error) {
	thresholds := normalizeThresholds(req.AlertThresholds)
	hardThrottle := true
	if req.HardThrottle != nil {
		hardThrottle = *req.HardThrottle
	}
	currency := req.Currency
	if currency == "" {
		currency = "USD"
	}

	b := &Budget{
		ID:              uuid.New(),
		ProjectID:       projectID,
		AmountCents:     req.AmountCents,
		Currency:        currency,
		Period:          req.Period,
		AlertThresholds: thresholds,
		HardThrottle:    hardThrottle,
	}

	query := `
		INSERT INTO budgets (id, project_id, amount_cents, currency, period, alert_thresholds, hard_throttle, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
		RETURNING created_at, updated_at
	`
	err := s.db.QueryRowContext(ctx, query,
		b.ID, b.ProjectID, b.AmountCents, b.Currency, string(b.Period),
		pq.Array(b.AlertThresholds), b.HardThrottle,
	).Scan(&b.CreatedAt, &b.UpdatedAt)

	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code.Name() == "unique_violation" {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("insert budget: %w", err)
	}

	return b, nil
}

// Update modifies a budget. Only non-nil fields on req are applied.
func (s *Store) Update(ctx context.Context, budgetID uuid.UUID, req UpdateRequest) (*Budget, error) {
	sets := []string{}
	args := []interface{}{}
	idx := 1

	if req.AmountCents != nil {
		if *req.AmountCents <= 0 {
			return nil, fmt.Errorf("amount_cents must be positive")
		}
		sets = append(sets, fmt.Sprintf("amount_cents = $%d", idx))
		args = append(args, *req.AmountCents)
		idx++
	}
	if req.AlertThresholds != nil {
		sets = append(sets, fmt.Sprintf("alert_thresholds = $%d", idx))
		args = append(args, pq.Array(normalizeThresholds(req.AlertThresholds)))
		idx++
	}
	if req.HardThrottle != nil {
		sets = append(sets, fmt.Sprintf("hard_throttle = $%d", idx))
		args = append(args, *req.HardThrottle)
		idx++
	}

	if len(sets) == 0 {
		return s.Get(ctx, budgetID)
	}

	sets = append(sets, "updated_at = now()")
	args = append(args, budgetID)

	query := fmt.Sprintf(
		"UPDATE budgets SET %s WHERE id = $%d",
		strings.Join(sets, ", "), idx,
	)
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("update budget: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}

	return s.Get(ctx, budgetID)
}

// Delete removes a budget. Returns ErrNotFound if nothing was deleted.
func (s *Store) Delete(ctx context.Context, budgetID uuid.UUID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM budgets WHERE id = $1`, budgetID)
	if err != nil {
		return fmt.Errorf("delete budget: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Get returns a single budget by id.
func (s *Store) Get(ctx context.Context, budgetID uuid.UUID) (*Budget, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project_id, amount_cents, currency, period, alert_thresholds, hard_throttle, created_at, updated_at
		FROM budgets WHERE id = $1
	`, budgetID)
	return scanBudget(row)
}

// ListByProject returns all budgets for a project, newest first.
func (s *Store) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*Budget, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, amount_cents, currency, period, alert_thresholds, hard_throttle, created_at, updated_at
		FROM budgets WHERE project_id = $1 ORDER BY created_at DESC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list budgets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*Budget
	for rows.Next() {
		b, err := scanBudget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListAll returns every budget — used by the evaluator.
func (s *Store) ListAll(ctx context.Context) ([]*Budget, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, amount_cents, currency, period, alert_thresholds, hard_throttle, created_at, updated_at
		FROM budgets ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("list all budgets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*Budget
	for rows.Next() {
		b, err := scanBudget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// InsertAlertEventIfMissing inserts a new alert event. If the (budget, period_start, threshold)
// tuple already exists it returns (existing, false, nil) without mutating.
// Returns (new, true, nil) when a fresh row was created.
func (s *Store) InsertAlertEventIfMissing(ctx context.Context, e *AlertEvent) (*AlertEvent, bool, error) {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO budget_alert_events (id, budget_id, project_id, period_start, period_end, threshold, actual_cents, budget_cents, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (budget_id, period_start, threshold) DO NOTHING
		RETURNING id, created_at
	`, e.ID, e.BudgetID, e.ProjectID, e.PeriodStart, e.PeriodEnd, e.Threshold, e.ActualCents, e.BudgetCents)

	var id uuid.UUID
	var createdAt time.Time
	if err := row.Scan(&id, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Already existed — load it for idempotent response.
			existing, loadErr := s.findAlertEvent(ctx, e.BudgetID, e.PeriodStart, e.Threshold)
			if loadErr != nil {
				return nil, false, loadErr
			}
			return existing, false, nil
		}
		return nil, false, fmt.Errorf("insert alert event: %w", err)
	}

	e.ID = id
	e.CreatedAt = createdAt
	return e, true, nil
}

func (s *Store) findAlertEvent(ctx context.Context, budgetID uuid.UUID, periodStart time.Time, threshold int) (*AlertEvent, error) {
	e := &AlertEvent{}
	var dispatchedAt sql.NullTime
	var lastErr sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, budget_id, project_id, period_start, period_end, threshold, actual_cents, budget_cents, dispatched_at, dispatch_attempts, last_error, created_at
		FROM budget_alert_events
		WHERE budget_id = $1 AND period_start = $2 AND threshold = $3
	`, budgetID, periodStart, threshold).Scan(
		&e.ID, &e.BudgetID, &e.ProjectID, &e.PeriodStart, &e.PeriodEnd, &e.Threshold,
		&e.ActualCents, &e.BudgetCents, &dispatchedAt, &e.DispatchAttempts, &lastErr, &e.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("find alert event: %w", err)
	}
	if dispatchedAt.Valid {
		e.DispatchedAt = &dispatchedAt.Time
	}
	if lastErr.Valid {
		e.LastError = lastErr.String
	}
	return e, nil
}

// MarkAlertDispatched records a successful dispatch.
func (s *Store) MarkAlertDispatched(ctx context.Context, alertID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE budget_alert_events
		SET dispatched_at = now(), dispatch_attempts = dispatch_attempts + 1, last_error = NULL
		WHERE id = $1
	`, alertID)
	return err
}

// MarkAlertFailed records a dispatch failure.
func (s *Store) MarkAlertFailed(ctx context.Context, alertID uuid.UUID, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE budget_alert_events
		SET dispatch_attempts = dispatch_attempts + 1, last_error = $2
		WHERE id = $1
	`, alertID, errMsg)
	return err
}

// ListRecentAlerts returns the newest `limit` alert events for a project.
func (s *Store) ListRecentAlerts(ctx context.Context, projectID uuid.UUID, limit int) ([]*AlertEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, budget_id, project_id, period_start, period_end, threshold, actual_cents, budget_cents, dispatched_at, dispatch_attempts, last_error, created_at
		FROM budget_alert_events
		WHERE project_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*AlertEvent
	for rows.Next() {
		e := &AlertEvent{}
		var dispatchedAt sql.NullTime
		var lastErr sql.NullString
		if err := rows.Scan(&e.ID, &e.BudgetID, &e.ProjectID, &e.PeriodStart, &e.PeriodEnd, &e.Threshold, &e.ActualCents, &e.BudgetCents, &dispatchedAt, &e.DispatchAttempts, &lastErr, &e.CreatedAt); err != nil {
			return nil, err
		}
		if dispatchedAt.Valid {
			e.DispatchedAt = &dispatchedAt.Time
		}
		if lastErr.Valid {
			e.LastError = lastErr.String
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UpsertThrottle creates an active throttle if none exists for (project, env_scope).
func (s *Store) UpsertThrottle(ctx context.Context, t *Throttle) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	if t.EnvScope == "" {
		t.EnvScope = "non-production"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO waybill_throttles (id, project_id, reason, budget_id, env_scope, activated_at, metadata)
		SELECT $1, $2, $3, $4, $5, now(), '{}'::jsonb
		WHERE NOT EXISTS (
			SELECT 1 FROM waybill_throttles
			WHERE project_id = $2 AND env_scope = $5 AND cleared_at IS NULL
		)
	`, t.ID, t.ProjectID, t.Reason, t.BudgetID, t.EnvScope)
	return err
}

// ListActiveThrottles returns throttles currently in effect for a project.
func (s *Store) ListActiveThrottles(ctx context.Context, projectID uuid.UUID) ([]*Throttle, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, reason, budget_id, env_scope, activated_at, cleared_at, cleared_by
		FROM waybill_throttles
		WHERE project_id = $1 AND cleared_at IS NULL
		ORDER BY activated_at DESC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list throttles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*Throttle
	for rows.Next() {
		t := &Throttle{}
		var budgetID uuid.NullUUID
		var clearedAt sql.NullTime
		var clearedBy uuid.NullUUID
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Reason, &budgetID, &t.EnvScope, &t.ActivatedAt, &clearedAt, &clearedBy); err != nil {
			return nil, err
		}
		if budgetID.Valid {
			t.BudgetID = &budgetID.UUID
		}
		if clearedAt.Valid {
			t.ClearedAt = &clearedAt.Time
		}
		if clearedBy.Valid {
			t.ClearedBy = &clearedBy.UUID
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RecordEvaluatorRun opens a new evaluator-run row and returns its id.
func (s *Store) RecordEvaluatorRun(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO budget_evaluator_runs (started_at) VALUES (now()) RETURNING id
	`).Scan(&id)
	return id, err
}

// FinishEvaluatorRun closes out an evaluator-run row.
func (s *Store) FinishEvaluatorRun(ctx context.Context, id int64, projects, alerts, errs int, notes string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE budget_evaluator_runs
		SET finished_at = now(), projects_evaluated = $2, alerts_fired = $3, errors = $4, notes = $5
		WHERE id = $1
	`, id, projects, alerts, errs, notes)
	return err
}

// --- helpers ---

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanBudget(s scanner) (*Budget, error) {
	b := &Budget{}
	var periodStr string
	var thresholds pq.Int64Array
	err := s.Scan(&b.ID, &b.ProjectID, &b.AmountCents, &b.Currency, &periodStr, &thresholds, &b.HardThrottle, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan budget: %w", err)
	}
	b.Period = Period(periodStr)
	b.AlertThresholds = make([]int, len(thresholds))
	for i, v := range thresholds {
		b.AlertThresholds[i] = int(v)
	}
	return b, nil
}

// normalizeThresholds dedupes, sorts ascending, and filters to positive ints.
func normalizeThresholds(xs []int) []int {
	if len(xs) == 0 {
		out := make([]int, len(DefaultThresholds))
		copy(out, DefaultThresholds)
		return out
	}
	seen := make(map[int]struct{}, len(xs))
	out := make([]int, 0, len(xs))
	for _, v := range xs {
		if v <= 0 || v > 500 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Ints(out)
	if len(out) == 0 {
		def := make([]int, len(DefaultThresholds))
		copy(def, DefaultThresholds)
		return def
	}
	return out
}
