package alerts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/madfam-org/enclii/apps/waybill/internal/budgets"
)

type fakeStore struct {
	budgets      []*budgets.Budget
	alerts       []*budgets.AlertEvent
	inserted     []*budgets.AlertEvent
	existingKeys map[string]bool // "budgetID|periodStartUnix|threshold" → already recorded
	throttles    []*budgets.Throttle
	dispatched   []uuid.UUID
	failed       map[uuid.UUID]string
	runsStarted  int
	runsFinished int
	runAlerts    int
	runErrors    int
}

func newFakeStore(budgetList []*budgets.Budget) *fakeStore {
	return &fakeStore{
		budgets:      budgetList,
		existingKeys: map[string]bool{},
		failed:       map[uuid.UUID]string{},
	}
}

func alertKey(budgetID uuid.UUID, periodStart time.Time, threshold int) string {
	return budgetID.String() + "|" + periodStart.UTC().Format(time.RFC3339) + "|" +
		itoa(threshold)
}

func itoa(x int) string {
	if x == 0 {
		return "0"
	}
	neg := x < 0
	if neg {
		x = -x
	}
	buf := [20]byte{}
	i := len(buf)
	for x > 0 {
		i--
		buf[i] = byte('0' + x%10)
		x /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func (f *fakeStore) ListAll(_ context.Context) ([]*budgets.Budget, error) {
	return f.budgets, nil
}

func (f *fakeStore) InsertAlertEventIfMissing(_ context.Context, e *budgets.AlertEvent) (*budgets.AlertEvent, bool, error) {
	k := alertKey(e.BudgetID, e.PeriodStart, e.Threshold)
	if f.existingKeys[k] {
		return e, false, nil
	}
	f.existingKeys[k] = true
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	e.CreatedAt = time.Now().UTC()
	f.inserted = append(f.inserted, e)
	f.alerts = append(f.alerts, e)
	return e, true, nil
}

func (f *fakeStore) MarkAlertDispatched(_ context.Context, id uuid.UUID) error {
	f.dispatched = append(f.dispatched, id)
	return nil
}

func (f *fakeStore) MarkAlertFailed(_ context.Context, id uuid.UUID, msg string) error {
	f.failed[id] = msg
	return nil
}

func (f *fakeStore) UpsertThrottle(_ context.Context, t *budgets.Throttle) error {
	f.throttles = append(f.throttles, t)
	return nil
}

func (f *fakeStore) RecordEvaluatorRun(_ context.Context) (int64, error) {
	f.runsStarted++
	return int64(f.runsStarted), nil
}

func (f *fakeStore) FinishEvaluatorRun(_ context.Context, _ int64, _, alertsFired, errs int, _ string) error {
	f.runsFinished++
	f.runAlerts = alertsFired
	f.runErrors = errs
	return nil
}

// staticCost returns a fixed total for every call, useful for crossing tests.
type staticCost struct {
	cents int64
	err   error
}

func (s staticCost) ProjectCost(_ context.Context, _ uuid.UUID, _, _ time.Time) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.cents, nil
}

type recordingDispatcher struct {
	calls    []DispatchPayload
	err      error
	errAfter int // if >0, first N-1 calls succeed then nth returns err
}

func (r *recordingDispatcher) Dispatch(_ context.Context, p DispatchPayload) error {
	r.calls = append(r.calls, p)
	if r.errAfter > 0 && len(r.calls) >= r.errAfter {
		return r.err
	}
	return r.err
}

// --- tests ---

func TestEvaluatorFiresBelowThreshold49(t *testing.T) {
	b := makeBudget(100_00, []int{50, 80, 100}) // $100 budget
	store := newFakeStore([]*budgets.Budget{b})
	disp := &recordingDispatcher{}
	e := NewEvaluator(nil, store, staticCost{cents: 49_00}, disp, zap.NewNop(), Config{Interval: time.Hour, Clock: fixedClock{now: clockNow()}})
	n, err := e.RunOnce(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("expected 0 alerts at 49%%, got n=%d err=%v", n, err)
	}
	if len(disp.calls) != 0 {
		t.Fatalf("dispatcher must not be called below threshold")
	}
}

func TestEvaluatorFires51Crosses50(t *testing.T) {
	b := makeBudget(100_00, []int{50, 80, 100})
	store := newFakeStore([]*budgets.Budget{b})
	disp := &recordingDispatcher{}
	e := NewEvaluator(nil, store, staticCost{cents: 51_00}, disp, zap.NewNop(), Config{Clock: fixedClock{now: clockNow()}})
	n, _ := e.RunOnce(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 fired at 51%%, got %d", n)
	}
	if disp.calls[0].ThresholdCrossed != 50 {
		t.Fatalf("expected 50%% threshold, got %d", disp.calls[0].ThresholdCrossed)
	}
}

func TestEvaluatorFires81Crosses50And80(t *testing.T) {
	b := makeBudget(100_00, []int{50, 80, 100})
	store := newFakeStore([]*budgets.Budget{b})
	disp := &recordingDispatcher{}
	e := NewEvaluator(nil, store, staticCost{cents: 81_00}, disp, zap.NewNop(), Config{Clock: fixedClock{now: clockNow()}})
	n, _ := e.RunOnce(context.Background())
	if n != 2 {
		t.Fatalf("expected 2 fired, got %d", n)
	}
}

func TestEvaluatorFires101HitsAllThreeAndThrottles(t *testing.T) {
	b := makeBudget(100_00, []int{50, 80, 100})
	b.HardThrottle = true
	store := newFakeStore([]*budgets.Budget{b})
	disp := &recordingDispatcher{}
	e := NewEvaluator(nil, store, staticCost{cents: 101_00}, disp, zap.NewNop(), Config{Clock: fixedClock{now: clockNow()}})
	n, _ := e.RunOnce(context.Background())
	if n != 3 {
		t.Fatalf("expected 3 fired, got %d", n)
	}
	if len(store.throttles) != 1 {
		t.Fatalf("expected a throttle row to be written")
	}
	if store.throttles[0].EnvScope != "non-production" {
		t.Fatalf("throttle must scope to non-production, got %s", store.throttles[0].EnvScope)
	}
}

func TestEvaluatorIdempotentOnSecondRun(t *testing.T) {
	b := makeBudget(100_00, []int{50, 80, 100})
	store := newFakeStore([]*budgets.Budget{b})
	disp := &recordingDispatcher{}
	e := NewEvaluator(nil, store, staticCost{cents: 81_00}, disp, zap.NewNop(), Config{Clock: fixedClock{now: clockNow()}})
	_, _ = e.RunOnce(context.Background())
	n2, _ := e.RunOnce(context.Background())
	if n2 != 0 {
		t.Fatalf("second run must not re-fire, got %d", n2)
	}
	if len(disp.calls) != 2 {
		t.Fatalf("dispatcher called %d times, expected 2 (one per threshold)", len(disp.calls))
	}
}

func TestEvaluatorDispatchFailureMarksAlert(t *testing.T) {
	b := makeBudget(100_00, []int{50})
	store := newFakeStore([]*budgets.Budget{b})
	disp := &recordingDispatcher{err: errors.New("boom")}
	e := NewEvaluator(nil, store, staticCost{cents: 80_00}, disp, zap.NewNop(), Config{Clock: fixedClock{now: clockNow()}})
	_, _ = e.RunOnce(context.Background())
	if len(store.failed) != 1 {
		t.Fatalf("expected 1 failed row, got %d", len(store.failed))
	}
}

func TestEvaluatorCrossingFromNewPeriodReFires(t *testing.T) {
	b := makeBudget(100_00, []int{50})
	store := newFakeStore([]*budgets.Budget{b})
	disp := &recordingDispatcher{}

	clk := &mutableClock{now: time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)}
	e := NewEvaluator(nil, store, staticCost{cents: 55_00}, disp, zap.NewNop(), Config{Clock: clk})
	n1, _ := e.RunOnce(context.Background())
	if n1 != 1 {
		t.Fatalf("expected 1 fire in first period, got %d", n1)
	}

	// Roll over into May: new (budget_id, period_start) tuple → re-fires.
	clk.now = time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
	n2, _ := e.RunOnce(context.Background())
	if n2 != 1 {
		t.Fatalf("expected 1 fire in new period, got %d", n2)
	}
}

func TestPercentCentsEdgeCases(t *testing.T) {
	if percentCents(0, 0) != 0 {
		t.Fatal("0/0 must be 0")
	}
	if percentCents(50, 100) != 50 {
		t.Fatal("50/100 must be 50")
	}
	if percentCents(-5, 100) != 0 {
		t.Fatal("negative actual must clamp to 0")
	}
	if percentCents(5000, 10) != 10000 {
		t.Fatal("extreme overages must clamp to 10000")
	}
}

func TestHTTPDispatcherSendsSignedEnvelope(t *testing.T) {
	var gotSig string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Madfam-Signature")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = buf
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewHTTPDispatcher(srv.URL, "shh-secret", zap.NewNop())
	err := d.Dispatch(context.Background(), DispatchPayload{
		AlertID:          uuid.New(),
		ProjectID:        uuid.New(),
		BudgetID:         uuid.New(),
		Period:           "monthly",
		PeriodStart:      time.Now(),
		PeriodEnd:        time.Now().Add(30 * 24 * time.Hour),
		ThresholdCrossed: 80,
		ActualCents:      80_00,
		BudgetCents:      100_00,
		Currency:         "USD",
	})
	if err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
	if gotSig == "" {
		t.Fatal("no signature sent")
	}
	if len(gotBody) == 0 {
		t.Fatal("empty body")
	}
}

func TestHTTPDispatcher500Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	d := NewHTTPDispatcher(srv.URL, "x", zap.NewNop())
	err := d.Dispatch(context.Background(), DispatchPayload{})
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

// --- helpers ---

func makeBudget(amountCents int64, thresholds []int) *budgets.Budget {
	return &budgets.Budget{
		ID:              uuid.New(),
		ProjectID:       uuid.New(),
		AmountCents:     amountCents,
		Currency:        "USD",
		Period:          budgets.PeriodMonthly,
		AlertThresholds: thresholds,
		HardThrottle:    true,
	}
}

type fixedClock struct{ now time.Time }

func (f fixedClock) Now() time.Time { return f.now }

type mutableClock struct{ now time.Time }

func (m *mutableClock) Now() time.Time { return m.now }

func clockNow() time.Time { return time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC) }
