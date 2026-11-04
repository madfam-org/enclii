package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"github.com/madfam-org/enclii/apps/waybill/internal/metering"
)

const testServiceKey = "test-service-key-0123456789abcdef"

func newTestServer(cfg *ServerConfig) *gin.Engine {
	h := NewHandlers(nil, metering.NewCalculator(nil, metering.DefaultPricing(), zap.NewNop()), zap.NewNop())
	return NewServer(h, nil, cfg, zap.NewNop()).Router()
}

// protectedRequests covers both guarded groups. Each request is well-formed
// enough that, once admitted, its handler answers without a database: the
// plans list and the estimate are pure, and the ingest endpoints reject an
// empty body with 400 before touching the collector.
func protectedRequests() []*http.Request {
	project := uuid.New().String()
	mk := func(method, path, body string) *http.Request {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		return req
	}
	return []*http.Request{
		mk(http.MethodPost, "/internal/events", "{}"),
		mk(http.MethodPost, "/internal/events/batch", "{}"),
		mk(http.MethodGet, "/api/v1/plans", ""),
		mk(http.MethodPost, "/api/v1/estimate", `{"plan":"pro"}`),
		mk(http.MethodGet, "/api/v1/projects/"+project+"/invoices", ""),
		mk(http.MethodGet, "/api/v1/projects/not-a-uuid/usage/current", ""),
	}
}

func serve(r *gin.Engine, req *http.Request) int {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestProtectedRoutesRejectMissingKey(t *testing.T) {
	r := newTestServer(&ServerConfig{InternalAPIKey: testServiceKey})
	for _, req := range protectedRequests() {
		assert.Equal(t, http.StatusUnauthorized, serve(r, req), "%s %s", req.Method, req.URL.Path)
	}
}

func TestProtectedRoutesRejectWrongKey(t *testing.T) {
	r := newTestServer(&ServerConfig{InternalAPIKey: testServiceKey})
	for _, header := range []string{"X-API-Key", "Authorization"} {
		for _, req := range protectedRequests() {
			value := testServiceKey + "x"
			if header == "Authorization" {
				value = "Bearer " + value
			}
			req.Header.Set(header, value)
			assert.Equal(t, http.StatusUnauthorized, serve(r, req), "%s %s via %s", req.Method, req.URL.Path, header)
		}
	}
}

func TestProtectedRoutesRejectKeyPrefix(t *testing.T) {
	// A truncated key must not pass: the comparison covers the whole value.
	r := newTestServer(&ServerConfig{InternalAPIKey: testServiceKey})
	for _, req := range protectedRequests() {
		req.Header.Set("X-API-Key", testServiceKey[:8])
		assert.Equal(t, http.StatusUnauthorized, serve(r, req), "%s %s", req.Method, req.URL.Path)
	}
}

func TestProtectedRoutesAdmitValidKey(t *testing.T) {
	r := newTestServer(&ServerConfig{InternalAPIKey: testServiceKey})
	for _, header := range []string{"X-API-Key", "Authorization"} {
		for _, req := range protectedRequests() {
			value := testServiceKey
			if header == "Authorization" {
				value = "Bearer " + value
			}
			req.Header.Set(header, value)
			code := serve(r, req)
			assert.NotEqual(t, http.StatusUnauthorized, code, "%s %s via %s", req.Method, req.URL.Path, header)
			assert.NotEqual(t, http.StatusServiceUnavailable, code, "%s %s via %s", req.Method, req.URL.Path, header)
		}
	}
}

func TestValidKeyReachesHandler(t *testing.T) {
	r := newTestServer(&ServerConfig{InternalAPIKey: testServiceKey})

	plans := httptest.NewRequest(http.MethodGet, "/api/v1/plans", nil)
	plans.Header.Set("X-API-Key", testServiceKey)
	assert.Equal(t, http.StatusOK, serve(r, plans))

	// An admitted ingest call meets the handler's own validation.
	event := httptest.NewRequest(http.MethodPost, "/internal/events", bytes.NewBufferString("{}"))
	event.Header.Set("Content-Type", "application/json")
	event.Header.Set("X-API-Key", testServiceKey)
	assert.Equal(t, http.StatusBadRequest, serve(r, event))
}

func TestConfiguredKeyIsTrimmed(t *testing.T) {
	r := newTestServer(&ServerConfig{InternalAPIKey: testServiceKey + "\n"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plans", nil)
	req.Header.Set("X-API-Key", testServiceKey)
	assert.Equal(t, http.StatusOK, serve(r, req))
}

func TestMissingConfiguredKeyFailsClosed(t *testing.T) {
	for name, cfg := range map[string]*ServerConfig{
		"empty":      {},
		"whitespace": {InternalAPIKey: " \n"},
		"nil":        nil,
	} {
		r := newTestServer(cfg)
		for _, req := range protectedRequests() {
			// An empty presented key must not "match" an empty configured one.
			req.Header.Set("X-API-Key", "")
			assert.Equal(t, http.StatusServiceUnavailable, serve(r, req), "%s: %s %s", name, req.Method, req.URL.Path)
		}
		for _, req := range protectedRequests() {
			req.Header.Set("X-API-Key", "anything")
			assert.Equal(t, http.StatusServiceUnavailable, serve(r, req), "%s: %s %s", name, req.Method, req.URL.Path)
		}
	}
}

func TestHealthEndpointsNeedNoKey(t *testing.T) {
	for _, cfg := range []*ServerConfig{{InternalAPIKey: testServiceKey}, {}} {
		r := newTestServer(cfg)
		for _, path := range []string{"/health", "/ready"} {
			assert.Equal(t, http.StatusOK, serve(r, httptest.NewRequest(http.MethodGet, path, nil)), path)
		}
	}
}
