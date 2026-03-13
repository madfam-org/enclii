package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/waybill/internal/metering"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupTestRouter() (*gin.Engine, *Handlers) {
	gin.SetMode(gin.TestMode)
	h := NewHandlers(nil, metering.NewCalculator(nil, metering.DefaultPricing(), zap.NewNop()), zap.NewNop())
	r := gin.New()
	r.GET("/health", h.HealthCheck)
	r.GET("/plans", h.GetPlans)
	r.GET("/projects/:project_id/invoices", h.GetInvoices)
	r.POST("/estimate-cost", h.EstimateCost)
	r.POST("/events", h.RecordEvent)
	r.POST("/events/batch", h.RecordEventBatch)
	r.GET("/projects/:project_id/usage", h.GetCurrentUsage)
	return r, h
}

func TestHealthCheck(t *testing.T) {
	r, _ := setupTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "healthy", resp["status"])
	assert.Equal(t, "waybill", resp["service"])
}

func TestGetPlans(t *testing.T) {
	r, _ := setupTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/plans", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	plans, ok := resp["plans"].([]interface{})
	require.True(t, ok)
	assert.Len(t, plans, 3)

	// Check plan IDs
	plan0 := plans[0].(map[string]interface{})
	assert.Equal(t, "hobby", plan0["id"])
	plan1 := plans[1].(map[string]interface{})
	assert.Equal(t, "pro", plan1["id"])
	plan2 := plans[2].(map[string]interface{})
	assert.Equal(t, "team", plan2["id"])
}

func TestGetInvoicesValidProject(t *testing.T) {
	r, _ := setupTestRouter()
	projectID := uuid.New()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/projects/"+projectID.String()+"/invoices", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, projectID.String(), resp["project_id"])

	invoices, ok := resp["invoices"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, invoices)
}

func TestGetInvoicesInvalidProject(t *testing.T) {
	r, _ := setupTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/projects/not-a-uuid/invoices", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestEstimateCost(t *testing.T) {
	r, _ := setupTestRouter()

	body, _ := json.Marshal(metering.ResourceSpecs{
		Replicas:        2,
		CPUMillicores:   500,
		MemoryMB:        512,
		StorageGB:       10,
		AvgBuildMinutes: 5,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/estimate-cost", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp metering.CostEstimate
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Greater(t, resp.TotalMonthly, 0.0)
	assert.Greater(t, resp.ComputeMonthly, 0.0)
	assert.Greater(t, resp.StorageMonthly, 0.0)
	assert.Greater(t, resp.BuildCost, 0.0)
}

func TestEstimateCostBadRequest(t *testing.T) {
	r, _ := setupTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/estimate-cost", bytes.NewReader([]byte(`{invalid`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRecordEventBadRequest(t *testing.T) {
	r, _ := setupTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/events", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRecordEventBatchBadRequest(t *testing.T) {
	r, _ := setupTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/events/batch", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetCurrentUsageInvalidProject(t *testing.T) {
	r, _ := setupTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/projects/bad-uuid/usage", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
