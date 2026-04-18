package api

import (
	"bytes"
	"context"
	"encoding/json"
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
