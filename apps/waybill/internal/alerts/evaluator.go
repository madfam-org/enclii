// Package alerts computes budget threshold crossings, persists them, and
// dispatches them to Dhanam over a signed HTTP boundary.
//
// The evaluator runs on an interval (default 15 min) and is intentionally
// idempotent:
//
//   - Alert events are uniqued by (budget_id, period_start, threshold);
//     a re-run in the same period never emits duplicates.
//   - On webhook failure the row stays un-dispatched, so the next tick
//     retries with the same (stable) idempotency tuple. Dhanam is
//     responsible for final dedup using the same tuple.
//
// The loop is single-process safe via a simple mutex. A future deploy with
// multiple replicas should wrap Run() in a Postgres advisory lock.
package alerts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/madfam-org/enclii/apps/waybill/internal/budgets"
)

// CostQuery is the minimal interface the evaluator needs from a cost reader.
// It mirrors budgets.CostReader.ProjectCost and keeps tests hermetic.
type CostQuery interface {
	ProjectCost(ctx context.Context, projectID uuid.UUID, start, end time.Time) (int64, error)
}

// BudgetStore is the slice of budgets.Store the evaluator depends on.
type BudgetStore interface {
	ListAll(ctx context.Context) ([]*budgets.Budget, error)
	InsertAlertEventIfMissing(ctx context.Context, e *budgets.AlertEvent) (*budgets.AlertEvent, bool, error)
	MarkAlertDispatched(ctx context.Context, alertID uuid.UUID) error
	MarkAlertFailed(ctx context.Context, alertID uuid.UUID, errMsg string) error
	UpsertThrottle(ctx context.Context, t *budgets.Throttle) error
	RecordEvaluatorRun(ctx context.Context) (int64, error)
	FinishEvaluatorRun(ctx context.Context, id int64, projects, alerts, errs int, notes string) error
}

// Evaluator is the periodic budget alert job.
type Evaluator struct {
	db         *sql.DB
	store      BudgetStore
	cost       CostQuery
	dispatcher Dispatcher
	logger     *zap.Logger
	interval   time.Duration
	clock      Clock

	mu      sync.Mutex
	running bool
}

// Clock is injected so tests can fast-forward.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// Config bundles the evaluator's knobs.
type Config struct {
	Interval time.Duration
	Clock    Clock
}

// NewEvaluator builds an Evaluator. Pass cfg.Interval = 0 to use 15m default.
func NewEvaluator(db *sql.DB, store BudgetStore, cost CostQuery, disp Dispatcher, logger *zap.Logger, cfg Config) *Evaluator {
	interval := cfg.Interval
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	return &Evaluator{
		db:         db,
		store:      store,
		cost:       cost,
		dispatcher: disp,
		logger:     logger,
		interval:   interval,
		clock:      clock,
	}
}

// RunOnce performs a single pass over all budgets. Safe to call concurrently;
// a mutex prevents overlapping executions within this process.
func (e *Evaluator) RunOnce(ctx context.Context) (fired int, err error) {
	if !e.tryLock() {
		e.logger.Info("skipping evaluator run: previous run still in progress")
		return 0, nil
	}
	defer e.unlock()

	runID, err := e.store.RecordEvaluatorRun(ctx)
	if err != nil {
		e.logger.Error("failed to record evaluator run", zap.Error(err))
	}

	budgetList, err := e.store.ListAll(ctx)
	if err != nil {
		return 0, fmt.Errorf("list budgets: %w", err)
	}

	projectsEvaluated := 0
	errCount := 0
	for _, b := range budgetList {
		n, err := e.evaluateBudget(ctx, b)
		if err != nil {
			e.logger.Error("budget evaluation failed",
				zap.String("budget_id", b.ID.String()),
				zap.String("project_id", b.ProjectID.String()),
				zap.Error(err),
			)
			errCount++
			continue
		}
		fired += n
		projectsEvaluated++
	}

	if runID > 0 {
		if err := e.store.FinishEvaluatorRun(ctx, runID, projectsEvaluated, fired, errCount, ""); err != nil {
			e.logger.Warn("finish evaluator run", zap.Error(err))
		}
	}
	return fired, nil
}

// evaluateBudget fires alerts for any thresholds the project has newly crossed.
// Returns the number of new alert events emitted.
func (e *Evaluator) evaluateBudget(ctx context.Context, b *budgets.Budget) (int, error) {
	periodStart, periodEnd := budgets.PeriodBounds(b.Period, e.clock.Now())
	actualCents, err := e.cost.ProjectCost(ctx, b.ProjectID, periodStart, periodEnd)
	if err != nil {
		return 0, fmt.Errorf("project cost: %w", err)
	}

	pct := percentCents(actualCents, b.AmountCents)
	fired := 0

	for _, threshold := range b.AlertThresholds {
		if pct < threshold {
			continue
		}
		event := &budgets.AlertEvent{
			BudgetID:    b.ID,
			ProjectID:   b.ProjectID,
			PeriodStart: periodStart,
			PeriodEnd:   periodEnd,
			Threshold:   threshold,
			ActualCents: actualCents,
			BudgetCents: b.AmountCents,
		}
		saved, created, err := e.store.InsertAlertEventIfMissing(ctx, event)
		if err != nil {
			return fired, err
		}
		if !created {
			// Already recorded this period — nothing new to dispatch.
			continue
		}
		fired++
		e.logger.Info("budget threshold crossed",
			zap.String("project_id", b.ProjectID.String()),
			zap.String("budget_id", b.ID.String()),
			zap.Int("threshold", threshold),
			zap.Int64("actual_cents", actualCents),
			zap.Int64("budget_cents", b.AmountCents),
		)

		if err := e.dispatchAlert(ctx, b, saved); err != nil {
			e.logger.Warn("dispatch failed; will retry on next tick",
				zap.String("alert_id", saved.ID.String()),
				zap.Error(err),
			)
			_ = e.store.MarkAlertFailed(ctx, saved.ID, err.Error())
		} else {
			_ = e.store.MarkAlertDispatched(ctx, saved.ID)
		}

		// 100% crossing → non-production throttle if enabled.
		if threshold >= 100 && b.HardThrottle {
			bid := b.ID
			err := e.store.UpsertThrottle(ctx, &budgets.Throttle{
				ProjectID: b.ProjectID,
				Reason:    "budget_exceeded",
				BudgetID:  &bid,
				EnvScope:  "non-production",
			})
			if err != nil {
				e.logger.Error("failed to write throttle row", zap.Error(err))
			}
		}
	}
	return fired, nil
}

func (e *Evaluator) dispatchAlert(ctx context.Context, b *budgets.Budget, ev *budgets.AlertEvent) error {
	if e.dispatcher == nil {
		return nil
	}
	return e.dispatcher.Dispatch(ctx, DispatchPayload{
		AlertID:          ev.ID,
		ProjectID:        ev.ProjectID,
		BudgetID:         b.ID,
		Period:           string(b.Period),
		PeriodStart:      ev.PeriodStart,
		PeriodEnd:        ev.PeriodEnd,
		ThresholdCrossed: ev.Threshold,
		ActualCents:      ev.ActualCents,
		BudgetCents:      ev.BudgetCents,
		Currency:         b.Currency,
	})
}

func (e *Evaluator) tryLock() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return false
	}
	e.running = true
	return true
}

func (e *Evaluator) unlock() {
	e.mu.Lock()
	e.running = false
	e.mu.Unlock()
}

// Start begins the periodic loop and returns a cancel func.
func (e *Evaluator) Start(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	ticker := time.NewTicker(e.interval)
	go func() {
		// Run once immediately so fresh deploys don't wait the full tick.
		if _, err := e.RunOnce(ctx); err != nil {
			e.logger.Error("initial evaluator run failed", zap.Error(err))
		}
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				if _, err := e.RunOnce(ctx); err != nil {
					e.logger.Error("evaluator tick failed", zap.Error(err))
				}
			}
		}
	}()
	return cancel
}

// --- dispatcher ---

// Dispatcher delivers an alert to Dhanam.
type Dispatcher interface {
	Dispatch(ctx context.Context, payload DispatchPayload) error
}

// DispatchPayload is the JSON body Waybill sends to Dhanam.
type DispatchPayload struct {
	AlertID          uuid.UUID `json:"alert_id"`
	ProjectID        uuid.UUID `json:"project_id"`
	BudgetID         uuid.UUID `json:"budget_id"`
	Period           string    `json:"period"`
	PeriodStart      time.Time `json:"period_start"`
	PeriodEnd        time.Time `json:"period_end"`
	ThresholdCrossed int       `json:"threshold_crossed"`
	ActualCents      int64     `json:"actual_cents"`
	BudgetCents      int64     `json:"budget_cents"`
	Currency         string    `json:"currency"`
}

// HTTPDispatcher is the production dispatcher. It posts to Dhanam's
// /billing/usage-alerts/ingest endpoint with an HMAC-SHA256 signature
// matching the MADFAM ecosystem signature format:
//
//	X-Madfam-Signature: t=<unix-seconds>,v1=<hex-hmac-sha256>
//
// where the HMAC is computed over `${timestamp}.${rawBody}`.
type HTTPDispatcher struct {
	endpoint string
	secret   string
	client   *http.Client
	logger   *zap.Logger
}

// NewHTTPDispatcher returns a dispatcher pointed at Dhanam. endpoint should
// include the full path (e.g. https://dhanam.io/api/v1/billing/usage-alerts/ingest).
func NewHTTPDispatcher(endpoint, secret string, logger *zap.Logger) *HTTPDispatcher {
	return &HTTPDispatcher{
		endpoint: endpoint,
		secret:   secret,
		client:   &http.Client{Timeout: 10 * time.Second},
		logger:   logger,
	}
}

// Dispatch POSTs the payload to Dhanam with a signed envelope.
func (d *HTTPDispatcher) Dispatch(ctx context.Context, payload DispatchPayload) error {
	if d.endpoint == "" {
		return fmt.Errorf("dispatcher endpoint not configured")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	ts := time.Now().Unix()
	sig := signMadfam(string(body), d.secret, ts)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Madfam-Signature", sig)
	req.Header.Set("User-Agent", "waybill-budget-evaluator/1.0")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("dhanam returned status %d", resp.StatusCode)
	}
	return nil
}

// signMadfam produces the `t=...,v1=...` signature header value.
func signMadfam(body, secret string, ts int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", ts, body)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

// NoopDispatcher is used when no endpoint is configured. It records a warning
// once so operators know alerts are being suppressed.
type NoopDispatcher struct {
	logger *zap.Logger
	warned sync.Once
}

// NewNoopDispatcher returns a dispatcher that discards alerts.
func NewNoopDispatcher(logger *zap.Logger) *NoopDispatcher {
	return &NoopDispatcher{logger: logger}
}

// Dispatch always returns nil; logs once.
func (d *NoopDispatcher) Dispatch(_ context.Context, _ DispatchPayload) error {
	d.warned.Do(func() {
		d.logger.Warn("alert dispatcher is a noop — set WAYBILL_ALERT_ENDPOINT and WAYBILL_ALERT_SIGNING_KEY to enable delivery")
	})
	return nil
}

// percentCents computes floor(100 * actual / budget) with bounds [0, 10000].
// Reported as an int percent so a 79.9% value never rings the 80% bell.
func percentCents(actual, budget int64) int {
	if budget <= 0 {
		return 0
	}
	p := (actual * 100) / budget
	if p < 0 {
		return 0
	}
	if p > 10000 {
		return 10000
	}
	return int(p)
}

// Normalized event payload identifier used by log-scraping / tests.
func (p DispatchPayload) LogKey() string {
	return strings.Join([]string{
		p.ProjectID.String(),
		p.BudgetID.String(),
		p.Period,
		fmt.Sprintf("%d", p.ThresholdCrossed),
	}, ":")
}
