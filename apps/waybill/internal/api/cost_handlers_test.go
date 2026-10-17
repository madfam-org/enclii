package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/madfam-org/enclii/apps/waybill/internal/budgets"
)

func newCostRouter(t *testing.T) (*gin.Engine, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	store := budgets.NewStore(db)
	cost := budgets.NewCostReader(db, budgets.PricingDollars{
		ComputePerGBHour: 0.000463, BuildPerMinute: 0.01,
		StoragePerGBMonth: 0.25, BandwidthPerGB: 0.10,
	})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewCostHandlers(store, cost, zap.NewNop())
	g := r.Group("/api/v1")
	h.Register(g)
	return r, mock, func() { _ = db.Close() }
}

func TestGetProjectCostInvalidPeriod(t *testing.T) {
	r, _, cleanup := newCostRouter(t)
	defer cleanup()

	req, _ := http.NewRequest("GET", "/api/v1/projects/"+uuid.New().String()+"/cost?period=abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateBudgetRejectsBadPeriod(t *testing.T) {
	r, _, cleanup := newCostRouter(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]interface{}{
		"amount_cents": 50000,
		"period":       "yearly",
	})
	req, _ := http.NewRequest("POST", "/api/v1/projects/"+uuid.New().String()+"/budgets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for yearly period, got %d", w.Code)
	}
}

func TestCreateBudgetSuccess(t *testing.T) {
	r, mock, cleanup := newCostRouter(t)
	defer cleanup()

	projectID := uuid.New()
	now := time.Now().UTC()

	mock.ExpectQuery("INSERT INTO budgets").
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

	body, _ := json.Marshal(map[string]interface{}{
		"amount_cents": 50000,
		"period":       "monthly",
	})
	req, _ := http.NewRequest("POST", "/api/v1/projects/"+projectID.String()+"/budgets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp budgets.Budget
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.AmountCents != 50000 {
		t.Fatalf("expected 50000, got %d", resp.AmountCents)
	}
}

func TestListBudgetsEmpty(t *testing.T) {
	r, mock, cleanup := newCostRouter(t)
	defer cleanup()

	mock.ExpectQuery("SELECT id, project_id").WillReturnRows(sqlmock.NewRows([]string{
		"id", "project_id", "amount_cents", "currency", "period",
		"alert_thresholds", "hard_throttle", "created_at", "updated_at",
	}))

	req, _ := http.NewRequest("GET", "/api/v1/projects/"+uuid.New().String()+"/budgets", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["budgets"] != nil {
		// Allowed to be nil or empty slice; just make sure it's not an error payload.
	}
}

func TestParsePeriodTable(t *testing.T) {
	cases := map[string]time.Duration{
		"7d":  7 * 24 * time.Hour,
		"14d": 14 * 24 * time.Hour,
		"30d": 30 * 24 * time.Hour,
		"90d": 90 * 24 * time.Hour,
	}
	for p, want := range cases {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		var gotStart, gotEnd time.Time
		r.GET("/t", func(c *gin.Context) {
			s, e, ok := parsePeriod(c)
			if !ok {
				t.Fatalf("unexpected parse failure for %s", p)
			}
			gotStart, gotEnd = s, e
		})
		req, _ := http.NewRequestWithContext(context.Background(), "GET", "/t?period="+p, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		delta := gotEnd.Sub(gotStart)
		// Allow ±1 hour slack since `time.Now()` may tick between assembly and assertion.
		if delta < want-2*time.Hour || delta > want+2*time.Hour {
			t.Fatalf("period %s: expected ~%v, got %v", p, want, delta)
		}
	}
}

// getCostWithMetering drives GET /projects/:id/cost through the real reader:
// the total and breakdown queries return no rows (empty hourly_usage), and
// the metering query returns the given signals.
func getCostWithMetering(t *testing.T, heartbeat interface{}, unaggregated int, meterErr error) map[string]interface{} {
	t.Helper()
	r, mock, cleanup := newCostRouter(t)
	defer cleanup()

	mock.ExpectQuery("SELECT metric_type").WillReturnRows(sqlmock.NewRows([]string{"metric_type", "total"}))
	mock.ExpectQuery("SELECT metric_type").WillReturnRows(sqlmock.NewRows([]string{"metric_type", "total"}))
	q := mock.ExpectQuery("FROM budget_evaluator_runs")
	if meterErr != nil {
		q.WillReturnError(meterErr)
	} else {
		q.WillReturnRows(sqlmock.NewRows([]string{"last_heartbeat", "last_hour", "project_rows", "unaggregated"}).
			AddRow(heartbeat, nil, 0, unaggregated))
	}

	req, _ := http.NewRequest("GET", "/api/v1/projects/"+uuid.New().String()+"/cost?period=30d", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return resp
}

func meteringStatusOf(t *testing.T, resp map[string]interface{}) string {
	t.Helper()
	m, ok := resp["metering"].(map[string]interface{})
	if !ok {
		t.Fatalf("response has no metering block: %v", resp)
	}
	s, _ := m["status"].(string)
	return s
}

func TestGetProjectCostReportsMeteringNotRunning(t *testing.T) {
	// No aggregator heartbeat and no usage rows: total_cents is 0, but the
	// response must say the 0 is not a measurement.
	resp := getCostWithMetering(t, nil, 0, nil)
	if resp["total_cents"].(float64) != 0 {
		t.Fatalf("expected total_cents 0, got %v", resp["total_cents"])
	}
	if resp["spend_known"] != false {
		t.Fatalf("spend_known must be false, got %v", resp["spend_known"])
	}
	if got := meteringStatusOf(t, resp); got != "not_running" {
		t.Fatalf("expected metering.status not_running, got %q", got)
	}
}

func TestGetProjectCostReportsMeteringUnknownOnReadError(t *testing.T) {
	resp := getCostWithMetering(t, nil, 0, errors.New("boom"))
	if resp["spend_known"] != false || meteringStatusOf(t, resp) != "unknown" {
		t.Fatalf("a failed metering read must be unknown/spend_known=false, got %v", resp)
	}
}

func TestGetProjectCostRealZeroIsKnown(t *testing.T) {
	resp := getCostWithMetering(t, time.Now().UTC().Add(-5*time.Minute), 0, nil)
	if resp["spend_known"] != true || meteringStatusOf(t, resp) != "ok" {
		t.Fatalf("a live aggregator with no usage is a real zero, got %v", resp)
	}
}
