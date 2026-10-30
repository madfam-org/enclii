package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestJanuaBatchAApplyRemainsHeldBeforeAnyNativeIO(t *testing.T) {
	require.False(t, januaBatchAApplySupported, "native writes need a separate reviewed activation change")
	gin.SetMode(gin.TestMode)
	for _, phase := range []string{"reserve", "namespaces", "provision", "custody"} {
		t.Run(phase, func(t *testing.T) {
			h := &Handler{} // No DB/Kubernetes/Vault client can be invoked.
			router := gin.New()
			router.POST("/projects/:slug/prepare", func(c *gin.Context) { c.Set("user_id", uuid.NewString()) }, h.JanuaStagingBatchAPrepare)
			body := `{"phase":"` + phase + `","plan":"standard-0","operation_id":"` + uuid.NewString() + `","dry_run":false,"expected_plan":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","reason":"reviewed staging prerequisite"}`
			request := httptest.NewRequest(http.MethodPost, "/projects/janua/prepare", bytes.NewBufferString(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusConflict, response.Code)
			require.Contains(t, response.Body.String(), `"apply_supported":false`)
		})
	}
}

func TestJanuaBatchARejectsCallerResourceOrCredentialOverrides(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &Handler{}
	router.POST("/projects/:slug/prepare", func(c *gin.Context) { c.Set("user_id", uuid.NewString()) }, h.JanuaStagingBatchAPrepare)
	for _, field := range []string{"namespace", "image", "database_url", "runtime_role", "sql", "source_app"} {
		body := `{"phase":"reserve","plan":"standard-0","operation_id":"` + uuid.NewString() + `","` + field + `":"refused"}`
		request := httptest.NewRequest(http.MethodPost, "/projects/janua/prepare", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusBadRequest, response.Code, field)
	}
}
