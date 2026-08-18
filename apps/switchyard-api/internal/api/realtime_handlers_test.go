package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// TestEnableRealtimeTableBadUUID confirms a non-UUID addon id is a 400 before
// any service is touched.
func TestEnableRealtimeTableBadUUID(t *testing.T) {
	h := &Handler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "not-a-uuid"}}
	c.Request, _ = http.NewRequest("POST", "/v1/addons/not-a-uuid/realtime/tables", bytes.NewReader([]byte(`{"table":"orders"}`)))

	h.EnableAddonRealtimeTable(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestEnableRealtimeTableNotConfigured surfaces 503 when the realtime manager
// is not wired, even with a valid UUID.
func TestEnableRealtimeTableNotConfigured(t *testing.T) {
	h := &Handler{} // realtimeManager is nil
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	c.Request, _ = http.NewRequest("POST", "/v1/addons/x/realtime/tables", bytes.NewReader([]byte(`{"table":"orders"}`)))

	h.EnableAddonRealtimeTable(c)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestListRealtimeTablesNotConfigured surfaces 503 when the manager is nil.
func TestListRealtimeTablesNotConfigured(t *testing.T) {
	h := &Handler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	c.Request, _ = http.NewRequest("GET", "/v1/addons/x/realtime/tables", nil)

	h.ListAddonRealtimeTables(c)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestDisableRealtimeTableBadUUID confirms a non-UUID addon id is a 400.
func TestDisableRealtimeTableBadUUID(t *testing.T) {
	h := &Handler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{
		{Key: "id", Value: "nope"},
		{Key: "schema", Value: "public"},
		{Key: "table", Value: "orders"},
	}
	c.Request, _ = http.NewRequest("DELETE", "/v1/addons/nope/realtime/tables/public/orders", nil)

	h.DisableAddonRealtimeTable(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestStreamRealtimeBadUUID confirms the WS entry point rejects a bad addon id
// before attempting an upgrade.
func TestStreamRealtimeBadUUID(t *testing.T) {
	h := &Handler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "not-a-uuid"}}
	c.Request, _ = http.NewRequest("GET", "/v1/projects/p/addons/not-a-uuid/realtime", nil)

	h.StreamAddonRealtime(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
