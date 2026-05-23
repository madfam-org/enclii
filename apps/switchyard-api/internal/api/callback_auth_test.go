package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
)

func TestVerifyRoundhouseCallbackAuth_ProductionRequiresKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{
		config: &config.Config{
			Environment:      "production",
			RoundhouseAPIKey: "",
		},
		logger: testLogger(t),
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/v1/callbacks/build-complete", nil)
	c.Request = req

	if h.verifyRoundhouseCallbackAuth(c) {
		t.Fatal("expected auth failure when key unset in production")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestVerifyRoundhouseCallbackAuth_ValidKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{
		config: &config.Config{
			Environment:      "production",
			RoundhouseAPIKey: "secret",
		},
		logger: testLogger(t),
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/v1/callbacks/build-complete", nil)
	req.Header.Set("Authorization", "Bearer secret")
	c.Request = req

	if !h.verifyRoundhouseCallbackAuth(c) {
		t.Fatalf("expected success, body=%s", w.Body.String())
	}
}
