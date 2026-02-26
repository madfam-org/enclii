package middleware

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newRouter creates a test router with ErrorHandlerMiddleware, registers a single
// GET handler, and returns it.  This exercises the full Gin pipeline so that
// AbortWithAppError → c.Error → ErrorHandlerMiddleware → JSON response works end-to-end.
func newRouter(path string, handler gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.Use(ErrorHandlerMiddleware(nil))
	r.GET(path, handler)
	return r
}

func doGET(router *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", path, nil)
	router.ServeHTTP(w, req)
	return w
}

func doPOST(router *gin.Engine, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

// parseErrorBody extracts the nested {"error": {"code": ..., "message": ...}} structure.
func parseErrorBody(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var outer map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &outer))
	errObj, ok := outer["error"].(map[string]interface{})
	if !ok {
		return outer // recovery middleware uses flat structure
	}
	return errObj
}

// --- AbortInternal: must never leak internal error details ---

func TestAbortInternal_ReturnsGenericMessage(t *testing.T) {
	r := newRouter("/test", func(c *gin.Context) {
		AbortInternal(c, fmt.Errorf("pq: password authentication failed for user postgres"))
	})
	w := doGET(r, "/test")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "pq:")
	assert.NotContains(t, w.Body.String(), "password")
	assert.NotContains(t, w.Body.String(), "postgres")

	body := parseErrorBody(t, w)
	assert.Equal(t, "INTERNAL_ERROR", body["code"])
}

func TestAbortInternal_DoesNotLeakSQLErrors(t *testing.T) {
	r := newRouter("/test", func(c *gin.Context) {
		AbortInternal(c, fmt.Errorf(`ERROR: relation "users" does not exist (SQLSTATE 42P01)`))
	})
	w := doGET(r, "/test")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "SQLSTATE")
	assert.NotContains(t, w.Body.String(), "relation")
}

func TestAbortInternal_DoesNotLeakConnectionStrings(t *testing.T) {
	r := newRouter("/test", func(c *gin.Context) {
		AbortInternal(c, fmt.Errorf("dial tcp 10.43.0.5:5432: connect: connection refused"))
	})
	w := doGET(r, "/test")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "10.43.0.5")
	assert.NotContains(t, w.Body.String(), "5432")
	assert.NotContains(t, w.Body.String(), "dial tcp")
}

// --- AbortBadRequest ---

func TestAbortBadRequest_ReturnsMessage(t *testing.T) {
	r := newRouter("/test", func(c *gin.Context) {
		AbortBadRequest(c, "Invalid request body")
	})
	w := doGET(r, "/test")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	body := parseErrorBody(t, w)
	assert.Equal(t, "INVALID_INPUT", body["code"])
}

// --- AbortValidation ---

func TestAbortValidation_ReturnsDetails(t *testing.T) {
	r := newRouter("/test", func(c *gin.Context) {
		AbortValidation(c, map[string]string{"field": "email", "reason": "required"})
	})
	w := doGET(r, "/test")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	body := parseErrorBody(t, w)
	assert.Equal(t, "VALIDATION_ERROR", body["code"])
}

// --- AbortNotFound ---

func TestAbortNotFound_KnownResources(t *testing.T) {
	resources := []struct {
		resourceType string
		expectedCode string
	}{
		{"project", "PROJECT_NOT_FOUND"},
		{"service", "SERVICE_NOT_FOUND"},
		{"release", "RELEASE_NOT_FOUND"},
		{"deployment", "DEPLOYMENT_NOT_FOUND"},
		{"unknown_thing", "NOT_FOUND"},
	}

	for _, tc := range resources {
		t.Run(tc.resourceType, func(t *testing.T) {
			rt := tc.resourceType
			code := tc.expectedCode
			r := newRouter("/test", func(c *gin.Context) {
				AbortNotFound(c, rt)
			})
			w := doGET(r, "/test")

			assert.Equal(t, http.StatusNotFound, w.Code)
			body := parseErrorBody(t, w)
			assert.Equal(t, code, body["code"])
		})
	}
}

// --- HandleDBError ---

func TestHandleDBError_NilError(t *testing.T) {
	// nil error should not be handled
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)

	handled := HandleDBError(c, nil, "service")
	assert.False(t, handled)
}

func TestHandleDBError_NoRows(t *testing.T) {
	r := newRouter("/test", func(c *gin.Context) {
		if HandleDBError(c, sql.ErrNoRows, "service") {
			return
		}
	})
	w := doGET(r, "/test")

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleDBError_OtherError(t *testing.T) {
	r := newRouter("/test", func(c *gin.Context) {
		if HandleDBError(c, fmt.Errorf("connection reset by peer"), "service") {
			return
		}
	})
	w := doGET(r, "/test")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "connection reset")
}

// --- ParseUUID ---

func TestParseUUID_Valid(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)

	id, ok := ParseUUID(c, "550e8400-e29b-41d4-a716-446655440000", "service_id")

	assert.True(t, ok)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", id.String())
}

func TestParseUUID_Invalid(t *testing.T) {
	r := newRouter("/test", func(c *gin.Context) {
		if _, ok := ParseUUID(c, "not-a-uuid", "service_id"); !ok {
			return
		}
	})
	w := doGET(r, "/test")

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestParseUUID_Empty(t *testing.T) {
	r := newRouter("/test", func(c *gin.Context) {
		if _, ok := ParseUUID(c, "", "service_id"); !ok {
			return
		}
	})
	w := doGET(r, "/test")

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- BindJSON ---

func TestBindJSON_InvalidJSON(t *testing.T) {
	r := gin.New()
	r.Use(ErrorHandlerMiddleware(nil))
	r.POST("/test", func(c *gin.Context) {
		var target struct {
			Name string `json:"name" binding:"required"`
		}
		if !BindJSON(c, &target) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	w := doPOST(r, "/test", `{}`)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- RecoveryMiddleware ---

func TestRecoveryMiddleware_CatchesPanic(t *testing.T) {
	r := gin.New()
	r.Use(RecoveryMiddleware(nil))
	r.GET("/panic", func(c *gin.Context) {
		panic("something went terribly wrong")
	})

	w := doGET(r, "/panic")

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	// Must return generic error
	assert.Equal(t, "internal_server_error", body["error"])
	assert.Equal(t, "An unexpected error occurred", body["message"])

	// Must NOT leak panic message
	assert.NotContains(t, w.Body.String(), "something went terribly wrong")
	// Must NOT have details field
	assert.Nil(t, body["details"])
}

func TestRecoveryMiddleware_DoesNotLeakStackTrace(t *testing.T) {
	r := gin.New()
	r.Use(RecoveryMiddleware(nil))
	r.GET("/panic", func(c *gin.Context) {
		panic(fmt.Errorf("internal path: /var/lib/postgres/data at 0x7fff12345"))
	})

	w := doGET(r, "/panic")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "/var/lib")
	assert.NotContains(t, w.Body.String(), "0x7fff")
	assert.NotContains(t, w.Body.String(), "postgres")
}

// --- ErrorHandlerMiddleware ---

func TestErrorHandlerMiddleware_HandlesAppError(t *testing.T) {
	r := newRouter("/test", func(c *gin.Context) {
		AbortWithAppError(c, errors.ErrNotFound)
	})
	w := doGET(r, "/test")

	assert.Equal(t, http.StatusNotFound, w.Code)
	body := parseErrorBody(t, w)
	assert.Equal(t, "NOT_FOUND", body["code"])
}

func TestErrorHandlerMiddleware_InternalErrorDoesNotLeak(t *testing.T) {
	r := newRouter("/test", func(c *gin.Context) {
		AbortInternal(c, fmt.Errorf("pq: SSL connection failed to 10.0.0.5:5432"))
	})
	w := doGET(r, "/test")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "pq:")
	assert.NotContains(t, w.Body.String(), "SSL")
	assert.NotContains(t, w.Body.String(), "10.0.0.5")
}
