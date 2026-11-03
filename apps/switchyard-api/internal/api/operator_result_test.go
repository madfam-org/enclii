package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeOperatorResult is the only place an operator response leaves the
// handler. api.enclii.dev sits behind Cloudflare, which swaps an origin 502 or
// 504 body for its own error page, so those statuses must not leave as-is.
func newOperatorResultHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logFile := filepath.Join(t.TempDir(), "switchyard.log")
	logger, err := logging.NewStructuredLogger(&logging.LogConfig{Level: "debug", Format: "json", Output: logFile})
	require.NoError(t, err)
	return &Handler{logger: logger}, logFile
}

func writeResult(t *testing.T, h *Handler, status int, resp operatorOperationResponse) (*httptest.ResponseRecorder, operatorOperationResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/ops/addons/backup-credential", nil)
	h.writeOperatorResult(c, status, resp)
	var got operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return rec, got
}

func TestWriteOperatorResultSendsUpstreamFailuresAs424WithTheirSummary(t *testing.T) {
	for _, upstream := range []int{http.StatusBadGateway, http.StatusGatewayTimeout} {
		h, logFile := newOperatorResultHandler(t)
		summary := "mint bucket-scoped token: cloudflare mint R2 API token failed: invalid policy (code 1001)"
		rec, got := writeResult(t, h, upstream, operatorOperationResponse{
			Operation: "ops.addons.backup-credential", AuditID: "aud-1", Status: "failed", Summary: summary,
		})

		assert.Equal(t, http.StatusFailedDependency, rec.Code)
		assert.Equal(t, summary, got.Summary, "the client gets the handler's own reason")
		assert.Equal(t, "failed", got.Status)
		require.Len(t, got.Warnings, 1)
		assert.Contains(t, got.Warnings[0], "reported as HTTP "+strconv.Itoa(upstream))

		logs, err := os.ReadFile(logFile)
		require.NoError(t, err)
		assert.Contains(t, string(logs), "Operator operation failed upstream")
		assert.Contains(t, string(logs), "invalid policy (code 1001)", "the reason survives in the server log too")
		assert.Contains(t, string(logs), "aud-1")
	}
}

func TestWriteOperatorResultPassesOtherStatusesThroughUntouched(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusConflict, http.StatusFailedDependency, http.StatusServiceUnavailable} {
		h, logFile := newOperatorResultHandler(t)
		rec, got := writeResult(t, h, status, operatorOperationResponse{Operation: "ops.pods.restart", Status: "x", Summary: "s"})

		assert.Equal(t, status, rec.Code)
		assert.Empty(t, got.Warnings)
		logs, err := os.ReadFile(logFile)
		if err == nil {
			assert.NotContains(t, string(logs), "Operator operation failed upstream")
		}
	}
}

func TestWriteOperatorResultToleratesANilLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec, got := writeResult(t, &Handler{}, http.StatusBadGateway, operatorOperationResponse{Operation: "ops.x.y", Summary: "s"})
	assert.Equal(t, http.StatusFailedDependency, rec.Code)
	assert.Equal(t, "s", got.Summary)
}
